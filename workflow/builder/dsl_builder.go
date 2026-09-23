package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/rulego/rulego/api/types"

	param "github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	confPackage "github.com/magic-lib/go-plat-workflow/workflow/config"
)

// DSLBuilder 规则链 DSL 组装器，实现 workflow.DSLBuilder 接口。
type DSLBuilder struct {
	nodeStore      workflow.NodeStore
	subChainStore  workflow.SubChainStore
	rootChainStore workflow.RootChainStore
}

// NewDSLBuilder 创建 DSL 组装器实例。
func NewDSLBuilder(nodeStore workflow.NodeStore, subChainStore workflow.SubChainStore, rootChainStore workflow.RootChainStore) *DSLBuilder {
	return &DSLBuilder{
		nodeStore:      nodeStore,
		subChainStore:  subChainStore,
		rootChainStore: rootChainStore,
	}
}

// Build 根据 BuildRequest 组装生成 RootChainDSL JSON。
// 流程：
//  1. 从 NodeStore 查询指定项目下的所有引用节点
//  2. 从 SubChainStore 查询指定项目下的所有引用子链
//  3. 构建 metadata.nodes 数组
//  4. 处理子链的节点合并（ID 加前缀防冲突）
//  5. 构建 metadata.connections 数组
//  6. 序列化为合法 rulego JSON 并存入数据库
func (b *DSLBuilder) Build(ctx context.Context, req *workflow.BuildRequest) (*workflow.RootChainDef, error) {
	if req.Project == "" {
		return nil, fmt.Errorf("%w: project is required", workflow.ErrDSLBuildFailed)
	}
	if req.ChainID == "" {
		return nil, fmt.Errorf("%w: chain_id is required", workflow.ErrDSLBuildFailed)
	}
	if len(req.NodeIDs) == 0 && len(req.SubChainIDs) == 0 {
		return nil, fmt.Errorf("%w: at least one node or sub chain must be specified", workflow.ErrDSLBuildFailed)
	}

	// 1. 查询节点（仅当前项目）；node_ids 可能含实例后缀 baseId__N，需去重为节点定义 ID
	nodes, err := b.nodeStore.ListByIDs(ctx, req.Project, dedupBaseIDs(req.NodeIDs))
	if err != nil {
		return nil, fmt.Errorf("%w: query nodes: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 2. 查询子链（仅当前项目，避免加载其他项目的子链）
	subChains, err := b.subChainStore.ListByIDs(ctx, req.Project, req.SubChainIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: query sub chains: %v", workflow.ErrDSLBuildFailed, err)
	}

	log.Ctx(ctx).Debug().
		Str("project", req.Project).
		Int("node_count", len(nodes)).
		Int("sub_chain_count", len(subChains)).
		Str("chain_id", req.ChainID).
		Msg("building root chain DSL")

	// 3. 构建 RuleChain DSL
	ruleChain := b.buildRuleChain(req, nodes, subChains)

	// 4. 序列化
	dslJSON := conv.String(ruleChain)

	// 5. 序列化 connections 为 JSON 存入独立字段，方便后续查看和修改
	connectionsJSON, _ := json.Marshal(req.Connections)

	// 计算当前链所有有效实例 ID（节点实例 + 子链 flow 实例），用于裁剪覆盖项
	validInstances := make(map[string]bool, len(req.NodeIDs)+len(req.SubChainIDs))
	for _, id := range req.NodeIDs {
		if id != "" {
			validInstances[id] = true
		}
	}
	for _, id := range req.SubChainIDs {
		if id != "" {
			validInstances[id] = true
		}
	}

	// 序列化前先裁剪：删除已被删除节点残留的覆盖配置，避免数据冗余/对应错误；
	// 再按节点定义同步每个实例的参数覆盖：节点已无的参数删除、节点新增的参数补充（以默认值），
	// 使 node_param_overrides 与节点实时变化保持一致。
	nodeParamOverrides := reconcileNodeParamOverrides(pruneOverrides(req.NodeParamOverrides, validInstances), nodes)

	nodeSwitchOverrides := pruneOverrides(req.NodeSwitchOverrides, validInstances)
	nodeNameOverrides := pruneOverrides(req.NodeNameOverrides, validInstances)
	nodeCollapseOverrides := pruneOverrides(req.NodeCollapseOverrides, validInstances)

	// 序列化 node_param_overrides 以便保存到根链，后续可恢复
	nodeParamOverridesJSON, _ := json.Marshal(nodeParamOverrides)

	// 序列化 node_switch_overrides 以便保存到根链，后续可恢复
	nodeSwitchOverridesJSON, _ := json.Marshal(nodeSwitchOverrides)

	// 序列化 node_name_overrides 以便保存到根链，后续可恢复
	nodeNameOverridesJSON, _ := json.Marshal(nodeNameOverrides)

	// 序列化 node_collapse_overrides 以便保存到根链（参数配置区收起状态），后续可恢复
	nodeCollapseOverridesJSON, _ := json.Marshal(nodeCollapseOverrides)

	// 6. 存储到数据库
	def := &workflow.RootChainDef{
		Project:             req.Project,
		ChainID:             req.ChainID,
		ChainKey:            req.ChainKey,
		Name:                req.ChainName,
		Description:         req.Description,
		DSLJSON:             dslJSON,
		Status:              1,
		NodeIDs:             strings.Join(req.NodeIDs, ","),
		SubChainIDs:         strings.Join(req.SubChainIDs, ","),
		ConnectionsData:     string(connectionsJSON),
		NodeParamOverrides:  string(nodeParamOverridesJSON),
		NodeSwitchOverrides: string(nodeSwitchOverridesJSON),
		NodeNameOverrides:   string(nodeNameOverridesJSON),
		NodeCollapseOverrides: string(nodeCollapseOverridesJSON),
	}
	// 先尝试更新（按 project+chain_id），不存在再创建。
	// 避免每次保存都物理删除重建导致自增主键 id 持续增长。
	if err := b.rootChainStore.Update(ctx, def); err != nil {
		if errors.Is(err, workflow.ErrRootChainNotFound) {
			// 不存在则创建
			if err := b.rootChainStore.Create(ctx, def); err != nil {
				return nil, fmt.Errorf("%w: save root chain: %v", workflow.ErrDSLBuildFailed, err)
			}
		} else {
			return nil, fmt.Errorf("%w: update root chain: %v", workflow.ErrDSLBuildFailed, err)
		}
	}

	log.Ctx(ctx).Info().
		Str("project", def.Project).
		Str("chain_id", def.ChainID).
		Int("dsl_size", len(dslJSON)).
		Msg("root chain DSL built and saved")

	return def, nil
}

// BuildSubChain 编排方式组装子链 DSL 并保存（新建）。
// req.ChainID 必须由调用方保证已赋值（为空时由 service 层自动生成）。
func (b *DSLBuilder) BuildSubChain(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	def, err := b.AssembleSubChain(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := b.subChainStore.Create(ctx, def); err != nil {
		return nil, fmt.Errorf("%w: save sub chain: %v", workflow.ErrDSLBuildFailed, err)
	}

	log.Ctx(ctx).Info().
		Str("project", def.Project).
		Str("chain_id", def.ChainID).
		Int("dsl_size", len(def.DSLJSON)).
		Msg("sub chain DSL built and saved")

	return def, nil
}

// AssembleSubChain 组装子链 DSL（不保存，可用于创建或更新）。
// 子链的编排方式与 RootChain 完全一致：节点 + 连接 + 可选嵌套子链引用（flow 节点），
// 并支持 FirstNodeIndex / Configuration 等根链级配置。
func (b *DSLBuilder) AssembleSubChain(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	if req.Project == "" {
		return nil, fmt.Errorf("%w: project is required", workflow.ErrDSLBuildFailed)
	}
	if req.ChainID == "" {
		return nil, fmt.Errorf("%w: chain_id is required", workflow.ErrDSLBuildFailed)
	}
	if len(req.NodeIDs) == 0 && len(req.SubChainIDs) == 0 {
		return nil, fmt.Errorf("%w: at least one node or sub chain must be specified", workflow.ErrDSLBuildFailed)
	}

	// 查询节点（仅当前项目）；node_ids 可能含实例后缀 baseId__N，需去重为节点定义 ID
	nodes, err := b.nodeStore.ListByIDs(ctx, req.Project, dedupBaseIDs(req.NodeIDs))
	if err != nil {
		return nil, fmt.Errorf("%w: query nodes: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 查询嵌套子链（仅当前项目）
	subChains, err := b.subChainStore.ListByIDs(ctx, req.Project, req.SubChainIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: query sub chains: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 复用根链节点构建逻辑（含 flow 节点生成 + 连接转换 + FirstNodeIndex/Configuration）
	fullReq := &workflow.BuildRequest{
		Project:             req.Project,
		ChainID:             req.ChainID,
		ChainName:           req.ChainName,
		Description:         req.Description,
		NodeIDs:             req.NodeIDs,
		SubChainIDs:         req.SubChainIDs,
		Connections:         req.Connections,
		DebugMode:           req.DebugMode,
		Configuration:       req.Configuration,
		FirstNodeIndex:      req.FirstNodeIndex,
		NodeParamOverrides:  req.NodeParamOverrides,
		NodeSwitchOverrides: req.NodeSwitchOverrides,
		NodeNameOverrides:   req.NodeNameOverrides,
	}
	ruleChain := b.buildRuleChain(fullReq, nodes, subChains)
	// 子链标记为非 Root
	ruleChain.RuleChain.Root = false

	dslJSON, err := json.Marshal(ruleChain)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal dsl: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 计算当前链所有有效实例 ID（节点实例 + 子链 flow 实例），用于裁剪覆盖项
	validInstances := make(map[string]bool, len(req.NodeIDs)+len(req.SubChainIDs))
	for _, id := range req.NodeIDs {
		if id != "" {
			validInstances[id] = true
		}
	}
	for _, id := range req.SubChainIDs {
		if id != "" {
			validInstances[id] = true
		}
	}

	// 序列化前先裁剪：删除已被删除节点残留的覆盖配置，避免数据冗余/对应错误
	nodeParamOverrides := pruneOverrides(req.NodeParamOverrides, validInstances)
	nodeSwitchOverrides := pruneOverrides(req.NodeSwitchOverrides, validInstances)
	nodeNameOverrides := pruneOverrides(req.NodeNameOverrides, validInstances)
	nodeCollapseOverrides := pruneOverrides(req.NodeCollapseOverrides, validInstances)

	// 序列化溯源字段
	connectionsJSON, _ := json.Marshal(req.Connections)
	nodeParamOverridesJSON, _ := json.Marshal(nodeParamOverrides)
	nodeSwitchOverridesJSON, _ := json.Marshal(nodeSwitchOverrides)
	nodeNameOverridesJSON, _ := json.Marshal(nodeNameOverrides)
	nodeCollapseOverridesJSON, _ := json.Marshal(nodeCollapseOverrides)

	return &workflow.SubChainDef{
		Project:             req.Project,
		ChainID:             req.ChainID,
		Name:                req.ChainName,
		Description:         req.Description,
		DSLJSON:             string(dslJSON),
		Status:              1,
		SubChainIDs:         strings.Join(req.SubChainIDs, ","),
		NodeIDs:             strings.Join(req.NodeIDs, ","),
		ConnectionsData:     string(connectionsJSON),
		NodeParamOverrides:  string(nodeParamOverridesJSON),
		NodeSwitchOverrides: string(nodeSwitchOverridesJSON),
		NodeNameOverrides:   string(nodeNameOverridesJSON),
		NodeCollapseOverrides: string(nodeCollapseOverridesJSON),
	}, nil
}

// instanceRef 描述一个编排中的节点实例：baseId 为节点定义 ID，instanceId 为 DSL 中
// 实际使用的唯一节点 ID（同一节点可多次添加，instanceId 形如 baseId__<随机段>，全局唯一）。
type instanceRef struct {
	baseId     string
	instanceId string
}

// dedupBaseIDs 将可能含实例后缀（baseId__N）的 ID 列表去重为节点定义 ID，用于查询节点定义。
func dedupBaseIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		base := raw
		if i := strings.Index(raw, "__"); i >= 0 {
			base = raw[:i]
		}
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
	}
	return out
}

// parseInstanceRefs 将编排传入的 ID 列表（可能含 baseId__N 后缀）解析为实例引用列表。
func parseInstanceRefs(ids []string) []instanceRef {
	out := make([]instanceRef, 0, len(ids))
	for _, raw := range ids {
		if raw == "" {
			continue
		}
		base := raw
		if i := strings.Index(raw, "__"); i >= 0 {
			base = raw[:i]
		}
		out = append(out, instanceRef{baseId: base, instanceId: raw})
	}
	return out
}

// pruneOverrides 仅保留 key 存在于 valid 集合中的覆盖项，删除已不存在节点（如被删除的节点）
// 对应的覆盖配置，避免 node_param_overrides / node_switch_overrides / node_name_overrides 残留
// 过期数据造成冗余或对应错误。
func pruneOverrides[V any](m map[string]V, valid map[string]bool) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		if valid[k] {
			out[k] = v
		}
	}
	return out
}

// reconcileNodeParamOverrides 对 node_param_overrides 中每个节点的参数做与节点定义的同步：
//   - 若该节点已不存在（被删除），整条覆盖丢弃；
//   - 覆盖中某参数在节点定义中存在 → 保留该对象；
//   - 覆盖中某参数在节点定义中不存在 → 删除（节点已移除该参数）；
//   - 节点定义中存在但覆盖中缺少的参数 → 以节点默认值补充上来。
//
// overrides 以实例 ID（形如 baseId__N）为 key，节点参数 key 为内层 key。
func reconcileNodeParamOverrides(overrides map[string]map[string]interface{}, nodes []*workflow.NodeDef) map[string]map[string]interface{} {
	// baseId -> 参数默认值（key -> 节点 DB 默认值），用于补充缺失参数
	paramDefaults := make(map[string]map[string]*confPackage.NodeConfigOverrideArgument, len(nodes))
	for _, nd := range nodes {
		defs := make(map[string]*confPackage.NodeConfigOverrideArgument)
		if len(nd.Params) > 0 {
			var args []*confPackage.NodeConfigArgument
			if err := json.Unmarshal(nd.Params, &args); err == nil {
				for _, a := range args {
					if a.Key == "" {
						continue
					}
					defs[a.Key] = &confPackage.NodeConfigOverrideArgument{
						Private: false,
						Src:     "fixed",
						Value:   a.Value,
					}
				}
			}
		}
		paramDefaults[nd.NodeID] = defs
	}

	out := make(map[string]map[string]interface{}, len(overrides))
	for instId, entry := range overrides {
		baseId := instId
		if idx := strings.Index(instId, "__"); idx >= 0 {
			baseId = instId[:idx]
		}
		defaults, ok := paramDefaults[baseId]
		if !ok {
			// 节点已不存在：丢弃该覆盖项（避免引用过期节点参数）
			continue
		}
		// 若节点定义中解析不出任何参数 key（如 Params 为空或非标准格式），
		// 无法判断哪些参数有效，则保持该实例原有覆盖不变，避免误删/污染。
		if len(defaults) == 0 {
			out[instId] = entry
			continue
		}
		newEntry := make(map[string]interface{})
		// 保留节点定义中存在、且覆盖中也有的参数
		for k, v := range entry {
			if _, exists := defaults[k]; exists {
				newEntry[k] = v
			}
		}
		// 补充节点定义中有但覆盖中缺失的参数（以节点默认值为准）
		for k, dv := range defaults {
			if _, has := entry[k]; !has {
				newEntry[k] = dv
			}
		}
		out[instId] = newEntry
	}
	return out
}

// sortBindConfigsByKey 按 Key 稳定排序 []*param.BindConfig，使 DSL 中的 arguments / responses
// 顺序与节点定义保持一致、可读且稳定（更新时不受遍历顺序影响）。
func sortBindConfigsByKey(bcs []*param.BindConfig) {
	sort.Slice(bcs, func(i, j int) bool {
		return bcs[i].Key < bcs[j].Key
	})
}
func sortRespConfigsByKey(bcs []*confPackage.NodeConfigResponse) {
	sort.Slice(bcs, func(i, j int) bool {
		return bcs[i].Key < bcs[j].Key
	})
}

// buildRuleNodes 将节点实例引用转换为 rulego RuleNode 列表（含参数覆盖策略合并）。
// 同一节点定义可出现多次，每次使用各自的 instanceId 作为 RuleNode ID，
// 参数覆盖 override key 也以 instanceId 匹配，从而实现同一节点在编排中添加多次。
func (b *DSLBuilder) buildRuleNodes(instances []instanceRef, defById map[string]*workflow.NodeDef, overrides map[string]map[string]interface{}, switchOverrides map[string]string, nameOverrides map[string]string) []*types.RuleNode {
	ruleNodes := make([]*types.RuleNode, 0, len(instances))
	for _, inst := range instances {
		node, ok := defById[inst.baseId]
		if !ok {
			continue
		}
		config := make(types.Configuration)
		if len(node.Configuration) > 0 {
			_ = json.Unmarshal(node.Configuration, &config)
		}

		// 解析节点参数定义（带策略），使用 param 包的覆盖策略合并用户输入与节点默认值
		// Params 格式: [{"key":"url","value":"https://default.com","policy":"backend+"}, ...]
		var bindConfigs []*param.BindConfig
		if len(node.Params) > 0 {
			// 注意：必须用标准 json.Unmarshal，不要用 conv.Unmarshal。
			// conv.Unmarshal 内部先走 copier 反射（依赖 go-plat-utils 版本/字段名），
			// 对 []*param.BindConfig 不可靠，可能静默产出空切片；与 reconcileNodeParamOverrides 保持一致。
			if err := json.Unmarshal(node.Params, &bindConfigs); err != nil {
				log.Ctx(context.Background()).Warn().Str("node_id", inst.baseId).Err(err).Msg("parse node.Params failed")
			}
		}

		// 构建用户传入参数（frontend），override key 使用实例 ID 以区分同一节点的多次添加
		frontendMap := make(map[string]any)
		frontendSrc := make(map[string]string) // 记录每个覆盖参数的来源 src，用于设置 DSL 的 policy
		privateKeys := make([]string, 0)       // 私有参数 key 列表（需从入参二级结构取值）
		if nodeOverrides, ok := overrides[inst.instanceId]; ok {
			for k, v := range nodeOverrides {
				// 兼容两种格式：
				//  - 新格式：{ "src": "fixed/upstream/entry", "value": "<最终值>" }（对象）
				//  - 旧格式：直接是字符串值（纯值）
				// 取其中的 value 作为写入节点 arguments 的最终值。
				if m, ok := v.(map[string]any); ok {
					if val, exists := m["value"]; exists {
						if k != "" {
							frontendMap[k] = val
						}
						if src, ok := m["src"].(string); ok {
							frontendSrc[k] = src
						}
						// 记录私有参数 key，供 DSL 持久化
						if pv, ok := m["private"].(bool); ok && pv {
							privateKeys = append(privateKeys, k)
						}
						continue
					}
				}
				if k != "" {
					frontendMap[k] = v
				}
			}
		}

		// 编排参数来源对应的 DSL policy（不改节点参数定义本身，仅在此处改写 DSL）：
		//  - ref_node（调用传入）：调用方本身就是来源，保留节点定义默认 policy（调用方优先）
		//  - ref_act（引用前序）：已配置来源 → 强制后台 PolicyBackendOnly，调用方同名不可覆盖
		//  - value（固定配置）且值为空：回退到节点参数定义里的默认 policy
		//  - value（固定配置）且值非空：已配置来源 → 强制后台 PolicyBackendOnly
		isEmptyValue := func(v any) bool {
			if v == nil {
				return true
			}
			s, ok := v.(string)
			return ok && s == ""
		}
		resolvePolicy := func(src string, v any, defaultPolicy param.KeySourcePolicy) param.KeySourcePolicy {
			switch src {
			case "ref_node":
				return defaultPolicy // 调用传入：保持调用方优先
			case "ref_act":
				return param.KeyPolicyBackendOnly // 引用前序：强制后台
			default: // value / 无来源
				if isEmptyValue(v) {
					return defaultPolicy // 固定配置为空：回退到节点参数定义默认 policy
				}
				return param.KeyPolicyBackendOnly // 固定配置非空：强制后台
			}
		}

		if len(bindConfigs) > 0 {
			// 将用户覆盖值合并进 BindConfig 数组（保留 key/value/policy），
			// 不直接展开为具体值，便于后期执行时按 policy 判断是否需要直接覆盖。
			args := make([]*param.BindConfig, 0)
			usedKeys := make(map[string]bool, len(bindConfigs))
			for _, bc := range bindConfigs {
				if v, ok := frontendMap[bc.Key]; ok {
					nb := *bc // 复制，避免修改节点定义缓存
					nb.Value = v
					// 编排里已配置来源：按来源改写 DSL 的 policy，避免调用方传同名参数覆盖配置的来源
					nb.Policy = resolvePolicy(frontendSrc[bc.Key], v, bc.Policy)
					if nb.Key == "" {
						continue
					}
					args = append(args, &nb)
					usedKeys[bc.Key] = true
				} else {
					if bc.Key == "" {
						continue
					}
					args = append(args, bc)
				}
			}
			// 按 key 自动排序，保证 DSL 稳定可读、与节点参数定义实时一致
			sortBindConfigsByKey(args)
			config["arguments"] = args
		} else if len(frontendMap) > 0 {
			// 无参数定义时的兜底：仍以 BindConfig 数组格式保存，便于后期判断覆盖策略
			args := make([]*param.BindConfig, 0)
			for k, v := range frontendMap {
				if k == "" {
					continue
				}
				args = append(args, &param.BindConfig{Key: k, Value: v, Policy: resolvePolicy(frontendSrc[k], v, param.KeyPolicyFrontendPriority)})
			}
			sortBindConfigsByKey(args)
			config["arguments"] = args
		}

		// responses：取节点定义中的返回值配置（config 已实时从 node.Configuration 加载），
		// 按 key 自动排序，确保与节点实时变化保持一致、顺序稳定。
		if raw, ok := config["responses"]; ok && raw != nil {
			var respArr []*confPackage.NodeConfigResponse
			if b, _ := json.Marshal(raw); len(b) > 0 && string(b) != "null" {
				if err := json.Unmarshal(b, &respArr); err == nil && len(respArr) > 0 {
					sortRespConfigsByKey(respArr)
					config["responses"] = respArr
				}
			}
		}

		addInfo := make(map[string]interface{})
		if len(node.AdditionalInfo) > 0 {
			_ = json.Unmarshal(node.AdditionalInfo, &addInfo)
		}

		// 将节点参数定义（含 label）注入 additionalInfo，供执行页展示「参数key（中文名）」。
		// 节点参数定义格式：[{"key":"url","label":"请求URL","type":"string",...}]
		if len(node.Params) > 0 {
			var paramDefs []struct {
				Key   string `json:"key"`
				Label string `json:"label"`
			}
			if _ = json.Unmarshal(node.Params, &paramDefs); len(paramDefs) > 0 {
				labelMap := make(map[string]string, len(paramDefs))
				for _, p := range paramDefs {
					if p.Key != "" {
						labelMap[p.Key] = p.Label
					}
				}
				labelJSON, _ := json.Marshal(labelMap)
				addInfo["node_param_labels"] = string(labelJSON)
			}
		}

		// 将私有参数 key 注入 additionalInfo，随 DSL 持久化，便于编排回显时恢复「是否私有」状态
		if len(privateKeys) > 0 {
			privJSON, _ := json.Marshal(privateKeys)
			addInfo["node_private_params"] = string(privJSON)
		}

		// 应用每节点 switch_condition 覆盖（仅本链生效）：写入该 DSL 节点 configuration.node_config.switch_condition，不改节点定义。
		// 覆盖键为节点实例 instanceId（形如 baseId__random），仅 Activity / CondSwitch 节点有意义。
		if sw, ok := switchOverrides[inst.instanceId]; ok && strings.TrimSpace(sw) != "" {
			if node.Type == common.ActivityNodeTypeName || node.Type == common.CondSwitchNodeTypeName {
				nc, _ := config["node_config"].(map[string]any)
				if nc == nil {
					nc = make(map[string]any)
				}
				nc["switch_condition"] = strings.TrimSpace(sw)
				config["node_config"] = nc
			}
		}

		// 应用每节点实例名称覆盖（仅本链生效）：写入该 DSL 节点 Name，不改节点定义。
		// 覆盖键为节点实例 instanceId（形如 baseId__random）。
		nodeName := node.Name
		if nm, ok := nameOverrides[inst.instanceId]; ok && strings.TrimSpace(nm) != "" {
			nodeName = strings.TrimSpace(nm)
		}

		ruleNodes = append(ruleNodes, &types.RuleNode{
			Id:             inst.instanceId,
			Type:           node.Type,
			Name:           nodeName,
			DebugMode:      node.DebugMode,
			Configuration:  config,
			AdditionalInfo: addInfo,
		})
	}
	return ruleNodes
}

// buildRuleChain 组装完整的 types.RuleChain 结构体。
func (b *DSLBuilder) buildRuleChain(req *workflow.BuildRequest, nodeDefs []*workflow.NodeDef, subChains []*workflow.SubChainDef) *types.RuleChain {
	// 构建 metadata nodes
	var ruleNodes []*types.RuleNode
	idMap := make(map[string]bool) // 用于检测 ID 冲突

	// 3a. 添加主链节点（支持同一节点多次添加，实例 ID 形如 baseId__N）
	defById := make(map[string]*workflow.NodeDef, len(nodeDefs))
	for _, nd := range nodeDefs {
		defById[nd.NodeID] = nd
	}
	instances := parseInstanceRefs(req.NodeIDs)
	mainNodes := b.buildRuleNodes(instances, defById, req.NodeParamOverrides, req.NodeSwitchOverrides, req.NodeNameOverrides)
	for _, n := range mainNodes {
		idMap[n.Id] = true
	}
	ruleNodes = append(ruleNodes, mainNodes...)

	// 3b. 处理子链：为每个子链创建一个 Flow Node 节点
	// Flow Node 使用 type="flow" + configuration.ruleChainId 引用子链，
	// 替代已废弃的 RuleChainConnections 机制。
	// 子链 DSL 需提前加载到 rulego pool 中。
	for _, sc := range subChains {
		// Flow Node 的 ID 直接使用子链自身 ChainID，不再加 flow_ 前缀。
		// 子链 ID 前缀为 S，普通节点前缀为 N，与 idMap 冲突检测配合不会撞。
		flowNodeID := sc.ChainID
		// 冲突检测（极端情况下子链 ID 与真实节点 ID 冲突）
		if idMap[flowNodeID] {
			flowNodeID = sc.ChainID + "_dup"
		}
		idMap[flowNodeID] = true

		// ruleChainId 必须与 engine 加载子链时用的 pool key 一致：project:chainID
		poolKey := req.Project + ":" + sc.ChainID
		flowNode := &types.RuleNode{
			Id:   flowNodeID,
			Type: "flow",
			Name: sc.Name,
			Configuration: types.Configuration{
				"ruleChainId": poolKey,
			},
		}
		ruleNodes = append(ruleNodes, flowNode)
	}

	// 4. 构建连接关系
	connections := make([]types.NodeConnection, 0, len(req.Connections))
	for _, conn := range req.Connections {
		// 子链 Flow Node 的 ID 即子链自身 ChainID，无需再加 flow_ 前缀转换。
		fromID := conn.FromID
		toID := conn.ToID
		connections = append(connections, types.NodeConnection{
			FromId: fromID,
			ToId:   toID,
			Type:   conn.Type,
			Label:  conn.Label,
		})
	}

	// 5. 组装 RuleChain
	debugMode := req.DebugMode
	firstNodeIndex := req.FirstNodeIndex
	config := make(types.Configuration)
	if len(req.Configuration) > 0 {
		_ = json.Unmarshal(req.Configuration, &config)
	}

	return &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{
			ID:            req.ChainID,
			Name:          req.ChainName,
			DebugMode:     debugMode,
			Root:          true,
			Disabled:      false,
			Configuration: config,
		},
		Metadata: types.RuleMetadata{
			FirstNodeIndex: firstNodeIndex,
			Nodes:          ruleNodes,
			Connections:    connections,
		},
	}
}
