package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// NodeStatus 节点状态常量
const (
	NodeStatusDisabled int8 = 0 // 禁用
	NodeStatusEnabled  int8 = 1 // 启用
)

// NodeKind 节点分类常量
const (
	NodeKindCondition = "condition" // 查询获取类：可产生 True/False 分支
	NodeKindAction    = "action"    // 策略执行类：仅 Success/Failure
)

// NodeModel 节点持久化模型，对应 wf_nodes 表。
type NodeModel struct {
	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Project        string         `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_node_id,priority:1;not null;index" json:"project"`
	NodeID         string         `gorm:"column:node_id;type:varchar(128);uniqueIndex:uk_project_node_id,priority:2;not null" json:"node_id"`
	Name           string         `gorm:"type:varchar(255);not null" json:"name"`
	Type           string         `gorm:"type:varchar(128);not null;index" json:"type"`
	DebugMode      bool           `gorm:"default:false" json:"debug_mode"`
	Configuration  string         `gorm:"type:text" json:"configuration"`
	AdditionalInfo string         `gorm:"type:text" json:"additional_info"`
	Params         string         `gorm:"type:text" json:"params"`
	Outputs        string         `gorm:"type:text" json:"outputs"`
	Kind           string         `gorm:"type:varchar(32);default:action;index" json:"kind"`
	Category       string         `gorm:"type:varchar(128);index" json:"category"`
	Description    string         `gorm:"type:varchar(512)" json:"description"`
	Status         int8           `gorm:"default:1;index" json:"status"`
	Version        string         `gorm:"type:varchar(64)" json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (NodeModel) TableName() string {
	return "wf_nodes"
}

// ToNodeDef 将数据库模型转换为 NodeDef。
func (m *NodeModel) ToNodeDef() *workflow.NodeDef {
	return &workflow.NodeDef{
		Project:        m.Project,
		NodeID:         m.NodeID,
		Name:           m.Name,
		Type:           m.Type,
		DebugMode:      m.DebugMode,
		Configuration:  json.RawMessage(m.Configuration),
		AdditionalInfo: json.RawMessage(m.AdditionalInfo),
		Params:         json.RawMessage(m.Params),
		Outputs:        json.RawMessage(m.Outputs),
		Kind:           m.Kind,
		Category:       m.Category,
		Description:    m.Description,
		Status:         m.Status,
		Version:        m.Version,
	}
}

// FromNodeDef 从 NodeDef 填充模型字段。
func (m *NodeModel) FromNodeDef(def *workflow.NodeDef) {
	m.Project = def.Project
	m.NodeID = def.NodeID
	m.Name = def.Name
	m.Type = def.Type
	m.DebugMode = def.DebugMode
	m.Configuration = string(def.Configuration)
	m.AdditionalInfo = string(def.AdditionalInfo)
	m.Params = string(def.Params)
	m.Outputs = string(def.Outputs)
	m.Kind = def.Kind
	m.Category = def.Category
	m.Description = def.Description
	m.Status = def.Status
	m.Version = def.Version
}
