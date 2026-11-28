package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// NodeTestRecordRepo 单节点测试记录仓储，实现 workflow.NodeTestRecordStore 接口。
type NodeTestRecordRepo struct {
	db *gorm.DB
}

// NewNodeTestRecordRepo 创建测试记录仓储实例。
func NewNodeTestRecordRepo(db *gorm.DB) *NodeTestRecordRepo {
	return &NodeTestRecordRepo{db: db}
}

// NextRecordID 预估下一个测试记录自动 ID（如 M000001），基于 max(id)+1，仅供展示参考。
// 实际创建时编号由 Create 按自增主键生成，不要用它的结果去创建测试记录。
func (r *NodeTestRecordRepo) NextRecordID(ctx context.Context, project string) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_node_test_records").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("M%06d", next), nil
}

// Create 创建测试记录。RecordID 为空（或为临时编号）时先插入再按自增主键回写 M000012 格式的编号，并回填到 def。
func (r *NodeTestRecordRepo) Create(ctx context.Context, def *workflow.NodeTestRecordDef) error {
	var m models.NodeTestRecordModel
	m.FromDef(def)
	if !needAutoBizID(def.RecordID) {
		return r.db.WithContext(ctx).Create(&m).Error
	}
	tempID := workflow.NewTempBizID()
	m.RecordID = tempID
	finalID, err := createWithAutoBizID(ctx, r.db, bizIDCreate{
		model: &m, column: "record_id", prefix: "M", tempID: tempID,
		id: func() uint { return m.ID },
	})
	if err != nil {
		return err
	}
	def.RecordID = finalID
	return nil
}

// GetByID 按项目 + 测试记录 ID 查询。
func (r *NodeTestRecordRepo) GetByID(ctx context.Context, project, recordID string) (*workflow.NodeTestRecordDef, error) {
	var m models.NodeTestRecordModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND record_id = ?", project, recordID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrRootChainNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListByNode 列出指定节点下所有测试记录，按创建时间倒序。
func (r *NodeTestRecordRepo) ListByNode(ctx context.Context, project, nodeID string) ([]*workflow.NodeTestRecordDef, error) {
	var modelsList []models.NodeTestRecordModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND node_id = ?", project, nodeID).
		Order("id DESC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.NodeTestRecordDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Delete 删除测试记录（按 project + record_id）。
func (r *NodeTestRecordRepo) Delete(ctx context.Context, project, recordID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND record_id = ?", project, recordID).
		Delete(&models.NodeTestRecordModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrRootChainNotFound
	}
	return nil
}

// DeleteByNode 删除指定节点下的所有测试记录（按 project + node_id 批量清除）。
func (r *NodeTestRecordRepo) DeleteByNode(ctx context.Context, project, nodeID string) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("project = ? AND node_id = ?", project, nodeID).
		Delete(&models.NodeTestRecordModel{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
