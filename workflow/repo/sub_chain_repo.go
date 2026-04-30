package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// SubChainRepo 子规则链仓储，实现 workflow.SubChainStore 接口。
type SubChainRepo struct {
	db *gorm.DB
}

// NewSubChainRepo 创建子链仓储实例。
func NewSubChainRepo(db *gorm.DB) *SubChainRepo {
	return &SubChainRepo{db: db}
}

// NextSubChainID 生成下一个子链的自动 ID（如 F000012），基于 max(id)+1。
func (r *SubChainRepo) NextSubChainID(ctx context.Context) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_sub_chains").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("F%06d", next), nil
}

// Create 创建子链。
func (r *SubChainRepo) Create(ctx context.Context, def *workflow.SubChainDef) error {
	var m models.SubChainModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// BatchUpsert 批量 upsert 子链：project + chain_id 冲突时更新全部字段，否则插入。
func (r *SubChainRepo) BatchUpsert(ctx context.Context, defs []*workflow.SubChainDef) error {
	if len(defs) == 0 {
		return nil
	}
	modelsList := make([]models.SubChainModel, 0, len(defs))
	for _, def := range defs {
		var m models.SubChainModel
		m.FromDef(def)
		modelsList = append(modelsList, m)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project"}, {Name: "chain_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "description", "dsl_json", "status",
			"node_ids", "sub_chain_ids", "connections_data", "node_param_overrides",
		}),
	}).Create(&modelsList).Error
}

// GetByID 按项目 + 子链 ID 查询。
func (r *SubChainRepo) GetByID(ctx context.Context, project, chainID string) (*workflow.SubChainDef, error) {
	var m models.SubChainModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ? AND status = ?", project, chainID, models.NodeStatusEnabled).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrSubChainNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListByIDs 按项目 + 子链 ID 列表批量查询。
func (r *SubChainRepo) ListByIDs(ctx context.Context, project string, chainIDs []string) ([]*workflow.SubChainDef, error) {
	if len(chainIDs) == 0 {
		return nil, nil
	}
	var modelsList []models.SubChainModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_id IN ? AND status = ?", project, chainIDs, models.NodeStatusEnabled).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.SubChainDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// List 列出指定项目下的子链。onlyEnabled=true 时仅返回启用状态的子链（用于编排选择）；
// onlyEnabled=false 时返回全部（含禁用，用于管理列表展示）。
func (r *SubChainRepo) List(ctx context.Context, project string, onlyEnabled bool) ([]*workflow.SubChainDef, error) {
	var modelsList []models.SubChainModel
	query := r.db.WithContext(ctx).
		Where("project = ?", project)
	if onlyEnabled {
		query = query.Where("status = ?", models.NodeStatusEnabled)
	}
	err := query.Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.SubChainDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Update 更新子链。
func (r *SubChainRepo) Update(ctx context.Context, def *workflow.SubChainDef) error {
	result := r.db.WithContext(ctx).
		Model(&models.SubChainModel{}).
		Where("project = ? AND chain_id = ?", def.Project, def.ChainID).
		Updates(map[string]interface{}{
			"name":               def.Name,
			"description":        def.Description,
			"dsl_json":           def.DSLJSON,
			"status":             def.Status,
			"node_ids":           def.NodeIDs,
			"sub_chain_ids":      def.SubChainIDs,
			"connections_data":    def.ConnectionsData,
			"node_param_overrides": def.NodeParamOverrides,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrSubChainNotFound
	}
	return nil
}

// Delete 软删除子链（按 project + chainID）。
func (r *SubChainRepo) Delete(ctx context.Context, project, chainID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ?", project, chainID).
		Delete(&models.SubChainModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrSubChainNotFound
	}
	return nil
}
