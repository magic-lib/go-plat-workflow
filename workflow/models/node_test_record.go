package models

import (
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// NodeTestRecordModel 单节点测试记录持久化模型，对应 wf_node_test_records 表。
// 每次"测试单个节点"的入参与返回结果都会保存，方便后期查看调试。
type NodeTestRecordModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project     string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_record_id,priority:1;not null;index" json:"project"`
	RecordID    string    `gorm:"column:record_id;type:varchar(128);uniqueIndex:uk_project_record_id,priority:2;not null" json:"record_id"`
	NodeID      string    `gorm:"column:node_id;type:varchar(128);not null;index" json:"node_id"`
	NodeName    string    `gorm:"column:node_name;type:varchar(255)" json:"node_name"`
	EnvName     string    `gorm:"column:env_name;type:varchar(128)" json:"env_name"`
	TraceID     string    `gorm:"column:trace_id;type:varchar(128);index;default:''" json:"trace_id"`
	InputParams string    `gorm:"column:input_params;type:text" json:"input_params"`
	EnvVars     string    `gorm:"column:env_vars;type:text" json:"env_vars"`
	Status      string    `gorm:"column:status;type:varchar(16);not null" json:"status"`
	Result      string    `gorm:"column:result;type:text" json:"result"`
	ErrorMsg    string    `gorm:"column:error_msg;type:text" json:"error_msg"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName 返回表名。
func (NodeTestRecordModel) TableName() string {
	return "wf_node_test_records"
}

// ToDef 将数据库模型转换为 NodeTestRecordDef。
func (m *NodeTestRecordModel) ToDef() *workflow.NodeTestRecordDef {
	return &workflow.NodeTestRecordDef{
		Project:     m.Project,
		RecordID:    m.RecordID,
		NodeID:      m.NodeID,
		NodeName:    m.NodeName,
		EnvName:     m.EnvName,
		TraceID:     m.TraceID,
		InputParams: m.InputParams,
		EnvVars:     m.EnvVars,
		Status:      m.Status,
		Result:      m.Result,
		ErrorMsg:    m.ErrorMsg,
		CreatedAt:   m.CreatedAt,
	}
}

// FromDef 从 NodeTestRecordDef 填充模型字段。
func (m *NodeTestRecordModel) FromDef(def *workflow.NodeTestRecordDef) {
	m.Project = def.Project
	m.RecordID = def.RecordID
	m.NodeID = def.NodeID
	m.NodeName = def.NodeName
	m.EnvName = def.EnvName
	m.TraceID = def.TraceID
	m.InputParams = def.InputParams
	m.EnvVars = def.EnvVars
	m.Status = def.Status
	m.Result = def.Result
	m.ErrorMsg = def.ErrorMsg
}
