package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// ActivityLogRepo activity 执行日志仓储，实现 workflow.ActivityLogStore 接口。
type ActivityLogRepo struct {
	db *gorm.DB
}

// NewActivityLogRepo 创建 activity 日志仓储实例。
func NewActivityLogRepo(db *gorm.DB) *ActivityLogRepo {
	return &ActivityLogRepo{db: db}
}

// Create 创建一条执行日志。
func (r *ActivityLogRepo) Create(ctx context.Context, def *workflow.ActivityLogDef) error {
	var m models.ActivityLogModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// ListByActivity 列出指定 activity 的执行日志（按时间倒序），支持按字段过滤与关键词搜索。
// actName 为活动的 act_name（活动唯一标识），用于按当前 activity 限定范围；
// filter 中可包含：level（精确）、act_namespace、act_name、event_id（精确）、
// keyword（模糊匹配 payload/result/error_msg）、start/end（unix 秒时间范围）。
func (r *ActivityLogRepo) ListByActivity(ctx context.Context, project, actName string, filter *workflow.ActivityLogFilter) ([]*workflow.ActivityLogDef, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.ActivityLogModel{})
	query = query.Where("project = ?", project)
	if actName != "" {
		query = query.Where("act_name = ?", actName)
	}
	if filter != nil {
		if filter.Level != "" {
			query = query.Where("level = ?", filter.Level)
		}
		if filter.ActNamespace != "" {
			query = query.Where("act_namespace = ?", filter.ActNamespace)
		}
		if filter.ActName != "" {
			query = query.Where("act_name = ?", filter.ActName)
		}
		if filter.EventID != "" {
			query = query.Where("event_id = ?", filter.EventID)
		}
		if filter.Env != "" {
			query = query.Where("env = ?", filter.Env)
		}
		if filter.Keyword != "" {
			kw := "%" + filter.Keyword + "%"
			query = query.Where("payload LIKE ? OR result LIKE ? OR error_msg LIKE ?", kw, kw, kw)
		}
		if filter.Start > 0 {
			query = query.Where("ts >= ?", filter.Start)
		}
		if filter.End > 0 {
			query = query.Where("ts <= ?", filter.End)
		}
	}
	// 总条数（不受分页影响）
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var modelsList []models.ActivityLogModel
	err := query.Order("id DESC").
		Limit(filterLimit(filter)).
		Offset(filterOffset(filter)).
		Find(&modelsList).Error
	if err != nil {
		return nil, 0, err
	}
	defs := make([]*workflow.ActivityLogDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, total, nil
}

func filterLimit(filter *workflow.ActivityLogFilter) int {
	if filter == nil || filter.Limit <= 0 {
		return 50
	}
	if filter.Limit > 1000 {
		return 1000
	}
	return filter.Limit
}

func filterOffset(filter *workflow.ActivityLogFilter) int {
	if filter == nil || filter.Offset < 0 {
		return 0
	}
	return filter.Offset
}

// DeleteByActivity 删除指定 activity 的全部日志（按 project + act_name）。
func (r *ActivityLogRepo) DeleteByActivity(ctx context.Context, project, actName string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND act_name = ?", project, actName).
		Delete(&models.ActivityLogModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("activity log not found")
	}
	return nil
}
