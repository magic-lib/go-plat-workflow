package repo

import (
	"context"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"time"

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

// StatsByDay 按 node + 天聚合统计每个 node 每天的访问量（总条数）与错误量（error_msg 非空条数）。
// days 表示统计最近 N 天（基于 created_at）；env 为空表示不限定环境；nodeID 为空表示统计全部 node。
// 返回结果按日期升序、node_id 升序排序，便于前端按日期横轴、node 分系列绘制。
func (r *NodeLogRepo) StatsByDay(ctx context.Context, project, env, nodeID string, days int) ([]workflow.NodeLogDayStat, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	since := time.Now().AddDate(0, 0, -(days-1)).Format("2006-01-02") + " 00:00:00"

	type row struct {
		NodeID   string `gorm:"column:node_id"`
		NodeName string `gorm:"column:node_name"`
		Date     string `gorm:"column:date"`
		Total    int64  `gorm:"column:total"`
		Errors   int64  `gorm:"column:errors"`
	}
	var rows []row
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{}).
		Select("node_id, node_name, DATE(created_at) as date, COUNT(*) as total, "+
			"SUM(CASE WHEN error_msg <> '' THEN 1 ELSE 0 END) as errors").
		Where("project = ? AND created_at >= ?", project, since)
	if env != "" {
		query = query.Where("env = ?", env)
	}
	if nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}
	if err := query.Group("node_id, node_name, DATE(created_at)").
		Order("date ASC, node_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	stats := make([]workflow.NodeLogDayStat, 0, len(rows))
	for _, rw := range rows {
		stats = append(stats, workflow.NodeLogDayStat{
			NodeID:   rw.NodeID,
			NodeName: rw.NodeName,
			Date:     rw.Date,
			Total:    rw.Total,
			Errors:   rw.Errors,
		})
	}
	return stats, nil
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
		query = query.Where("trace_id = ?", id.GetUUID(f.TraceID))
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
