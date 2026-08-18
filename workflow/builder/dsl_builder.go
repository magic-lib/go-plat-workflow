package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/rulego/rulego/api/types"

	param "github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow"
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
	dslJSON, err := json.Marshal(ruleChain)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal dsl: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 5. 序列化 connections 为 JSON 存入独立字段，方便后续查看和修改
	connectionsJSON, _ := json.Marshal(req.Connections)

	// 序列化 node_param_overrides 以便保存到根链，后续可恢复
	nodeParamOverridesJSON, _ := json.Marshal(req.NodeParamOverrides)

	// 6. 存储到数据库
	def := &workflow.RootChainDef{
		Project:            req.Project,
		ChainID:            req.ChainID,
		ChainKey:           req.ChainKey,
		Name:               req.ChainName,
		Description:        req.Description,
		DSLJSON:            string(dslJSON),
		Status:             1,
		NodeIDs:            strings.Join(req.NodeIDs, ","),
		SubChainIDs:        strings.Join(req.SubChainIDs, ","),
		ConnectionsData:    string(connectionsJSON),
		NodeParamOverrides: string(nodeParamOverridesJSON),
	}
	if err := b.rootChainStore.Create(ctx, def); err != nil {
		return nil, fmt.Errorf("%w: save root chain: %v", workflow.ErrDSLBuildFailed, err)
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
		Project:            req.Project,
		ChainID:            req.ChainID,
		ChainName:          req.ChainName,
		Description:        req.Description,
		NodeIDs:            req.NodeIDs,
		SubChainIDs:        req.SubChainIDs,
		Connections:        req.Connections,
		DebugMode:          req.DebugMode,
		Configuration:      req.Configuration,
		FirstNodeIndex:     req.FirstNodeIndex,
		NodeParamOverrides: req.NodeParamOverrides,
	}
	ruleChain := b.buildRuleChain(fullReq, nodes, subChains)
	// 子链标记为非 Root
	ruleChain.RuleChain.Root = false

	dslJSON, err := json.Marshal(ruleChain)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal dsl: %v", workflow.ErrDSLBuildFailed, err)
	}

	// 序列化溯源字段
	connectionsJSON, _ := json.Marshal(req.Connections)
	nodeParamOverridesJSON, _ := json.Marshal(req.NodeParamOverrides)

	return &workflow.SubChainDef{
		Project:            req.Project,
		ChainID:           req.ChainID,
		Name:               req.ChainName,
		Description:        req.Description,
		DSLJSON:            string(dslJSON),
		Status:             1,
		SubChainIDs:        strings.Join(req.SubChainIDs, ","),
		NodeIDs:            strings.Join(req.NodeIDs, ","),
		ConnectionsData:    string(connectionsJSON),
		NodeParamOverrides: string(nodeParamOverridesJSON),
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

// buildRuleNodes 将节点实例引用转换为 rulego RuleNode 列表（含参数覆盖策略合并）。
// 同一节点定义可出现多次，每次使用各自的 instanceId 作为 RuleNode ID，
// 参数覆盖 override key 也以 instanceId 匹配，从而实现同一节点在编排中添加多次。
func (b *DSLBuilder) buildRuleNodes(instances []instanceRef, defById map[string]*workflow.NodeDef, overrides map[string]map[string]interface{}) []*types.RuleNode {
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
			_ = json.Unmarshal(node.Params, &bindConfigs)
		}

		// 构建用户传入参数（frontend），override key 使用实例 ID 以区分同一节点的多次添加
		frontendMap := make(map[string]any)
		if nodeOverrides, ok := overrides[inst.instanceId]; ok {
			for k, v := range nodeOverrides {
				frontendMap[k] = v
			}
		}

		if len(bindConfigs) > 0 {
			// 将用户覆盖值合并进 BindConfig 数组（保留 key/value/policy），
			// 不直接展开为具体值，便于后期执行时按 policy 判断是否需要直接覆盖。
			args := make([]*param.BindConfig, 0, len(bindConfigs))
			usedKeys := make(map[string]bool, len(bindConfigs))
			for _, bc := range bindConfigs {
				if v, ok := frontendMap[bc.Key]; ok {
					nb := *bc           // 复制，避免修改节点定义缓存
					nb.Value = v        // 用户覆盖值
					args = append(args, &nb)
					usedKeys[bc.Key] = true
				} else {
					args = append(args, bc)
				}
			}
			// 用户新增了节点定义中不存在的 key，统一追加（默认直接覆盖策略）
			for k, v := range frontendMap {
				if usedKeys[k] {
					continue
				}
				args = append(args, &param.BindConfig{Key: k, Value: v})
			}
			// arguments 为 BindConfig 数组 JSON：[{"key":..,"value":..,"policy":..}, ...]
			config["arguments"] = args
		} else if len(frontendMap) > 0 {
			// 无参数定义时的兜底：仍以 BindConfig 数组格式保存，便于后期判断覆盖策略
			args := make([]*param.BindConfig, 0, len(frontendMap))
			for k, v := range frontendMap {
				args = append(args, &param.BindConfig{Key: k, Value: v})
			}
			config["arguments"] = args
		}

		addInfo := make(map[string]interface{})
		if len(node.AdditionalInfo) > 0 {
			_ = json.Unmarshal(node.AdditionalInfo, &addInfo)
		}

		ruleNodes = append(ruleNodes, &types.RuleNode{
			Id:             inst.instanceId,
			Type:           node.Type,
			Name:           node.Name,
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
	mainNodes := b.buildRuleNodes(instances, defById, req.NodeParamOverrides)
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
			FirstNodeIndex:  firstNodeIndex,
			Nodes:           ruleNodes,
			Connections:     connections,
		},
	}
}

