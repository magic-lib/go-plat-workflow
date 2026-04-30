package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// NodeLogRepo node 运行日志仓储，实现 workflow.NodeLogStore 接口。
type NodeLogRepo struct {
	db *gorm.DB
}

// NewNodeLogRepo 创建 node 运行日志仓储实例。
func NewNodeLogRepo(db *gorm.DB) *NodeLogRepo {
	return &NodeLogRepo{db: db}
}

// Create 创建一条 node 运行日志。
func (r *NodeLogRepo) Create(ctx context.Context, def *workflow.NodeLogDef) error {
	var m models.NodeLogModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// ListByNode 列出指定 node 的运行日志（按时间倒序），支持分页。
func (r *NodeLogRepo) ListByNode(ctx context.Context, project, nodeID string, limit, offset int) ([]*workflow.NodeLogDef, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{})
	query = query.Where("project = ? AND node_id = ?", project, nodeID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var modelsList []models.NodeLogModel
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&modelsList).Error; err != nil {
		return nil, 0, err
	}
	defs := make([]*workflow.NodeLogDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, total, nil
}

// ListByFilter 按条件全局查询 node 运行日志（按时间倒序），支持分页。
// 支持 level/node_id/env/trace_id 精确匹配，node_name/keyword 模糊匹配。
func (r *NodeLogRepo) ListByFilter(ctx context.Context, project string, f *workflow.NodeLogFilter) ([]*workflow.NodeLogDef, int64, error) {
	if f == nil {
		f = &workflow.NodeLogFilter{}
	}
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{})
	query = query.Where("project = ?", project)
	if f.Level != "" {
		query = query.Where("level = ?", f.Level)
	}
	if f.NodeID != "" {
		query = query.Where("node_id = ?", f.NodeID)
	}
	if f.NodeName != "" {
		query = query.Where("node_name LIKE ?", "%"+f.NodeName+"%")
	}
	if f.Env != "" {
		query = query.Where("env = ?", f.Env)
	}
	if f.TraceID != "" {
		query = query.Where("trace_id = ?", f.TraceID)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		query = query.Where("payload LIKE ? OR result LIKE ? OR error_msg LIKE ?", kw, kw, kw)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	offset := f.Offset
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var modelsList []models.NodeLogModel
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&modelsList).Error; err != nil {
		return nil, 0, err
	}
	defs := make([]*workflow.NodeLogDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, total, nil
}
