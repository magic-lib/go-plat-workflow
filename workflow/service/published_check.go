package service

import (
	"context"
	"encoding/json"
	"strings"
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
		nodes:      make(map[string]struct{}),
		activities: make(map[string]struct{}),
	}
	releases, err := s.releaseRepo.ListCurrentByProject(ctx, project)
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	var visit func(dslJSON, nodeIDsCSV, subChainIDsCSV string)
	visit = func(dslJSON, nodeIDsCSV, subChainIDsCSV string) {
		var subChainIDs []string
		if dslJSON != "" {
			nodes, activities, flowSubIDs, ok := parseReleasedDSL(dslJSON)
			if ok {
				for id := range nodes {
					idx.nodes[id] = struct{}{}
				}
				for k := range activities {
					idx.activities[k] = struct{}{}
				}
				subChainIDs = append(subChainIDs, flowSubIDs...)
			} else {
				// DSL 异常（旧数据/格式变化）时回退溯源字段，避免漏判
				for _, id := range splitCSV(nodeIDsCSV) {
					idx.nodes[baseNodeID(id)] = struct{}{}
				}
			}
		}
		subChainIDs = append(subChainIDs, splitCSV(subChainIDsCSV)...)
		for _, sid := range subChainIDs {
			if sid == "" || visited[sid] {
				continue
			}
			visited[sid] = true
			sc, err := s.subChainRepo.GetByID(ctx, project, sid)
			if err != nil || sc == nil {
				continue
			}
			visit(sc.DSLJSON, sc.NodeIDs, sc.SubChainIDs)
		}
	}

	for _, rel := range releases {
		if rel == nil {
			continue
		}
		visit(rel.DSLJSON, rel.NodeIDs, rel.SubChainIDs)
	}
	return idx, nil
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
