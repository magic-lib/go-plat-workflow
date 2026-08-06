package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// ProjectRepo 项目仓储，实现 workflow.ProjectStore 接口。
type ProjectRepo struct {
	db *gorm.DB
}

// NewProjectRepo 创建项目仓储实例。
func NewProjectRepo(db *gorm.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Create 创建项目。
func (r *ProjectRepo) Create(ctx context.Context, def *workflow.ProjectDef) error {
	var m models.ProjectModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// GetByID 按项目 ID 查询。
func (r *ProjectRepo) GetByID(ctx context.Context, project string) (*workflow.ProjectDef, error) {
	var m models.ProjectModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND status = ?", project, models.NodeStatusEnabled).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrProjectNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// List 列出所有启用的项目。
func (r *ProjectRepo) List(ctx context.Context) ([]*workflow.ProjectDef, error) {
	var modelsList []models.ProjectModel
	err := r.db.WithContext(ctx).
		Where("status = ?", models.NodeStatusEnabled).
		Order("project ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.ProjectDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Update 更新项目。
func (r *ProjectRepo) Update(ctx context.Context, def *workflow.ProjectDef) error {
	updates := map[string]interface{}{
		"name":        def.Name,
		"description": def.Description,
		"status":      def.Status,
	}
	// 密钥仅在页面显式传入时才更新（避免清空已有密钥）
	if def.SecretKey != "" {
		updates["secret_key"] = def.SecretKey
	}
	result := r.db.WithContext(ctx).
		Model(&models.ProjectModel{}).
		Where("project = ?", def.Project).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrProjectNotFound
	}
	return nil
}

// GetSecret 按项目 ID 查询密钥（不返回其他字段）。
func (r *ProjectRepo) GetSecret(ctx context.Context, project string) (string, error) {
	var secret string
	err := r.db.WithContext(ctx).
		Model(&models.ProjectModel{}).
		Where("project = ? AND status = ?", project, models.NodeStatusEnabled).
		Pluck("secret_key", &secret).Error
	if err != nil {
		return "", err
	}
	if secret == "" {
		return "", workflow.ErrProjectNotFound
	}
	return secret, nil
}

// Delete 软删除项目。
func (r *ProjectRepo) Delete(ctx context.Context, project string) error {
	result := r.db.WithContext(ctx).
		Where("project = ?", project).
		Delete(&models.ProjectModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrProjectNotFound
	}
	return nil
}
