package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// ============================================================
// 已发布引用检查
// ============================================================
// 节点/activity 一旦被包含在「当前生效」的根链（生产快照，is_current=true）中，即禁止编辑与删除，
// 避免改动影响线上调用。判断依据为发布快照的 DSL（含子链传递引用）：
//   - 根链发布快照 wf_root_chain_releases.dsl_json（生产执行的真实 DSL，仅 is_current=true 的版本）
//   - 被根链 flow 节点引用的子链 DSL（生产执行时按 ID 实时加载，同样属于线上内容）
//
// 重要：仅以 is_current=true 的「线上生效版本」为准。
// 历史发布版本（可回滚候选、未上线）不计入，即出现在历史版本里的节点/activity 仍可正常编辑、删除。

// publishedRefIndex 从已发布根链（含传递引用的子链）DSL 中提取的引用集合。
type publishedRefIndex struct {
	nodes      map[string]struct{} // 节点定义 ID（已去实例后缀）
	activities map[string]struct{} // act_namespace + "\x00" + act_name
	// nodeChains 节点定义 ID →（根链 ChainID → 根链信息），记录每条根链对节点的引用。
	// 用于 Nodes 列表展示「被多少个已发布根链引用」及明细。
	// 注意：同一节点被同一根链多处（含子链递归）引用时按去重处理，只计一次。
	nodeChains map[string]map[string]*workflow.PublishedRootChainRef
	// activityChains activity 键 →（根链 ChainID → 根链信息），语义同 nodeChains。
	activityChains map[string]map[string]*workflow.PublishedRootChainRef
}

// addRef 记录某次引用：键 → 根链。keyType 由调用方保证一致性（节点 ID 或 activity 键）。
func addRef(m map[string]map[string]*workflow.PublishedRootChainRef, key string, cur *workflow.PublishedRootChainRef) {
	if key == "" || cur == nil {
		return
	}
	bucket, ok := m[key]
	if !ok {
		bucket = make(map[string]*workflow.PublishedRootChainRef)
		m[key] = bucket
	}
	if _, exists := bucket[cur.ChainID]; !exists {
		bucket[cur.ChainID] = cur
	}
}

