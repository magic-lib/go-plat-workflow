package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// EnvConfigRepo 环境配置仓储，实现 workflow.EnvConfigStore 接口。
type EnvConfigRepo struct {
	db *gorm.DB
}

// NewEnvConfigRepo 创建环境配置仓储实例。
func NewEnvConfigRepo(db *gorm.DB) *EnvConfigRepo {
	return &EnvConfigRepo{db: db}
}

// Upsert 创建或更新环境配置（按 project + env_name 冲突时更新全部字段）。
func (r *EnvConfigRepo) Upsert(ctx context.Context, def *workflow.EnvConfigDef) error {
	var m models.EnvConfigModel
	r.db.WithContext(ctx).Where("project = ? AND env_name = ?", def.Project, def.EnvName).FirstOrInit(&m)
	m.FromDef(def)
	return r.db.WithContext(ctx).Save(&m).Error
}

// GetByName 按项目 + 环境名查询。
func (r *EnvConfigRepo) GetByName(ctx context.Context, project, envName string) (*workflow.EnvConfigDef, error) {
	var m models.EnvConfigModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND env_name = ?", project, envName).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrProjectNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListByProject 列出指定项目下所有环境配置，按环境名排序。
func (r *EnvConfigRepo) ListByProject(ctx context.Context, project string) ([]*workflow.EnvConfigDef, error) {
	var modelsList []models.EnvConfigModel
	err := r.db.WithContext(ctx).
		Where("project = ?", project).
		Order("env_name ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.EnvConfigDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Delete 删除环境配置（按 project + env_name）。
func (r *EnvConfigRepo) Delete(ctx context.Context, project, envName string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND env_name = ?", project, envName).
		Delete(&models.EnvConfigModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrProjectNotFound
	}
	return nil
}

// ListAll 列出系统中所有项目下的全部环境配置（按 project、env_name 排序），
// 用于管理端自动发现各环境配置的 Redis 并启动对应监听。
func (r *EnvConfigRepo) ListAll(ctx context.Context) ([]*workflow.EnvConfigDef, error) {
	var modelsList []models.EnvConfigModel
	err := r.db.WithContext(ctx).
		Order("project ASC, env_name ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.EnvConfigDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}
