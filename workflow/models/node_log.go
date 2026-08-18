package models

import (
	"encoding/json"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// NodeLogModel node 运行日志持久化模型，对应 wf_node_logs 表。
// 由管理端收集器从 redis（workflow:node:log:<namespace>）消费 node 上报的入参/返回值后写入，
// 供前端按 node 查看与检索执行情况。
type NodeLogModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project     string    `gorm:"column:project;type:varchar(128);not null;index:idx_node_proj" json:"project"`
	Env         string    `gorm:"column:env;type:varchar(128);not null" json:"env"`
	NodeID      string    `gorm:"column:node_id;type:varchar(255);not null;index:idx_node_proj" json:"node_id"`
	NodeName    string    `gorm:"column:node_name;type:varchar(255);not null" json:"node_name"`
	EventID     string    `gorm:"column:event_id;type:varchar(128);index" json:"event_id"`
	Level       string    `gorm:"column:level;type:varchar(16);not null;index" json:"level"`
	Timestamp   int64     `gorm:"column:ts;not null;index" json:"timestamp"`
	DurationMs  int64     `gorm:"column:duration_ms" json:"duration_ms"`
	Payload     string    `gorm:"column:payload;type:json" json:"payload"`
	Result      string    `gorm:"column:result;type:json" json:"result"`
	ErrorMsg    string    `gorm:"column:error_msg;type:text" json:"error_msg"`
	TraceID      string    `gorm:"column:trace_id;type:varchar(128);index;default:''" json:"trace_id"`
	RootChainID  string    `gorm:"column:root_chain_id;type:varchar(128);index;default:''" json:"root_chain_id"`
	SpanID       string    `gorm:"column:span_id;type:varchar(128);index;default:''" json:"span_id"`
	RelationType string    `gorm:"column:relation_type;type:varchar(64);index;default:''" json:"relation_type"`
	CreatedAt    time.Time `json:"created_at"`
}

// TableName 返回表名。
func (NodeLogModel) TableName() string {
	return "wf_node_logs"
}

// ToDef 将数据库模型转换为 NodeLogDef。
func (m *NodeLogModel) ToDef() *workflow.NodeLogDef {
	return &workflow.NodeLogDef{
		ID:          m.ID,
		Project:     m.Project,
		Env:         m.Env,
		NodeID:      m.NodeID,
		NodeName:    m.NodeName,
		EventID:     m.EventID,
		Level:       m.Level,
		Timestamp:   m.Timestamp,
		DurationMs:  m.DurationMs,
		Payload:     json.RawMessage(m.Payload),
		Result:      json.RawMessage(m.Result),
		ErrorMsg:    m.ErrorMsg,
		TraceID:      m.TraceID,
		RootChainID:  m.RootChainID,
		SpanID:       m.SpanID,
		RelationType: m.RelationType,
		CreatedAt:    m.CreatedAt,
	}
}

// FromDef 从 NodeLogDef 填充模型字段。
func (m *NodeLogModel) FromDef(def *workflow.NodeLogDef) {
	m.Project = def.Project
	m.Env = def.Env
	m.NodeID = def.NodeID
	m.NodeName = def.NodeName
	m.EventID = def.EventID
	m.Level = def.Level
	m.Timestamp = def.Timestamp
	m.DurationMs = def.DurationMs
	m.Payload = string(def.Payload)
	m.Result = string(def.Result)
	m.ErrorMsg = def.ErrorMsg
	m.TraceID = def.TraceID
	m.RootChainID = def.RootChainID
	m.SpanID = def.SpanID
	m.RelationType = def.RelationType
}
