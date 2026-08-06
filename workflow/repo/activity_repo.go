package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// ActivityRepo activity 模板仓储，实现 workflow.ActivityStore 接口。
type ActivityRepo struct {
	db *gorm.DB
}

// NewActivityRepo 创建 activity 仓储实例。
func NewActivityRepo(db *gorm.DB) *ActivityRepo {
	return &ActivityRepo{db: db}
}

// NextActivityID 生成下一个 activity 的自动 ID（如 A000001），基于 max(id)+1。
func (r *ActivityRepo) NextActivityID(ctx context.Context) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_activities").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("A%06d", next), nil
}

// Create 创建 activity，返回数据库生成的自增主键 id。
// 调用方应据此生成业务 activity_id（A+6 位自增 id），再 Update 回写，
// 这样 activity_id 与真实自增主键绑定，删除后不会被复用，避免与外部已用 ID 冲突。
func (r *ActivityRepo) Create(ctx context.Context, def *workflow.ActivityDef) (uint, error) {
	var m models.ActivityModel
	m.FromDef(def)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return 0, err
	}
	return m.ID, nil
}

// GetByID 按项目 + activity ID 查询。
func (r *ActivityRepo) GetByID(ctx context.Context, project, activityID string) (*workflow.ActivityDef, error) {
	var m models.ActivityModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND activity_id = ?", project, activityID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrActivityNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// List 列出指定项目下所有启用的 activity。
func (r *ActivityRepo) List(ctx context.Context, project string) ([]*workflow.ActivityDef, error) {
	var modelsList []models.ActivityModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND status = ?", project, int8(1)).
		Order("id ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.ActivityDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// ExistsByNamespaceName 判断指定项目下是否已存在相同 (act_namespace, act_name) 的 activity。
// excludeActivityID 用于更新场景，排除自身。返回 true 表示已存在重复。
func (r *ActivityRepo) ExistsByNamespaceName(ctx context.Context, project, actNamespace, actName, excludeActivityID string) (bool, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&models.ActivityModel{}).
		Where("project = ? AND act_namespace = ? AND act_name = ?", project, actNamespace, actName)
	if excludeActivityID != "" {
		query = query.Where("activity_id <> ?", excludeActivityID)
	}
	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateActivityID 将指定行的 activity_id 从 oldID 更新为 newID（用于插入后回写业务 ID）。
func (r *ActivityRepo) UpdateActivityID(ctx context.Context, project, oldID, newID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.ActivityModel{}).
		Where("project = ? AND activity_id = ?", project, oldID).
		Update("activity_id", newID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrActivityNotFound
	}
	return nil
}

// Update 更新 activity。
func (r *ActivityRepo) Update(ctx context.Context, def *workflow.ActivityDef) error {
	result := r.db.WithContext(ctx).
		Model(&models.ActivityModel{}).
		Where("project = ? AND activity_id = ?", def.Project, def.ActivityID).
		Updates(map[string]interface{}{
			"name":          def.Name,
			"act_namespace": def.ActNamespace,
			"act_name":      def.ActName,
			"activity_type": def.ActivityType,
			"kind":          def.Kind,
			"http_config":   def.HTTPConfig,
			"arguments":     string(def.Arguments),
			"arg_template":  def.ArgTemplate,
			"responses":     string(def.Responses),
			"status":        def.Status,
			"description":   def.Description,
			"tags":          models.SerializeActivityTags(def.Tags),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrActivityNotFound
	}
	return nil
}

// Delete 软删除 activity（按 project + activityID）。
// 注意：ActivityModel 不含 gorm.DeletedAt，此处用 Unscoped 物理删除。
func (r *ActivityRepo) Delete(ctx context.Context, project, activityID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND activity_id = ?", project, activityID).
		Delete(&models.ActivityModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrActivityNotFound
	}
	return nil
}
