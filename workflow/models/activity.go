package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// normalizeJSONRaw 将 json.RawMessage 归一化为可持久化的字符串。
// 当值为 nil、空字节或 JSON 的 null 字面量时，返回空字符串，
// 避免 json.Marshal(nil) 产生 "null" 字符串写入数据库。
func NormalizeJSONRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	return s
}

// ActivityModel activity 模板持久化模型，对应 wf_activities 表。
type ActivityModel struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project      string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_activity_id,priority:1;uniqueIndex:uk_project_ns_name,priority:1;not null;index" json:"project"`
	ActivityID   string    `gorm:"column:activity_id;type:varchar(128);uniqueIndex:uk_project_activity_id,priority:2;not null" json:"activity_id"`
	Name         string    `gorm:"type:varchar(255);not null" json:"name"`
	ActNamespace string    `gorm:"column:act_namespace;type:varchar(255);not null;index;uniqueIndex:uk_project_ns_name,priority:2" json:"act_namespace"`
	ActName      string    `gorm:"column:act_name;type:varchar(255);not null;uniqueIndex:uk_project_ns_name,priority:3" json:"act_name"`
	ActivityType string    `gorm:"column:activity_type;type:varchar(128)" json:"activity_type"`
	Kind         string    `gorm:"column:kind;type:varchar(32);default:redis" json:"kind"`
	HTTPConfig   string    `gorm:"column:http_config;type:text" json:"http_config"`
	Arguments    string    `gorm:"type:text" json:"arguments"`
	ArgTemplate  string    `gorm:"column:arg_template;type:text" json:"arg_template"`
	Responses    string    `gorm:"type:text" json:"responses"`
	ReturnValues string    `gorm:"column:return_values;type:text" json:"return_values"`
	Status       int8      `gorm:"default:1;index" json:"status"`
	Description  string    `gorm:"type:varchar(512)" json:"description"`
	Tags         string    `gorm:"type:varchar(512)" json:"tags"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 返回表名。
func (ActivityModel) TableName() string {
	return "wf_activities"
}

// ToDef 将数据库模型转换为 ActivityDef。
func (m *ActivityModel) ToDef() *workflow.ActivityDef {
	def := &workflow.ActivityDef{
		Project:      m.Project,
		ActivityID:   m.ActivityID,
		Name:         m.Name,
		ActNamespace: m.ActNamespace,
		ActName:      m.ActName,
		ActivityType: m.ActivityType,
		Kind:         m.Kind,
		HTTPConfig:   m.HTTPConfig,
		Arguments:    json.RawMessage(m.Arguments),
		ArgTemplate:  m.ArgTemplate,
		Responses:    json.RawMessage(m.Responses),
		ReturnValues: json.RawMessage(m.ReturnValues),
		Status:       m.Status,
		Description:  m.Description,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
	def.Tags = parseTags(m.Tags)
	return def
}

// FromDef 从 ActivityDef 填充模型字段。
func (m *ActivityModel) FromDef(def *workflow.ActivityDef) {
	m.Project = def.Project
	m.ActivityID = def.ActivityID
	m.Name = def.Name
	m.ActNamespace = def.ActNamespace
	m.ActName = def.ActName
	m.ActivityType = def.ActivityType
	if def.Kind == "" {
		m.Kind = workflow.ActivityKindRedis
	} else {
		m.Kind = def.Kind
	}
	m.HTTPConfig = def.HTTPConfig
	m.Arguments = NormalizeJSONRaw(def.Arguments)
	m.ArgTemplate = def.ArgTemplate
	m.Responses = NormalizeJSONRaw(def.Responses)
	m.ReturnValues = NormalizeJSONRaw(def.ReturnValues)
	m.Status = def.Status
	m.Description = def.Description
	m.Tags = serializeTags(def.Tags)
}

// parseTags 将逗号分隔的标签字符串解析为切片。
func parseTags(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// serializeTags 将标签切片序列化为逗号分隔字符串。
func serializeTags(tags []string) string {
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, ",")
}

// SerializeActivityTags 导出标签序列化方法（供 repo 层调用）。
func SerializeActivityTags(tags []string) string {
	return serializeTags(tags)
}
