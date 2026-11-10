package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// SaveSubChainBuild 编排方式保存子链：ChainID 对应的子链已存在则更新，否则新增（ChainID 为空时自动生成）。
// 更新时若子链被已发布根链引用且编排内容有变化，需 req.ConfirmPublished=true 才保存，
// 否则返回 ErrSubChainPublishedNeedConfirm（错误信息里列出需要重新保存并发布的根链）。
func (s *WorkflowService) SaveSubChainBuild(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	if req.ChainID == "" {
		return s.CreateSubChainBuild(ctx, req)
	}
	existing, err := s.subChainRepo.GetByID(ctx, req.Project, req.ChainID)
	if err != nil {
		if errors.Is(err, workflow.ErrSubChainNotFound) {
			return s.CreateSubChainBuild(ctx, req)
		}
		return nil, fmt.Errorf("get sub chain %s: %w", req.ChainID, err)
	}
	def, err := s.buildSubChainDef(ctx, req)
	if err != nil {
		return nil, err
	}
	// 编排保存不改启停状态，沿用原状态
	def.Status = existing.Status
	if subChainDSLChanged(existing.DSLJSON, def.DSLJSON) {
		if err := s.checkSubChainPublishedChange(ctx, req.Project, req.ChainID, req.ConfirmPublished); err != nil {
			return nil, err
		}
	}
	if err := s.subChainRepo.Update(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// UpdateSubChainWithConfirm 同 UpdateSubChain；子链被已发布根链引用且 DSL 有变化时，
// confirmed=false 返回 ErrSubChainPublishedNeedConfirm，confirmed=true 放行。
func (s *WorkflowService) UpdateSubChainWithConfirm(ctx context.Context, def *workflow.SubChainDef, confirmed bool) error {
	existing, err := s.subChainRepo.GetByID(ctx, def.Project, def.ChainID)
	if err != nil && !errors.Is(err, workflow.ErrSubChainNotFound) {
		return fmt.Errorf("get sub chain %s: %w", def.ChainID, err)
	}
	if existing != nil {
		mergeSubChainOrchFields(def, existing)
		normalizeSubChainDSL(def)
		if subChainDSLChanged(existing.DSLJSON, def.DSLJSON) {
			if err := s.checkSubChainPublishedChange(ctx, def.Project, def.ChainID, confirmed); err != nil {
				return err
			}
		}
	}
	return s.UpdateSubChain(ctx, def)
}

// mergeSubChainOrchFields 子链管理弹框只提交 name/description/status/dsl_json，
// 请求里没带的编排字段（节点、子链、连线、各类覆盖）沿用库里原值，避免整行更新时被清空：
// 清空后编排页无法恢复，sub_chain_ids 丢失还会让删除/停用、已发布引用检查漏判嵌套引用。
// 手动改了 DSL 时，sub_chain_ids 按 DSL 里的 flow 节点重新计算，保证引用检查以实际执行内容为准。
func mergeSubChainOrchFields(def, existing *workflow.SubChainDef) {
	if strings.TrimSpace(def.DSLJSON) == "" {
		def.DSLJSON = existing.DSLJSON
	}
	keep := func(dst *string, old string) {
		if strings.TrimSpace(*dst) == "" {
			*dst = old
		}
	}
	keep(&def.NodeIDs, existing.NodeIDs)
	keep(&def.ConnectionsData, existing.ConnectionsData)
	keep(&def.NodeParamOverrides, existing.NodeParamOverrides)
	keep(&def.NodeSwitchOverrides, existing.NodeSwitchOverrides)
	keep(&def.NodeNameOverrides, existing.NodeNameOverrides)
	keep(&def.NodeCollapseOverrides, existing.NodeCollapseOverrides)
	if def.SubChainIDs != "" {
		return
	}
	if !subChainDSLChanged(existing.DSLJSON, def.DSLJSON) {
		def.SubChainIDs = existing.SubChainIDs
		return
	}
	if ids, ok := flowSubChainInstanceIDs(def.DSLJSON); ok {
		def.SubChainIDs = strings.Join(ids, ",")
	} else {
		def.SubChainIDs = existing.SubChainIDs
	}
}

// flowSubChainInstanceIDs 按 DSL 中 flow 节点计算 sub_chain_ids：节点 ID 是该子链的实例 ID（子链ID 或 子链ID__后缀）时用节点 ID，
// 否则（手写的任意节点 ID）用引用的子链 ID。DSL 解析失败返回 ok=false。
func flowSubChainInstanceIDs(dslJSON string) ([]string, bool) {
	var chain struct {
		Metadata struct {
			Nodes []struct {
				ID            string          `json:"id"`
				Type          string          `json:"type"`
				Configuration json.RawMessage `json:"configuration"`
			} `json:"nodes"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(dslJSON), &chain); err != nil {
		return nil, false
	}
	var ids []string
	seen := make(map[string]bool)
	for _, n := range chain.Metadata.Nodes {
		if n.Type != "flow" {
			continue
		}
		sid := flowSubChainID(n.Configuration)
		if sid == "" {
			continue
		}
		id := sid
		if baseNodeID(n.ID) == sid {
			id = n.ID
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, true
}

// subChainDSLChanged 判断子链 DSL 的执行内容是否变化：按 JSON 语义比较（数据库 json 列会重排键顺序），
// 忽略 ruleChain.name（只改名称不影响执行）。解析失败时按字符串比较。
func subChainDSLChanged(oldDSL, newDSL string) bool {
	var a, b map[string]interface{}
	if json.Unmarshal([]byte(oldDSL), &a) != nil || json.Unmarshal([]byte(newDSL), &b) != nil {
		return oldDSL != newDSL
	}
	for _, m := range []map[string]interface{}{a, b} {
		if rc, ok := m["ruleChain"].(map[string]interface{}); ok {
			delete(rc, "name")
		}
	}
	return !reflect.DeepEqual(a, b)
}
