package models

import (
	"encoding/json"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// ActivityLogModel activity 执行日志持久化模型，对应 wf_activity_logs 表。
// 由管理端收集器从 redis 消费 worker 上报的日志后写入，供前端按 activity 查看与检索。
type ActivityLogModel struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project      string    `gorm:"column:project;type:varchar(128);not null;index:idx_proj_act" json:"project"`
	Env          string    `gorm:"column:env;type:varchar(128);not null" json:"env"`
	ActNamespace string    `gorm:"column:act_namespace;type:varchar(255);not null;index:idx_proj_act" json:"act_namespace"`
	ActName      string    `gorm:"column:act_name;type:varchar(255);not null;index:idx_proj_act" json:"act_name"`
	EventID      string    `gorm:"column:event_id;type:varchar(128);index" json:"event_id"`
	Level        string    `gorm:"column:level;type:varchar(16);not null;index" json:"level"`
	Timestamp    int64     `gorm:"column:ts;not null;index" json:"timestamp"`
	DurationMs   int64     `gorm:"column:duration_ms" json:"duration_ms"`
	Payload      string    `gorm:"column:payload;type:json" json:"payload"`
	Result       string    `gorm:"column:result;type:text" json:"result"`
	ErrorMsg     string    `gorm:"column:error_msg;type:text" json:"error_msg"`
	RootChainID  string    `gorm:"column:root_chain_id;type:varchar(128);index;Default:''" json:"root_chain_id"`
	TraceID      string    `gorm:"column:trace_id;type:varchar(128);index;Default:''" json:"trace_id"`
	SpanID       string    `gorm:"column:span_id;type:varchar(128);index;Default:''" json:"span_id"`
	Attributes   string    `gorm:"column:attributes;type:text" json:"attributes"`
	CreatedAt    time.Time `json:"created_at"`
}

// TableName 返回表名。
func (ActivityLogModel) TableName() string {
	return "wf_activity_logs"
}

// ToDef 将数据库模型转换为 ActivityLogDef。
func (m *ActivityLogModel) ToDef() *workflow.ActivityLogDef {
	return &workflow.ActivityLogDef{
		ID:           m.ID,
		Project:      m.Project,
		Env:          m.Env,
		ActNamespace: m.ActNamespace,
		ActName:      m.ActName,
		EventID:      m.EventID,
		Level:        m.Level,
		Timestamp:    m.Timestamp,
		DurationMs:   m.DurationMs,
		Payload:      json.RawMessage(m.Payload),
		Result:       json.RawMessage(m.Result),
		ErrorMsg:     m.ErrorMsg,
		RootChainID:  m.RootChainID,
		TraceID:      m.TraceID,
		SpanID:       m.SpanID,
		Attributes:   m.Attributes,
		CreatedAt:    m.CreatedAt,
	}
}

// FromDef 从 ActivityLogDef 填充模型字段。
func (m *ActivityLogModel) FromDef(def *workflow.ActivityLogDef) {
	m.Project = def.Project
	m.Env = def.Env
	m.ActNamespace = def.ActNamespace
	m.ActName = def.ActName
	m.EventID = def.EventID
	m.Level = def.Level
	m.Timestamp = def.Timestamp
	m.DurationMs = def.DurationMs
	m.Payload = string(def.Payload)
	m.Result = string(def.Result)
	m.ErrorMsg = def.ErrorMsg
	m.RootChainID = def.RootChainID
	m.TraceID = def.TraceID
	m.SpanID = def.SpanID
	m.Attributes = def.Attributes
}
