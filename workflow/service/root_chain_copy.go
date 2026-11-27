package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/rs/zerolog/log"
)

// rootChainCopySuffix 复制根链时新根链名称的后缀。
const rootChainCopySuffix = "-副本"

// CopyRootChain 复制一条根链：新建一条根链（新的 ChainID / ChainKey，名称加「-副本」），
// 草稿内容（DSL、引用节点/子链、连线、各节点参数/路由/名称/收起覆盖、根节点返回值定义）与原链一致，
// DSL 中的 ruleChain.id / name 改为新根链的 ID 与名称；发布记录不复制，新根链需自行发布。
func (s *WorkflowService) CopyRootChain(ctx context.Context, project, chainID string) (*workflow.RootChainDef, error) {
	src, err := s.rootChainRepo.GetByProjectID(ctx, project, chainID)
	if err != nil {
		return nil, fmt.Errorf("get root chain %s failed: %w", chainID, err)
	}
	dst := *src
	dst.ChainID = ""
	dst.ChainKey = ""
	dst.Name = src.Name + rootChainCopySuffix
	dst.DSLJSON = "{}" // ChainID 创建后才确定，先建记录再回写 DSL
	dst.HasReleases = false
	dst.MustInputParams = nil
	if err := s.rootChainRepo.Create(ctx, &dst); err != nil {
		return nil, fmt.Errorf("create root chain copy failed: %w", err)
	}

	dsl, err := rewriteRootChainDSLIdentity(src.DSLJSON, dst.ChainID, dst.Name)
	if err == nil {
		dst.DSLJSON = dsl
		err = s.rootChainRepo.Update(ctx, &dst)
	}
	if err != nil {
		// 回写失败：删除刚建的半成品，避免留下只有基本信息的空链
		if delErr := s.rootChainRepo.Delete(ctx, project, dst.ChainID); delErr != nil {
			log.Ctx(ctx).Warn().Err(delErr).Str("chain_id", dst.ChainID).Msg("delete incomplete root chain copy failed")
		}
		return nil, fmt.Errorf("copy root chain %s failed: %w", chainID, err)
	}
	log.Ctx(ctx).Info().Str("project", project).Str("from", chainID).Str("chain_id", dst.ChainID).Msg("root chain copied")
	return &dst, nil
}

// rewriteRootChainDSLIdentity 把根链 DSL 中 ruleChain.id / name 改为新根链的 ID 与名称，其余内容原样保留。
// 原 DSL 为空时返回 "{}"。
func rewriteRootChainDSLIdentity(dslJSON, chainID, name string) (string, error) {
	if strings.TrimSpace(dslJSON) == "" || strings.TrimSpace(dslJSON) == "{}" {
		return "{}", nil
	}
	dsl := make(map[string]any)
	if err := json.Unmarshal([]byte(dslJSON), &dsl); err != nil {
		return "", fmt.Errorf("parse root chain dsl failed: %w", err)
	}
	if rc, ok := dsl["ruleChain"].(map[string]any); ok {
		rc["id"] = chainID
		rc["name"] = name
	}
	b, err := json.Marshal(dsl)
	if err != nil {
		return "", fmt.Errorf("marshal root chain dsl failed: %w", err)
	}
	return string(b), nil
}
