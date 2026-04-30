package models

import (
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// RootChainReleaseModel 根链发布版本持久化模型，对应 wf_root_chain_releases 表。
// 每次发布生成一条不可变快照记录，is_current=true 的版本为生产环境当前使用版本。
// 发布记录只增不改，便于审计和回滚。
type RootChainReleaseModel struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Project            string    `gorm:"column:project;type:varchar(128);uniqueIndex:uk_project_chain_version,priority:1;not null;index" json:"project"`
	ChainID            string    `gorm:"column:chain_id;type:varchar(128);uniqueIndex:uk_project_chain_version,priority:2;not null;index" json:"chain_id"`
	Version            int       `gorm:"column:version;uniqueIndex:uk_project_chain_version,priority:3;not null" json:"version"`
	Name               string    `gorm:"type:varchar(255);not null" json:"name"`
	Description        string    `gorm:"type:varchar(512)" json:"description"`
	DSLJSON            string    `gorm:"type:json;not null" json:"dsl_json"`
	NodeIDs            string    `gorm:"column:node_ids;type:text" json:"node_ids"`
	SubChainIDs        string    `gorm:"column:sub_chain_ids;type:text" json:"sub_chain_ids"`
	ConnectionsData    string    `gorm:"column:connections_data;type:text" json:"connections_data"`
	NodeParamOverrides string    `gorm:"column:node_param_overrides;type:text" json:"node_param_overrides"`
	IsCurrent          bool      `gorm:"column:is_current;default:false;index" json:"is_current"`
	PublishedAt        time.Time `gorm:"column:published_at;not null" json:"published_at"`
	CreatedAt          time.Time `json:"created_at"`
}

// TableName 返回表名。
func (RootChainReleaseModel) TableName() string {
	return "wf_root_chain_releases"
}

// ToDef 将数据库模型转换为 RootChainReleaseDef。
func (m *RootChainReleaseModel) ToDef() *workflow.RootChainReleaseDef {
	return &workflow.RootChainReleaseDef{
		Project:            m.Project,
		ChainID:            m.ChainID,
		Version:            m.Version,
		Name:               m.Name,
		Description:        m.Description,
		DSLJSON:            m.DSLJSON,
		NodeIDs:            m.NodeIDs,
		SubChainIDs:        m.SubChainIDs,
		ConnectionsData:    m.ConnectionsData,
		NodeParamOverrides: m.NodeParamOverrides,
		IsCurrent:          m.IsCurrent,
		PublishedAt:        m.PublishedAt,
	}
}

// FromDef 从 RootChainReleaseDef 填充模型字段。
func (m *RootChainReleaseModel) FromDef(def *workflow.RootChainReleaseDef) {
	m.Project = def.Project
	m.ChainID = def.ChainID
	m.Version = def.Version
	m.Name = def.Name
	m.Description = def.Description
	m.DSLJSON = def.DSLJSON
	m.NodeIDs = def.NodeIDs
	m.SubChainIDs = def.SubChainIDs
	m.ConnectionsData = def.ConnectionsData
	m.NodeParamOverrides = def.NodeParamOverrides
	m.IsCurrent = def.IsCurrent
	m.PublishedAt = def.PublishedAt
}