// sortedRefs 取出某键的引用根链列表，按 ChainID 排序保证前端展示顺序稳定。
func sortedRefs(m map[string]map[string]*workflow.PublishedRootChainRef, key string) []*workflow.PublishedRootChainRef {
	bucket, ok := m[key]
	if !ok || len(bucket) == 0 {
		return nil
	}
	out := make([]*workflow.PublishedRootChainRef, 0, len(bucket))
	for _, v := range bucket {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChainID != out[j].ChainID {
			return out[i].ChainID < out[j].ChainID
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// NodePublishedInRootChain 判断节点是否已被发布到根链（当前生效版本，直接放置或经子链传递引用）。
// isAdmin 为 true 时表示超级管理员，不受限制，直接返回 false（可随意编辑/删除）。
func (s *WorkflowService) NodePublishedInRootChain(ctx context.Context, project, nodeID string, isAdmin bool) (bool, error) {
	if isAdmin || nodeID == "" {
		return false, nil
	}
	idx, err := s.buildPublishedRefIndex(ctx, project)
	if err != nil {
		return false, err
	}
	_, ok := idx.nodes[nodeID]
	return ok, nil
}

// ActivityPublishedInRootChain 判断 activity（按 act_namespace+act_name 标识）是否已被发布到根链。
// 节点编排中的 activity 引用以 namespace+name 为准（不依赖 activity_id）。
// isAdmin 为 true 时表示超级管理员，不受限制，直接返回 false（可随意编辑/删除）。
func (s *WorkflowService) ActivityPublishedInRootChain(ctx context.Context, project, actNamespace, actName string, isAdmin bool) (bool, error) {
	if isAdmin || actNamespace == "" || actName == "" {
		return false, nil
	}
	idx, err := s.buildPublishedRefIndex(ctx, project)
	if err != nil {
		return false, err
	}
	_, ok := idx.activities[actNamespace+"\x00"+actName]
	return ok, nil
}

// buildPublishedRefIndex 构建指定项目下「当前生效发布引用集合」：
// 仅遍历 is_current=true（当前生产生效）的发布快照 DSL，收集直接引用的节点与 activity，
// 并对快照中引用的子链做深度遍历（生产执行时子链 DSL 实时加载，同样计入线上内容）。
//
// 注意：以「当前生效版本」为准，历史版本（仅回滚候选，未上线）不计入。
func (s *WorkflowService) buildPublishedRefIndex(ctx context.Context, project string) (*publishedRefIndex, error) {
	idx := &publishedRefIndex{
		nodes:          make(map[string]struct{}),
		activities:     make(map[string]struct{}),
		nodeChains:     make(map[string]map[string]*workflow.PublishedRootChainRef),
		activityChains: make(map[string]map[string]*workflow.PublishedRootChainRef),
	}
	releases, err := s.releaseRepo.ListCurrentByProject(ctx, project)
	if err != nil {
		return nil, err
	}

	// visited 用「根链+子链」复合 key，避免跨根链共用子链时被判定为已访问而漏记归属。
	visited := make(map[string]bool)
	var visit func(dslJSON, nodeIDsCSV, subChainIDsCSV string, cur *workflow.PublishedRootChainRef)
	visit = func(dslJSON, nodeIDsCSV, subChainIDsCSV string, cur *workflow.PublishedRootChainRef) {
		var subChainIDs []string
		if dslJSON != "" {
			nodes, activities, flowSubIDs, ok := parseReleasedDSL(dslJSON)
			if ok {
				for id := range nodes {
					idx.nodes[id] = struct{}{}
					addRef(idx.nodeChains, id, cur)
				}
				for k := range activities {
					idx.activities[k] = struct{}{}
					addRef(idx.activityChains, k, cur)
				}
				subChainIDs = append(subChainIDs, flowSubIDs...)
			} else {
				// DSL 异常（旧数据/格式变化）时回退溯源字段，避免漏判
				for _, id := range splitCSV(nodeIDsCSV) {
					base := baseNodeID(id)
					idx.nodes[base] = struct{}{}
					addRef(idx.nodeChains, base, cur)
				}
			}
		}
		subChainIDs = append(subChainIDs, splitCSV(subChainIDsCSV)...)
		for _, sid := range subChainIDs {
			if sid == "" {
				continue
			}
			vk := sid
			if cur != nil {
				vk = cur.ChainID + "/" + sid
			}
			if visited[vk] {
				continue
			}
			visited[vk] = true
			sc, err := s.subChainRepo.GetByID(ctx, project, sid)
			if err != nil || sc == nil {
				continue
			}
			// 子链内引用仍归属当前遍历的根链（生产执行时被该根链加载）
			visit(sc.DSLJSON, sc.NodeIDs, sc.SubChainIDs, cur)
		}
	}

	for _, rel := range releases {
		if rel == nil {
			continue
		}
		// cur 携带根链信息（名称取发布时快照，无需额外查库）
		cur := &workflow.PublishedRootChainRef{ChainID: rel.ChainID, Name: rel.Name, Version: rel.Version}
		visit(rel.DSLJSON, rel.NodeIDs, rel.SubChainIDs, cur)
	}
	return idx, nil
}

// nodeActivityRefs 解析节点配置中引用的 activity 列表，返回 [namespace, name] 对。
// 与 web 层 extractNodeActivities 保持同一套兼容口径，覆盖以下历史结构：
//   - node_config.activities（二维，按阶段）
//   - node_config.stages（二维）
//   - activities（一维）
//   - node_config 下单个 act_namespace + act_name
//
// 同一节点内重复引用同一 activity 会去重。
func nodeActivityRefs(cfgJSON json.RawMessage) [][2]string {
	if len(cfgJSON) == 0 {
		return nil
	}
	var cfg struct {
		NodeConfig struct {
			Activities [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"activities"`
			Stages [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"stages"`
			ActNamespace string `json:"act_namespace"`
			ActName      string `json:"act_name"`
		} `json:"node_config"`
		Activities []struct {
			ActNamespace string `json:"act_namespace"`
			ActName      string `json:"act_name"`
		} `json:"activities"`
	}
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out [][2]string
	add := func(ns, nm string) {
		if ns == "" || nm == "" {
			return
		}
		key := ns + "\x00" + nm
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, [2]string{ns, nm})
	}
	if len(cfg.NodeConfig.Activities) > 0 {
		for _, stage := range cfg.NodeConfig.Activities {
			for _, a := range stage {
				add(a.ActNamespace, a.ActName)
			}
		}
	} else if len(cfg.NodeConfig.Stages) > 0 {
		for _, stage := range cfg.NodeConfig.Stages {
			for _, a := range stage {
				add(a.ActNamespace, a.ActName)
			}
		}
	} else if len(cfg.Activities) > 0 {
		for _, a := range cfg.Activities {
			add(a.ActNamespace, a.ActName)
		}
	} else if cfg.NodeConfig.ActNamespace != "" && cfg.NodeConfig.ActName != "" {
		add(cfg.NodeConfig.ActNamespace, cfg.NodeConfig.ActName)
	}
	return out
}

// buildActivityRefNodes 构建「activity → 引用它的 Node 列表」索引。
// key 为 act_namespace + "\x00" + act_name，与 publishedRefIndex.activities 一致。
// idx 用于标注每个节点自身是否已发布到根链，从而区分影响面是线上还是草稿。
//
// 与 PublishedRootChains 是两个不同维度：前者是最终生效的根链，这里是直接使用该 activity 的节点，
// 用于修改 activity 前评估影响范围。
func (s *WorkflowService) buildActivityRefNodes(ctx context.Context, project string, idx *publishedRefIndex) (map[string][]*workflow.RefNodeInfo, error) {
	// onlyEnabled=false：禁用的节点未来可能被启用，影响面应包含它们
	nodes, err := s.nodeRepo.List(ctx, project, "", false)
	if err != nil {
		return nil, err
	}
	byActivity := make(map[string]map[string]*workflow.RefNodeInfo)
	for _, n := range nodes {
		if n == nil {
			continue
		}
		for _, pair := range nodeActivityRefs(n.Configuration) {
			aKey := pair[0] + "\x00" + pair[1]
			bucket, ok := byActivity[aKey]
			if !ok {
				bucket = make(map[string]*workflow.RefNodeInfo)
				byActivity[aKey] = bucket
			}
			if _, exists := bucket[n.NodeID]; exists {
				continue
			}
			_, published := idx.nodes[n.NodeID]
			bucket[n.NodeID] = &workflow.RefNodeInfo{NodeID: n.NodeID, Name: n.Name, Published: published}
		}
	}
	// 按 NodeID 排序，保证前端展示顺序稳定
	out := make(map[string][]*workflow.RefNodeInfo, len(byActivity))
	for aKey, bucket := range byActivity {
		list := make([]*workflow.RefNodeInfo, 0, len(bucket))
		for _, v := range bucket {
			list = append(list, v)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].NodeID < list[j].NodeID })
		out[aKey] = list
	}
	return out, nil
}

// NodePublishedRootChains 返回引用了该节点且已发布（当前生效版本）的根链明细列表（含子链传递引用）。
// 列表非空即等价于 NodePublishedInRootChain 为 true；isAdmin 不参与过滤（仅控制能否编辑）。
func (s *WorkflowService) NodePublishedRootChains(ctx context.Context, project, nodeID string) ([]*workflow.PublishedRootChainRef, error) {
	if nodeID == "" {
		return nil, nil
	}
	idx, err := s.buildPublishedRefIndex(ctx, project)
	if err != nil {
		return nil, err
	}
	return sortedRefs(idx.nodeChains, nodeID), nil
}

// parseReleasedDSL 解析发布 DSL，返回其中的节点定义 ID、activity 引用与引用的子链 ID。
// DSL 结构为 rulego RuleChain JSON：{"ruleChain":{...},"metadata":{"nodes":[...],"connections":[...]}}。
func parseReleasedDSL(dslJSON string) (nodes map[string]struct{}, activities map[string]struct{}, subChainIDs []string, ok bool) {
	var doc struct {
		Metadata struct {
			Nodes []struct {
				ID            string          `json:"id"`
				Type          string          `json:"type"`
				Configuration json.RawMessage `json:"configuration"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(dslJSON), &doc); err != nil {
		return nil, nil, nil, false
	}
	nodes = make(map[string]struct{})
	activities = make(map[string]struct{})
	for _, n := range doc.Metadata.Nodes {
		if base := baseNodeID(n.ID); base != "" {
			nodes[base] = struct{}{}
		}
		switch n.Type {
		case "custom/Activity", "activity":
			collectActivityRefs(n.Configuration, activities)
		case "flow":
			if sid := flowSubChainID(n.Configuration); sid != "" {
				subChainIDs = append(subChainIDs, sid)
			}
		}
	}
	return nodes, activities, subChainIDs, true
}

// baseNodeID 去掉 DSL 节点 ID 的实例后缀（baseId__N），返回节点定义 ID。
func baseNodeID(id string) string {
	if i := strings.Index(id, "__"); i >= 0 {
		return id[:i]
	}
	return id
}

// flowSubChainID 从 flow 节点的 ruleChainId（格式 project:chainID）中提取子链 ID。
func flowSubChainID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg struct {
		RuleChainID string `json:"ruleChainId"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	_, id, found := strings.Cut(cfg.RuleChainID, ":")
	if !found {
		return cfg.RuleChainID
	}
	return id
}

// collectActivityRefs 从 custom/Activity 节点配置中收集 activity 引用（namespace+name）。
// 兼容新版 stages 编排、旧版 activities 数组与单 activity 直配（node_config.act_namespace/act_name）。
func collectActivityRefs(raw json.RawMessage, out map[string]struct{}) {
	if len(raw) == 0 || !json.Valid(raw) {
		return
	}
	var cfg struct {
		NodeConfig struct {
			ActNamespace string `json:"act_namespace"`
			ActName      string `json:"act_name"`
			Stages       [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"stages"`
			Activities [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"activities"`
		} `json:"node_config"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return
	}
	add := func(ns, name string) {
		if ns != "" && name != "" {
			out[ns+"\x00"+name] = struct{}{}
		}
	}
	add(cfg.NodeConfig.ActNamespace, cfg.NodeConfig.ActName)
	for _, group := range cfg.NodeConfig.Stages {
		for _, it := range group {
			add(it.ActNamespace, it.ActName)
		}
	}
	for _, group := range cfg.NodeConfig.Activities {
		for _, it := range group {
			add(it.ActNamespace, it.ActName)
		}
	}
}

// splitCSV 将逗号分隔的 ID 串拆分为非空列表。
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
