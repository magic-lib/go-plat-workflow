package workflow

import (
	"context"

	"github.com/magic-lib/go-plat-workflow/workflow/engine"
)

// engineRootChainAdapter 将 *repo.RootChainRepo 适配为 engine.RootChainStore。
// engine 包不能反向 import 父包 workflow（否则循环引用），
// 因此由本包提供适配器，把 workflow.RootChainDef 转换为 engine.RootChainDef。
type engineRootChainAdapter struct {
	repo RootChainStore
}

// GetByKey 按项目+ChainKey 查询根链。
func (a *engineRootChainAdapter) GetByKey(ctx context.Context, project, chainKey string) (*engine.RootChainDef, error) {
	def, err := a.repo.GetByKey(ctx, project, chainKey)
	if err != nil {
		return nil, err
	}
	return &engine.RootChainDef{
		ChainID:     def.ChainID,
		DSLJSON:     def.DSLJSON,
		SubChainIDs: def.SubChainIDs,
	}, nil
}

func (a *engineRootChainAdapter) GetByID(ctx context.Context, chainID string) (*engine.RootChainDef, error) {
	def, err := a.repo.GetByID(ctx, chainID)
	if err != nil {
		return nil, err
	}
	return &engine.RootChainDef{
		ChainID:     def.ChainID,
		DSLJSON:     def.DSLJSON,
		SubChainIDs: def.SubChainIDs,
	}, nil
}

// engineSubChainAdapter 将 *repo.SubChainRepo 适配为 engine.SubChainStore。
type engineSubChainAdapter struct {
	repo SubChainStore
}

func (a *engineSubChainAdapter) GetByID(ctx context.Context, project, chainID string) (*engine.SubChainDef, error) {
	def, err := a.repo.GetByID(ctx, project, chainID)
	if err != nil {
		return nil, err
	}
	return &engine.SubChainDef{
		DSLJSON:     def.DSLJSON,
		SubChainIDs: def.SubChainIDs,
	}, nil
}

// NewEngineRootChainStore 构造 engine 可用的 RootChainStore 适配器。
// 导出供 workflow/service 等子包使用（同包内亦可无前缀调用）。
func NewEngineRootChainStore(repo RootChainStore) engine.RootChainStore {
	return &engineRootChainAdapter{repo: repo}
}

// NewEngineSubChainStore 构造 engine 可用的 SubChainStore 适配器。
func NewEngineSubChainStore(repo SubChainStore) engine.SubChainStore {
	return &engineSubChainAdapter{repo: repo}
}
