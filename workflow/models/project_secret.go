package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// ProjectSecretModel 项目密钥持久化模型，对应 wf_project_secrets 表。
// 一个项目可配置多个密钥，每个密钥含备注，用于为不同账户分配不同密钥访问对外配置查询接口。
type ProjectSecretModel struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Project   string         `gorm:"column:project;type:varchar(128);index;not null" json:"project"`
	SecretKey string         `gorm:"column:secret_key;type:varchar(255);not null" json:"secret_key"`
	Remark    string         `gorm:"column:remark;type:varchar(255)" json:"remark"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (ProjectSecretModel) TableName() string {
	return "wf_project_secrets"
}

// ToItem 转换为对外密钥项（不含明文，仅在管理查询接口按需返回 Key）。
func (m *ProjectSecretModel) ToItem() *workflow.SecretKeyItem {
	return &workflow.SecretKeyItem{
		Key:    m.SecretKey,
		Remark: m.Remark,
	}
}
