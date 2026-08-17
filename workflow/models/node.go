package models

import (
	"encoding/json"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
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
	Namespace      string         `gorm:"column:namespace;type:varchar(255);index" json:"namespace"`
	Name           string         `gorm:"type:varchar(255);not null" json:"name"`
	Type           string         `gorm:"type:varchar(128);not null;index" json:"type"`
	DebugMode      bool           `gorm:"default:false" json:"debug_mode"`
	Configuration  string         `gorm:"type:text" json:"configuration"`
	AdditionalInfo string         `gorm:"type:text" json:"additional_info"`
	Params         string         `gorm:"type:text" json:"params"`
	Outputs        string         `gorm:"type:text" json:"outputs"`
	Kind           string         `gorm:"type:varchar(32);default:action;index" json:"kind"`
	Category       string         `gorm:"type:varchar(128);index" json:"category"`
	Tags           string         `gorm:"type:varchar(512)" json:"tags"`
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

// 节点类型历史别名映射。
// 早期版本自定义节点在库中存储的是无命名空间的值（activity/condSwitch），
// 现统一改为带 custom/ 前缀以与 rulego 原生节点区分，并在加载时做兼容归一化，
// 保证存量数据无需手动迁移即可被 builder / executor 正确识别。
var nodeTypeAlias = map[string]string{
	"activity":   common.ActivityNodeTypeName,
	"condSwitch": common.CondSwitchNodeTypeName,
}

// normalizeNodeType 将历史类型值归一化为当前规范值。
// 未知类型（如 rulego 原生 log/jsTransform 或已带 custom/ 前缀的自定义类型）原样返回。
func normalizeNodeType(t string) string {
	if norm, ok := nodeTypeAlias[t]; ok {
		return norm
	}
	return t
}

// ToNodeDef 将数据库模型转换为 NodeDef。
// Type 字段会经过 normalizeNodeType 兼容存量数据中无 custom/ 前缀的旧值。
// 注意：json.RawMessage("") 是非法 JSON，序列化会报错导致回显失败，故对空值回退为合法空对象/数组。
func (m *NodeModel) ToNodeDef() *workflow.NodeDef {
	conf := m.Configuration
	if len(conf) == 0 {
		conf = "{}"
	}
	additional := m.AdditionalInfo
	if len(additional) == 0 {
		additional = "{}"
	}
	params := m.Params
	if len(params) == 0 {
		params = "[]"
	}
	outputs := m.Outputs
	if len(outputs) == 0 {
		outputs = "[]"
	}
	return &workflow.NodeDef{
		Project:        m.Project,
		NodeID:         m.NodeID,
		Namespace:      m.Namespace,
		Name:           m.Name,
		Type:           normalizeNodeType(m.Type),
		DebugMode:      m.DebugMode,
		Configuration:  json.RawMessage(conf),
		AdditionalInfo: json.RawMessage(additional),
		Params:         json.RawMessage(params),
		Outputs:        json.RawMessage(outputs),
		Kind:           m.Kind,
		Category:       m.Category,
		Tags:           parseTags(m.Tags),
		Description:    m.Description,
		Status:         m.Status,
		Version:        m.Version,
	}
}

// FromNodeDef 从 NodeDef 填充模型字段。
func (m *NodeModel) FromNodeDef(def *workflow.NodeDef) {
	m.Project = def.Project
	m.NodeID = def.NodeID
	m.Namespace = def.Namespace
	m.Name = def.Name
	m.Type = def.Type
	m.DebugMode = def.DebugMode
	m.Configuration = rawOr(def.Configuration, "{}")
	m.AdditionalInfo = rawOr(def.AdditionalInfo, "{}")
	m.Params = rawOr(def.Params, "[]")
	m.Outputs = rawOr(def.Outputs, "[]")
	m.Kind = def.Kind
	m.Category = def.Category
	m.Tags = serializeTags(def.Tags)
	m.Description = def.Description
	m.Status = def.Status
	m.Version = def.Version
}

// rawOr 将 json.RawMessage 转为字符串，nil 或空时回退为 fallback（合法 JSON）。
func rawOr(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	return string(raw)
}
