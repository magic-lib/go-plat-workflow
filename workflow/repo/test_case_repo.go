package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// TestCaseRepo 测试用例仓储，实现 workflow.TestCaseStore 接口。
type TestCaseRepo struct {
	db *gorm.DB
}

// NewTestCaseRepo 创建测试用例仓储实例。
func NewTestCaseRepo(db *gorm.DB) *TestCaseRepo {
	return &TestCaseRepo{db: db}
}

// NextCaseID 生成下一个测试用例自动 ID（如 T000001），基于 max(id)+1。
func (r *TestCaseRepo) NextCaseID(ctx context.Context, project string) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_test_cases").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("T%06d", next), nil
}

// Create 创建测试用例。
func (r *TestCaseRepo) Create(ctx context.Context, def *workflow.TestCaseDef) error {
	var m models.TestCaseModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// Update 更新测试用例（按 project + case_id）。
func (r *TestCaseRepo) Update(ctx context.Context, def *workflow.TestCaseDef) error {
	result := r.db.WithContext(ctx).
		Model(&models.TestCaseModel{}).
		Where("project = ? AND case_id = ?", def.Project, def.CaseID).
		Updates(map[string]interface{}{
			"owner_id":            def.OwnerID,
			"owner_type":          def.OwnerType,
			"name":                def.Name,
			"chain_id":            def.ChainID,
			"chain_name":          def.ChainName,
			"node_ids":            def.NodeIDs,
			"sub_chain_ids":       def.SubChainIDs,
			"connections_data":    def.ConnectionsData,
			"payload":             def.Payload,
			"debug_mode":          def.DebugMode,
			"use_release":         def.UseRelease,
			"node_param_overrides": def.NodeParamOverrides,
			"last_result":         def.LastResult,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrRootChainNotFound
	}
	return nil
}

// GetByID 按项目 + 测试用例 ID 查询。
func (r *TestCaseRepo) GetByID(ctx context.Context, project, caseID string) (*workflow.TestCaseDef, error) {
	var m models.TestCaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND case_id = ?", project, caseID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrRootChainNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListByOwner 列出指定 owner（owner_id）下所有测试用例，按创建时间倒序。
func (r *TestCaseRepo) ListByOwner(ctx context.Context, project, ownerID string) ([]*workflow.TestCaseDef, error) {
	var modelsList []models.TestCaseModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND owner_id = ?", project, ownerID).
		Order("id DESC").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.TestCaseDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// Delete 删除测试用例（按 project + caseID）。
func (r *TestCaseRepo) Delete(ctx context.Context, project, caseID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND case_id = ?", project, caseID).
		Delete(&models.TestCaseModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrRootChainNotFound
	}
	return nil
}
