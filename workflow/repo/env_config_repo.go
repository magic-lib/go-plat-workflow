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

// ListAlertEnvs 列出所有【开启了告警】的环境配置（供离线巡检扫描告警目标）。
// 告警配置以 JSON 存在 alert_config 列，SQL 层无法直接过滤 enabled，
// 故全量取出后在内存中筛选（环境数量很小，成本可忽略）。
func (r *EnvConfigRepo) ListAlertEnvs(ctx context.Context) ([]*workflow.EnvConfigDef, error) {
	var modelsList []models.EnvConfigModel
	err := r.db.WithContext(ctx).
		Where("alert_config IS NOT NULL AND alert_config <> ''").
		Order("project ASC, env_name ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	out := make([]*workflow.EnvConfigDef, 0, len(modelsList))
	for i := range modelsList {
		def := modelsList[i].ToDef()
		if def.AlertConfig.AlertEnabled() {
			out = append(out, def)
		}
	}
	return out, nil
}

// ListByProject 列出指定项目下所有环境配置，按环境名排序。
// 按 env_name 去重（保留首个），避免数据库中存在重复环境名时前端展示重复项。
func (r *EnvConfigRepo) ListByProject(ctx context.Context, project string) ([]*workflow.EnvConfigDef, error) {
	var modelsList []models.EnvConfigModel
	err := r.db.WithContext(ctx).
		Where("project = ?", project).
		Order("env_name ASC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(modelsList))
	defs := make([]*workflow.EnvConfigDef, 0, len(modelsList))
	for i := range modelsList {
		name := modelsList[i].EnvName
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
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
