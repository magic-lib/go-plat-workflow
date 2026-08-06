package models

import (
	"time"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// SubChainModel 子规则链持久化模型，对应 wf_sub_chains 表。
type SubChainModel struct {
	ID                 uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Project            string         `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_chain_id,priority:1;not null;index" json:"project"`
	ChainID            string         `gorm:"column:chain_id;type:varchar(128);uniqueIndex:uk_project_chain_id,priority:2;not null" json:"chain_id"`
	Name               string         `gorm:"type:varchar(255);not null" json:"name"`
	Description        string         `gorm:"type:varchar(512)" json:"description"`
	DSLJSON            string         `gorm:"type:json;not null" json:"dsl_json"`
	Status             int8           `gorm:"default:1;index" json:"status"`
	NodeIDs            string         `gorm:"column:node_ids;type:text" json:"node_ids"`
	SubChainIDs        string         `gorm:"column:sub_chain_ids;type:text" json:"sub_chain_ids"`
	ConnectionsData    string         `gorm:"column:connections_data;type:text" json:"connections_data"`
	NodeParamOverrides string         `gorm:"column:node_param_overrides;type:text" json:"node_param_overrides"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 返回表名。
func (SubChainModel) TableName() string {
	return "wf_sub_chains"
}

// ToDef 将数据库模型转换为 SubChainDef。
func (m *SubChainModel) ToDef() *workflow.SubChainDef {
	return &workflow.SubChainDef{
		Project:            m.Project,
		ChainID:            m.ChainID,
		Name:               m.Name,
		Description:        m.Description,
		DSLJSON:            m.DSLJSON,
		Status:             m.Status,
		SubChainIDs:        m.SubChainIDs,
		NodeIDs:            m.NodeIDs,
		ConnectionsData:    m.ConnectionsData,
		NodeParamOverrides: m.NodeParamOverrides,
	}
}

// FromDef 从 SubChainDef 填充模型字段。
func (m *SubChainModel) FromDef(def *workflow.SubChainDef) {
	m.Project = def.Project
	m.ChainID = def.ChainID
	m.Name = def.Name
	m.Description = def.Description
	m.DSLJSON = def.DSLJSON
	m.Status = def.Status
	m.NodeIDs = def.NodeIDs
	m.SubChainIDs = def.SubChainIDs
	m.ConnectionsData = def.ConnectionsData
	m.NodeParamOverrides = def.NodeParamOverrides
}
