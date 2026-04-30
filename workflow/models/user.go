package models

import (
	"time"

	"gorm.io/gorm"
)

// UserModel 后台用户表，对应 wf_users。
// role 字段区分 admin（全项目权限，可管理用户）与 viewer（仅拥有被授权项目的访问）。
type UserModel struct {
	ID           uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string         `gorm:"column:username;type:varchar(128);uniqueIndex;not null" json:"username"`
	PasswordHash string         `gorm:"column:password_hash;type:varchar(255);not null" json:"-"`
	Nickname     string         `gorm:"column:nickname;type:varchar(255)" json:"nickname"`
	Role         string         `gorm:"column:role;type:varchar(32);default:viewer;index" json:"role"`
	Status       int8           `gorm:"column:status;default:1;index" json:"status"` // 1=启用 0=禁用
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (UserModel) TableName() string { return "wf_users" }

// UserSessionModel 登录会话表，对应 wf_user_sessions。
// 支持多实例共享、主动登出与过期清理。
type UserSessionModel struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint      `gorm:"column:user_id;type:bigint;not null;index" json:"user_id"`
	Token     string    `gorm:"column:token;type:varchar(128);uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 返回表名。
func (UserSessionModel) TableName() string { return "wf_user_sessions" }

// UserProjectModel 用户-项目授权关系表，对应 wf_user_projects。
// 记录某用户对某项目的权限级别；admin 用户不在此表中体现（视为拥有全部项目）。
// role 字段区分项目级权限：viewer=只读（可查日志/执行单元测试），editor=管理（可编辑）。
type UserProjectModel struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint           `gorm:"column:user_id;type:bigint;not null;uniqueIndex:uk_user_project,priority:1" json:"user_id"`
	Project   string         `gorm:"column:project;type:varchar(128);not null;uniqueIndex:uk_user_project,priority:2" json:"project"`
	Role      string         `gorm:"column:role;type:varchar(32);default:viewer;index" json:"role"` // viewer=只读 editor=管理
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (UserProjectModel) TableName() string { return "wf_user_projects" }
