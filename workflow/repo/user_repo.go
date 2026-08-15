package repo

import (
	"context"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow/models"
	"gorm.io/gorm"
)

// UserRepo 用户、会话与项目授权的数据访问层。
type UserRepo struct {
	db *gorm.DB
}

// NewUserRepo 创建用户仓储实例。
func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

// ============================================================
// 用户
// ============================================================

// CreateUser 创建用户（密码需在外部预先哈希后写入 PasswordHash）。
func (r *UserRepo) CreateUser(ctx context.Context, m *models.UserModel) (uint, error) {
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return 0, err
	}
	return m.ID, nil
}

// GetUserByUsername 按用户名查询用户（含已软删）。
func (r *UserRepo) GetUserByUsername(ctx context.Context, username string) (*models.UserModel, error) {
	var m models.UserModel
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetUserByID 按 ID 查询用户。
func (r *UserRepo) GetUserByID(ctx context.Context, id uint) (*models.UserModel, error) {
	var m models.UserModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// CountUsers 返回未软删用户总数（用于种子判断）。
func (r *UserRepo) CountUsers(ctx context.Context) (int64, error) {
	var c int64
	if err := r.db.WithContext(ctx).Model(&models.UserModel{}).Count(&c).Error; err != nil {
		return 0, err
	}
	return c, nil
}

// ListUsers 列出所有未软删用户（不含密码哈希）。
func (r *UserRepo) ListUsers(ctx context.Context) ([]*models.UserModel, error) {
	var list []*models.UserModel
	if err := r.db.WithContext(ctx).Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateUser 更新用户（部分字段）。
func (r *UserRepo) UpdateUser(ctx context.Context, m *models.UserModel) error {
	return r.db.WithContext(ctx).Model(m).Where("id = ?", m.ID).Updates(map[string]interface{}{
		"nickname":      m.Nickname,
		"role":          m.Role,
		"status":        m.Status,
		"password_hash": m.PasswordHash,
		"updated_at":    time.Now(),
	}).Error
}

// DeleteUser 软删用户（同时清理其项目授权）。
func (r *UserRepo) DeleteUser(ctx context.Context, id uint) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.UserModel{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("user_id = ?", id).Delete(&models.UserProjectModel{}).Error
}

// ============================================================
// 会话
// ============================================================

// CreateSession 写入一条登录会话。
func (r *UserRepo) CreateSession(ctx context.Context, m *models.UserSessionModel) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// GetSession 按 token 查询未过期会话（带用户联表）。
func (r *UserRepo) GetSession(ctx context.Context, token string) (*models.UserSessionModel, *models.UserModel, error) {
	var s models.UserSessionModel
	if err := r.db.WithContext(ctx).Where("token = ? AND expires_at > ?", token, time.Now()).First(&s).Error; err != nil {
		return nil, nil, err
	}
	var u models.UserModel
	if err := r.db.WithContext(ctx).Where("id = ?", s.UserID).First(&u).Error; err != nil {
		return nil, nil, err
	}
	return &s, &u, nil
}

// DeleteSession 删除单条会话（登出）。
func (r *UserRepo) DeleteSession(ctx context.Context, token string) error {
	return r.db.WithContext(ctx).Where("token = ?", token).Delete(&models.UserSessionModel{}).Error
}

// DeleteExpiredSessions 清理过期会话（定时任务调用）。
func (r *UserRepo) DeleteExpiredSessions(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&models.UserSessionModel{}).Error
}

// ============================================================
// 用户-项目授权
// ============================================================

// BindProject 绑定用户对某项目的访问权限（幂等 upsert）。
func (r *UserRepo) BindProject(ctx context.Context, userID uint, project string) error {
	var cnt int64
	if err := r.db.WithContext(ctx).Model(&models.UserProjectModel{}).
		Where("user_id = ? AND project = ?", userID, project).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&models.UserProjectModel{
		UserID:  userID,
		Project: project,
	}).Error
}

// UnbindProject 解绑用户对某项目的访问权限。
func (r *UserRepo) UnbindProject(ctx context.Context, userID uint, project string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND project = ?", userID, project).
		Delete(&models.UserProjectModel{}).Error
}

// ListProjectsByUser 列出用户被授权的项目列表（admin 不走此表）。
func (r *UserRepo) ListProjectsByUser(ctx context.Context, userID uint) ([]string, error) {
	var projects []string
	if err := r.db.WithContext(ctx).Model(&models.UserProjectModel{}).
		Where("user_id = ?", userID).Pluck("project", &projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

// ListUsersByProject 列出被授权访问某项目的用户 ID 列表。
func (r *UserRepo) ListUsersByProject(ctx context.Context, project string) ([]uint, error) {
	var ids []uint
	if err := r.db.WithContext(ctx).Model(&models.UserProjectModel{}).
		Where("project = ?", project).Pluck("user_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
