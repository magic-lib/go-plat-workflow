package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// ProjectModel 项目持久化模型，对应 wf_projects 表。
type ProjectModel struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Project     string         `gorm:"column:project;type:varchar(128);uniqueIndex;not null" json:"project"`
	Name        string         `gorm:"type:varchar(255);not null" json:"name"`
	Description string         `gorm:"type:varchar(512)" json:"description"`
	Status      int8           `gorm:"default:1;index" json:"status"`
	// SecretKey 项目密钥，用于对外接口鉴权（不通过 ProjectDef 对外暴露）
	SecretKey string `gorm:"column:secret_key;type:varchar(255)" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (ProjectModel) TableName() string {
	return "wf_projects"
}

// ToDef 将数据库模型转换为 ProjectDef。
func (m *ProjectModel) ToDef() *workflow.ProjectDef {
	return &workflow.ProjectDef{
		Project:     m.Project,
		Name:        m.Name,
		Description: m.Description,
		Status:      m.Status,
	}
}

// FromDef 从 ProjectDef 填充模型字段。
func (m *ProjectModel) FromDef(def *workflow.ProjectDef) {
	m.Project = def.Project
	m.Name = def.Name
	m.Description = def.Description
	m.Status = def.Status
	m.SecretKey = def.SecretKey
}
