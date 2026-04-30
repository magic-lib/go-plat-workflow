package models

import (
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// TestCaseModel 测试用例持久化模型，对应 wf_test_cases 表。
// 一条测试用例保存一次 Execute Tab 的完整测试输入配置，挂载在某个
// RootChain 或 SubChain（owner）上，便于复用以调试。
type TestCaseModel struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project            string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_case_id,priority:1;not null;index" json:"project"`
	CaseID             string    `gorm:"column:case_id;type:varchar(128);uniqueIndex:uk_project_case_id,priority:2;not null" json:"case_id"`
	OwnerID            string    `gorm:"column:owner_id;type:varchar(128);not null;index" json:"owner_id"`
	OwnerType          string    `gorm:"column:owner_type;type:varchar(16);not null;index" json:"owner_type"`
	Name               string    `gorm:"type:varchar(255);not null" json:"name"`
	ChainID            string    `gorm:"column:chain_id;type:varchar(128);not null" json:"chain_id"`
	ChainName          string    `gorm:"column:chain_name;type:varchar(255)" json:"chain_name"`
	NodeIDs            string    `gorm:"column:node_ids;type:text" json:"node_ids"`
	SubChainIDs        string    `gorm:"column:sub_chain_ids;type:text" json:"sub_chain_ids"`
	ConnectionsData    string    `gorm:"column:connections_data;type:text" json:"connections_data"`
	Payload            string    `gorm:"type:text" json:"payload"`
	DebugMode          bool      `gorm:"column:debug_mode;default:false" json:"debug_mode"`
	UseRelease         bool      `gorm:"column:use_release;default:false" json:"use_release"`
	NodeParamOverrides string    `gorm:"column:node_param_overrides;type:text" json:"node_param_overrides"`
	LastResult         string    `gorm:"column:last_result;type:text" json:"last_result"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// TableName 返回表名。
func (TestCaseModel) TableName() string {
	return "wf_test_cases"
}

// ToDef 将数据库模型转换为 TestCaseDef。
func (m *TestCaseModel) ToDef() *workflow.TestCaseDef {
	return &workflow.TestCaseDef{
		Project:            m.Project,
		CaseID:             m.CaseID,
		OwnerID:            m.OwnerID,
		OwnerType:          m.OwnerType,
		Name:               m.Name,
		ChainID:            m.ChainID,
		ChainName:          m.ChainName,
		NodeIDs:            m.NodeIDs,
		SubChainIDs:        m.SubChainIDs,
		ConnectionsData:    m.ConnectionsData,
		Payload:            m.Payload,
		DebugMode:          m.DebugMode,
		UseRelease:         m.UseRelease,
		NodeParamOverrides: m.NodeParamOverrides,
		LastResult:         m.LastResult,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}
}

// FromDef 从 TestCaseDef 填充模型字段。
func (m *TestCaseModel) FromDef(def *workflow.TestCaseDef) {
	m.Project = def.Project
	m.CaseID = def.CaseID
	m.OwnerID = def.OwnerID
	m.OwnerType = def.OwnerType
	m.Name = def.Name
	m.ChainID = def.ChainID
	m.ChainName = def.ChainName
	m.NodeIDs = def.NodeIDs
	m.SubChainIDs = def.SubChainIDs
	m.ConnectionsData = def.ConnectionsData
	m.Payload = def.Payload
	m.DebugMode = def.DebugMode
	m.UseRelease = def.UseRelease
	m.NodeParamOverrides = def.NodeParamOverrides
	m.LastResult = def.LastResult
}
