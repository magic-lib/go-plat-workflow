package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// ActivityTestRecordRepo activity 测试记录仓储，实现 workflow.ActivityTestRecordStore 接口。
type ActivityTestRecordRepo struct {
	db *gorm.DB
}

// NewActivityTestRecordRepo 创建 activity 测试记录仓储实例。
func NewActivityTestRecordRepo(db *gorm.DB) *ActivityTestRecordRepo {
	return &ActivityTestRecordRepo{db: db}
}

// NextRecordID 预估下一个测试记录自动 ID（如 T000001），基于 max(id)+1，仅供展示参考。
// 实际创建时编号由 Create 按自增主键生成，不要用它的结果去创建测试记录。
func (r *ActivityTestRecordRepo) NextRecordID(ctx context.Context, project string) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_activity_test_records").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("T%06d", next), nil
}

// Create 创建测试记录。RecordID 为空（或为临时编号）时先插入再按自增主键回写 T000012 格式的编号，并回填到 def。
func (r *ActivityTestRecordRepo) Create(ctx context.Context, def *workflow.ActivityTestRecordDef) error {
	var m models.ActivityTestRecordModel
	m.FromDef(def)
	if !needAutoBizID(def.RecordID) {
		return r.db.WithContext(ctx).Create(&m).Error
	}
	tempID := workflow.NewTempBizID()
	m.RecordID = tempID
	finalID, err := createWithAutoBizID(ctx, r.db, bizIDCreate{
		model: &m, column: "record_id", prefix: "T", tempID: tempID,
		id: func() uint { return m.ID },
	})
	if err != nil {
		return err
	}
	def.RecordID = finalID
	return nil
}

// GetByID 按项目 + 测试记录 ID 查询。
func (r *ActivityTestRecordRepo) GetByID(ctx context.Context, project, recordID string) (*workflow.ActivityTestRecordDef, error) {
	var m models.ActivityTestRecordModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND record_id = ?", project, recordID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrActivityNotFound
		}
		return nil, err
	}
	return m.ToDef(), nil
}

// ListByActivity 列出指定 activity 下所有测试记录，按创建时间倒序。
func (r *ActivityTestRecordRepo) ListByActivity(ctx context.Context, project, activityID string) ([]*workflow.ActivityTestRecordDef, error) {
	var modelsList []models.ActivityTestRecordModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND activity_id = ?", project, activityID).
		Order("id DESC").
		Limit(50).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.ActivityTestRecordDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, nil
}

// ListTestStatusByActivities 批量查询每个 activity 的测试状态汇总（可按 env 限定环境）。
// 返回值：activity_id -> "success"(至少一条成功) / "failed"(有记录但无成功) / "none"(无记录)。
// env 为空时统计该 activity 在所有环境下的测试记录。
func (r *ActivityTestRecordRepo) ListTestStatusByActivities(ctx context.Context, project string, env string, activityIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(activityIDs) == 0 {
		return result, nil
	}
	type row struct {
		ActivityID string
		HasSuccess int
		Total      int
	}
	var rows []row
	query := r.db.WithContext(ctx).
		Model(&models.ActivityTestRecordModel{}).
		Select("activity_id, SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS has_success, COUNT(*) AS total").
		Where("project = ? AND activity_id IN ?", project, activityIDs)
	if env != "" {
		query = query.Where("env_name = ?", env)
	}
	err := query.Group("activity_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, rw := range rows {
		switch {
		case rw.HasSuccess > 0:
			result[rw.ActivityID] = "success"
		case rw.Total > 0:
			result[rw.ActivityID] = "failed"
		default:
			result[rw.ActivityID] = "none"
		}
	}
	return result, nil
}

// Delete 删除测试记录（按 project + record_id）。
func (r *ActivityTestRecordRepo) Delete(ctx context.Context, project, recordID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND record_id = ?", project, recordID).
		Delete(&models.ActivityTestRecordModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrActivityNotFound
	}
	return nil
}
