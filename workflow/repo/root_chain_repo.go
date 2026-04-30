package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-utils/utils"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// RootChainRepo 根规则链仓储，实现 workflow.RootChainStore 接口。
type RootChainRepo struct {
	db *gorm.DB
}

// NewRootChainRepo 创建根链仓储实例。
func NewRootChainRepo(db *gorm.DB) *RootChainRepo {
	return &RootChainRepo{db: db}
}

// Create 创建根链。
// 创建成功后读取数据库自增 ID，生成 R000001 格式的 ChainID 回写；
// 若入参 ChainID 为空则填充该 ChainID；若 ChainKey 为空则默认等于 ChainID。
func (r *RootChainRepo) Create(ctx context.Context, def *workflow.RootChainDef) error {
	var m models.RootChainModel
	m.FromDef(def)

	// 插入前若 ChainID 为空，先用预估的自增 ID 生成占位，满足唯一索引/非空约束
	m.ChainID = utils.RandomString(5)

	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return err
	}

	// 插入后取真实自增 ID，生成最终 ChainID（格式 R000012）
	finalID := "R" + utils.PadLeft(fmt.Sprintf("%d", m.ID), 6, '0')

	updates := map[string]interface{}{}
	updates["chain_id"] = finalID
	m.ChainID = finalID
	def.ChainID = finalID

	// ChainKey 为空则默认等于 ChainID
	if def.ChainKey == "" {
		chainKey := fmt.Sprintf("%s-%s", finalID, utils.RandomString(5))
		m.ChainKey = chainKey
		def.ChainKey = chainKey
		updates["chain_key"] = chainKey
	} else if def.ChainKey != m.ChainKey {
		// 调用方指定了 ChainKey，同步回填到 def
		def.ChainKey = m.ChainKey
	}

	if len(updates) > 0 {
		if err := r.db.WithContext(ctx).
			Model(&models.RootChainModel{}).
			Where("id = ?", m.ID).
			Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetByKey 按项目+ChainKey 查询根链（project 与 chain_key 联合唯一，用于以业务键直接调用主链）。
func (r *RootChainRepo) GetByKey(ctx context.Context, project, chainKey string) (*workflow.RootChainDef, error) {
	var m models.RootChainModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_key = ? AND status = ?", project, chainKey, models.NodeStatusEnabled).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrRootChainNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// GetByID 按项目 + 根链 ID 查询。
func (r *RootChainRepo) GetByID(ctx context.Context, chainID string) (*workflow.RootChainDef, error) {
	var m models.RootChainModel
	err := r.db.WithContext(ctx).
		Where("chain_id = ? AND status = ?", chainID, models.NodeStatusEnabled).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrRootChainNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// List 列出指定项目下所有启用的根链。
func (r *RootChainRepo) List(ctx context.Context, project string) ([]*workflow.RootChainDef, error) {
	var modelsList []models.RootChainModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND status = ?", project, models.NodeStatusEnabled).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.RootChainDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Update 更新根链。
func (r *RootChainRepo) Update(ctx context.Context, def *workflow.RootChainDef) error {
	result := r.db.WithContext(ctx).
		Model(&models.RootChainModel{}).
		Where("project = ? AND chain_id = ?", def.Project, def.ChainID).
		Updates(map[string]interface{}{
			"name":                 def.Name,
			"description":          def.Description,
			"dsl_json":             def.DSLJSON,
			"status":               def.Status,
			"node_ids":             def.NodeIDs,
			"sub_chain_ids":        def.SubChainIDs,
			"connections_data":     def.ConnectionsData,
			"node_param_overrides": def.NodeParamOverrides,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrRootChainNotFound
	}
	return nil
}

// Delete 物理删除根链（按 project + chainID）。
// 注意：必须使用 Unscoped 物理删除而非软删除——唯一索引 uk_project_chain_id 不包含
// deleted_at，软删除的旧行仍占用唯一键，会导致同 ID 更新/重建时报 duplicate entry。
// 历史追溯由 wf_root_chain_releases 发布快照表承担。
func (r *RootChainRepo) Delete(ctx context.Context, project, chainID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ?", project, chainID).
		Unscoped().
		Delete(&models.RootChainModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrRootChainNotFound
	}
	return nil
}
