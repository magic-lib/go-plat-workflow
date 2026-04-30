package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// ProjectSecretRepo 项目密钥仓储。
type ProjectSecretRepo struct {
	db *gorm.DB
}

// NewProjectSecretRepo 创建项目密钥仓储实例。
func NewProjectSecretRepo(db *gorm.DB) *ProjectSecretRepo {
	return &ProjectSecretRepo{db: db}
}

// List 列出项目下所有密钥（含明文，仅用于管理查询接口）。
func (r *ProjectSecretRepo) List(ctx context.Context, project string) ([]*workflow.SecretKeyItem, error) {
	var rows []models.ProjectSecretModel
	err := r.db.WithContext(ctx).
		Where("project = ?", project).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*workflow.SecretKeyItem, 0, len(rows))
	for i := range rows {
		items = append(items, rows[i].ToItem())
	}
	return items, nil
}

// ListKeys 列出项目下所有密钥明文（用于对外配置查询接口的鉴权比对）。
func (r *ProjectSecretRepo) ListKeys(ctx context.Context, project string) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).
		Model(&models.ProjectSecretModel{}).
		Where("project = ?", project).
		Pluck("secret_key", &keys).Error
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// Create 新增一个密钥项。
func (r *ProjectSecretRepo) Create(ctx context.Context, project, key, remark string) error {
	m := models.ProjectSecretModel{Project: project, SecretKey: key, Remark: remark}
	return r.db.WithContext(ctx).Create(&m).Error
}

// DeleteByKey 按明文密钥删除密钥项。
func (r *ProjectSecretRepo) DeleteByKey(ctx context.Context, project, key string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND secret_key = ?", project, key).
		Delete(&models.ProjectSecretModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("secret not found")
	}
	return nil
}

// Delete 按 ID 删除密钥项。
func (r *ProjectSecretRepo) Delete(ctx context.Context, project string, id uint) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND id = ?", project, id).
		Delete(&models.ProjectSecretModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("secret not found")
	}
	return nil
}
