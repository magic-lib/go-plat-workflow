package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// subChainRootRef 根链对子链的引用：草稿里的 sub_chain_ids 加上当前发布版本快照里的 sub_chain_ids。
type subChainRootRef struct {
	ChainID     string
	Name        string
	SubChainIDs []string
}

// checkSubChainNotReferenced 子链被未停用的根链引用（直接引用，或经其他子链嵌套引用）时返回
// ErrSubChainReferencedByRootChain，用于禁止删除/停用子链，避免根链执行时调不到子链。
// 根链草稿和当前发布版本都算引用：正式执行读的是发布快照里的 sub_chain_ids。
func (s *WorkflowService) checkSubChainNotReferenced(ctx context.Context, project, chainID string) error {
	roots, err := s.rootChainRepo.List(ctx, project) // 只返回启用（未停用）的根链
	if err != nil {
		return fmt.Errorf("list root chains: %w", err)
	}
	if len(roots) == 0 {
		return nil
	}
	refs := make([]*subChainRootRef, 0, len(roots))
	refByID := make(map[string]*subChainRootRef, len(roots))
	for _, rc := range roots {
		ref := &subChainRootRef{ChainID: rc.ChainID, Name: rc.Name, SubChainIDs: splitSubChainBaseIDs(rc.SubChainIDs)}
		refs = append(refs, ref)
		refByID[rc.ChainID] = ref
	}
	releases, err := s.releaseRepo.ListCurrentByProject(ctx, project)
	if err != nil {
		return fmt.Errorf("list current releases: %w", err)
	}
	for _, rel := range releases {
		if ref, ok := refByID[rel.ChainID]; ok {
			ref.SubChainIDs = append(ref.SubChainIDs, splitSubChainBaseIDs(rel.SubChainIDs)...)
		}
	}
	nested, err := s.subChainNestedRefs(ctx, project)
	if err != nil {
		return err
	}

	hits := rootChainsReferencingSubChain(chainID, refs, nested)
	if len(hits) == 0 {
		return nil
	}
	return fmt.Errorf("子链 %s 被以下未停用的根链引用，不能删除或停用，请先在根链中移除该子链或停用根链：%s（%w）",
		chainID, strings.Join(hits, "、"), workflow.ErrSubChainReferencedByRootChain)
}

// checkSubChainPublishedChange 子链被根链的当前发布版本引用（直接或经嵌套子链）时返回 ErrSubChainPublishedNeedConfirm。
// 子链内容在保存根链时嵌入根链 DSL，修改子链不会影响已发布版本；保存前提示调用方：
// 这些根链需重新编辑保存并发布才会用上本次修改（confirmed=true 时放行）。
func (s *WorkflowService) checkSubChainPublishedChange(ctx context.Context, project, chainID string, confirmed bool) error {
	if confirmed || chainID == "" {
		return nil
	}
	releases, err := s.releaseRepo.ListCurrentByProject(ctx, project)
	if err != nil {
		return fmt.Errorf("list current releases: %w", err)
	}
	if len(releases) == 0 {
		return nil
	}
	refs := make([]*subChainRootRef, 0, len(releases))
	for _, rel := range releases {
		if rel == nil {
			continue
		}
		refs = append(refs, &subChainRootRef{
			ChainID:     rel.ChainID,
			Name:        fmt.Sprintf("%s（v%d）", rel.Name, rel.Version),
			SubChainIDs: splitSubChainBaseIDs(rel.SubChainIDs),
		})
	}
	nested, err := s.subChainNestedRefs(ctx, project)
	if err != nil {
		return err
	}
	hits := rootChainsReferencingSubChain(chainID, refs, nested)
	if len(hits) == 0 {
		return nil
	}
	return fmt.Errorf("子链 %s 被以下已发布根链引用：%s。根链使用的是保存根链时的子链内容，本次修改不影响已发布版本，需在根链编排页重新保存并发布后才会生效（%w）",
		chainID, strings.Join(hits, "、"), workflow.ErrSubChainPublishedNeedConfirm)
}

// subChainNestedRefs 返回子链 ID -> 该子链引用的子链 ID 列表（含停用子链，已去实例后缀）。
func (s *WorkflowService) subChainNestedRefs(ctx context.Context, project string) (map[string][]string, error) {
	subs, err := s.subChainRepo.List(ctx, project, false)
	if err != nil {
		return nil, fmt.Errorf("list sub chains: %w", err)
	}
	nested := make(map[string][]string, len(subs))
	for _, sc := range subs {
		nested[sc.ChainID] = splitSubChainBaseIDs(sc.SubChainIDs)
	}
	return nested, nil
}

// splitSubChainBaseIDs 拆分逗号分隔的 sub_chain_ids，并去掉实例后缀（F000001__ab12c → F000001）。
func splitSubChainBaseIDs(csv string) []string {
	ids := splitCSV(csv)
	for i, id := range ids {
		ids[i] = baseNodeID(id)
	}
	return ids
}

// rootChainsReferencingSubChain 返回直接或经嵌套子链引用了 target 的根链（形如 "R000002 名称"，按 ID 排序）。
// nested 为子链 ID -> 该子链引用的子链 ID 列表；visited 防止子链循环引用导致死循环。
func rootChainsReferencingSubChain(target string, roots []*subChainRootRef, nested map[string][]string) []string {
	var hits []string
	for _, rc := range roots {
		visited := make(map[string]bool)
		queue := append([]string(nil), rc.SubChainIDs...)
		found := false
		for len(queue) > 0 && !found {
			id := queue[0]
			queue = queue[1:]
			if visited[id] {
				continue
			}
			visited[id] = true
			if id == target {
				found = true
				break
			}
			queue = append(queue, nested[id]...)
		}
		if found {
			label := rc.ChainID
			if rc.Name != "" {
				label += " " + rc.Name
			}
			hits = append(hits, label)
		}
	}
	sort.Strings(hits)
	return hits
}
