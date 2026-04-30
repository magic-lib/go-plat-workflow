package models

import (
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// ActivityTestRecordModel activity 测试记录持久化模型，对应 wf_activity_test_records 表。
// 每次"测试单个 activity"的入参与返回结果都会保存，方便后期查看调试。
type ActivityTestRecordModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project     string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_proj_record_id,priority:1;not null;index" json:"project"`
	RecordID    string    `gorm:"column:record_id;type:varchar(128);uniqueIndex:uk_proj_record_id,priority:2;not null" json:"record_id"`
	ActivityID  string    `gorm:"column:activity_id;type:varchar(128);not null;index" json:"activity_id"`
	ActivityName string   `gorm:"column:activity_name;type:varchar(255)" json:"activity_name"`
	EnvName     string    `gorm:"column:env_name;type:varchar(128)" json:"env_name"`
	InputParams string    `gorm:"column:input_params;type:text" json:"input_params"`
	EnvVars     string    `gorm:"column:env_vars;type:text" json:"env_vars"`
	Status      string    `gorm:"column:status;type:varchar(16);not null" json:"status"`
	Result      string    `gorm:"column:result;type:text" json:"result"`
	ErrorMsg    string    `gorm:"column:error_msg;type:text" json:"error_msg"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName 返回表名。
func (ActivityTestRecordModel) TableName() string {
	return "wf_activity_test_records"
}

// ToDef 将数据库模型转换为 ActivityTestRecordDef。
func (m *ActivityTestRecordModel) ToDef() *workflow.ActivityTestRecordDef {
	return &workflow.ActivityTestRecordDef{
		Project:     m.Project,
		RecordID:    m.RecordID,
		ActivityID:  m.ActivityID,
		ActivityName: m.ActivityName,
		EnvName:     m.EnvName,
		InputParams: m.InputParams,
		EnvVars:     m.EnvVars,
		Status:      m.Status,
		Result:      m.Result,
		ErrorMsg:    m.ErrorMsg,
		CreatedAt:   m.CreatedAt,
	}
}

// FromDef 从 ActivityTestRecordDef 填充模型字段。
func (m *ActivityTestRecordModel) FromDef(def *workflow.ActivityTestRecordDef) {
	m.Project = def.Project
	m.RecordID = def.RecordID
	m.ActivityID = def.ActivityID
	m.ActivityName = def.ActivityName
	m.EnvName = def.EnvName
	m.InputParams = def.InputParams
	m.EnvVars = def.EnvVars
	m.Status = def.Status
	m.Result = def.Result
	m.ErrorMsg = def.ErrorMsg
}
