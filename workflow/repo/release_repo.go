package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// RootChainReleaseRepo 根链发布版本仓储，实现 workflow.RootChainReleaseStore 接口。
type RootChainReleaseRepo struct {
	db *gorm.DB
}

// NewRootChainReleaseRepo 创建根链发布版本仓储实例。
func NewRootChainReleaseRepo(db *gorm.DB) *RootChainReleaseRepo {
	return &RootChainReleaseRepo{db: db}
}

// Create 创建发布版本。
func (r *RootChainReleaseRepo) Create(ctx context.Context, def *workflow.RootChainReleaseDef) error {
	var m models.RootChainReleaseModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// ListByChain 列出指定根链的所有发布版本（按版本号倒序）。
func (r *RootChainReleaseRepo) ListByChain(ctx context.Context, project, chainID string) ([]*workflow.RootChainReleaseDef, error) {
	var modelsList []models.RootChainReleaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ?", project, chainID).
		Order("version DESC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.RootChainReleaseDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// GetByVersion 查询指定发布版本。
func (r *RootChainReleaseRepo) GetByVersion(ctx context.Context, project, chainID string, version int) (*workflow.RootChainReleaseDef, error) {
	var m models.RootChainReleaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ? AND version = ?", project, chainID, version).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrReleaseNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// GetCurrent 查询当前生产环境使用的发布版本。
func (r *RootChainReleaseRepo) GetCurrent(ctx context.Context, project, chainID string) (*workflow.RootChainReleaseDef, error) {
	var m models.RootChainReleaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND chain_id = ? AND is_current = ?", project, chainID, true).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrReleaseNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListCurrentByProject 列出指定项目下所有根链的当前发布版本。
func (r *RootChainReleaseRepo) ListCurrentByProject(ctx context.Context, project string) ([]*workflow.RootChainReleaseDef, error) {
	var modelsList []models.RootChainReleaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND is_current = ? and chain_id in (select chain_id from wf_root_chains where project = ?)", project, true, project).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.RootChainReleaseDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// HasReleases 判断指定根链是否存在发布记录。
func (r *RootChainReleaseRepo) HasReleases(ctx context.Context, project, chainID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.RootChainReleaseModel{}).
		Where("project = ? AND chain_id = ?", project, chainID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MaxVersion 查询指定根链的最大发布版本号（无记录返回 0）。
func (r *RootChainReleaseRepo) MaxVersion(ctx context.Context, project, chainID string) (int, error) {
	var maxVer int
	err := r.db.WithContext(ctx).
		Model(&models.RootChainReleaseModel{}).
		Where("project = ? AND chain_id = ?", project, chainID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&maxVer).Error
	return maxVer, err
}

// DeleteByVersion 删除指定发布版本（当前生产版本不允许删除）。
func (r *RootChainReleaseRepo) DeleteByVersion(ctx context.Context, project, chainID string, version int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m models.RootChainReleaseModel
		if err := tx.Where("project = ? AND chain_id = ? AND version = ?", project, chainID, version).
			First(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return workflow.ErrReleaseNotFound
			}
			return err
		}
		if m.IsCurrent {
			return workflow.ErrReleaseInUse
		}
		return tx.Where("project = ? AND chain_id = ? AND version = ?", project, chainID, version).
			Delete(&models.RootChainReleaseModel{}).Error
	})
}

// SetCurrent 将指定版本设为当前生产版本（同事务内清除同链其他版本的当前标记）。
func (r *RootChainReleaseRepo) SetCurrent(ctx context.Context, project, chainID string, version int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.RootChainReleaseModel{}).
			Where("project = ? AND chain_id = ?", project, chainID).
			Update("is_current", false).Error; err != nil {
			return err
		}
		result := tx.Model(&models.RootChainReleaseModel{}).
			Where("project = ? AND chain_id = ? AND version = ?", project, chainID, version).
			Update("is_current", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return workflow.ErrReleaseNotFound
		}
		return nil
	})
}
