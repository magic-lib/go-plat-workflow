package models

import (
	"encoding/json"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// EnvConfigModel 环境配置持久化模型，对应 wf_env_configs 表。
// 每个 project 下可配置多个环境（如 dev/test/prod），每个环境可保存
// 环境变量、Redis、MySQL 连接信息，供后续使用。
type EnvConfigModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project     string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_env,priority:1;not null;index" json:"project"`
	EnvName     string    `gorm:"column:env_name;type:varchar(128);uniqueIndex:uk_project_env,priority:2;not null" json:"env_name"`
	Description string    `gorm:"type:varchar(512)" json:"description"`
	EnvVars     string    `gorm:"column:env_vars;type:text" json:"env_vars"`
	RedisConfig string    `gorm:"column:redis_config;type:text" json:"redis_config"`
	MySQLConfig string    `gorm:"column:mysql_config;type:text" json:"mysql_config"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 返回表名。
func (EnvConfigModel) TableName() string {
	return "wf_env_configs"
}

// ToDef 将数据库模型转换为 EnvConfigDef。
func (m *EnvConfigModel) ToDef() *workflow.EnvConfigDef {
	def := &workflow.EnvConfigDef{
		Project:     m.Project,
		EnvName:     m.EnvName,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.EnvVars != "" {
		_ = json.Unmarshal([]byte(m.EnvVars), &def.EnvVars)
	}
	if m.RedisConfig != "" {
		_ = json.Unmarshal([]byte(m.RedisConfig), &def.RedisConfig)
	}
	if m.MySQLConfig != "" {
		_ = json.Unmarshal([]byte(m.MySQLConfig), &def.MySQLConfig)
	}
	return def
}

// FromDef 从 EnvConfigDef 填充模型字段。
func (m *EnvConfigModel) FromDef(def *workflow.EnvConfigDef) {
	m.Project = def.Project
	m.EnvName = def.EnvName
	m.Description = def.Description
	if def.EnvVars != nil {
		b, _ := json.Marshal(def.EnvVars)
		m.EnvVars = string(b)
	}
	if def.RedisConfig != nil {
		b, _ := json.Marshal(def.RedisConfig)
		m.RedisConfig = string(b)
	}
	if def.MySQLConfig != nil {
		b, _ := json.Marshal(def.MySQLConfig)
		m.MySQLConfig = string(b)
	}
}
