package workflow

import (
	"context"
	"time"
)

// NodeTestRecordDef 单节点测试记录定义。
// 每次"测试单个节点"时，保存本次测试传入的参数、环境变量以及节点返回结果，方便后期查看。
type NodeTestRecordDef struct {
	// Project 所属项目
	Project string `json:"project"`
	// RecordID 测试记录唯一标识（如 M000001，同 project 内唯一）
	RecordID string `json:"record_id"`
	// NodeID 被测节点 ID
	NodeID string `json:"node_id"`
	// NodeName 被测节点名称（快照，便于查看）
	NodeName string `json:"node_name"`
	// EnvName 测试使用的环境名（决定 Redis 等依赖配置）
	EnvName string `json:"env_name,omitempty"`
	// TraceID 本次测试的分布式追踪 ID，用于回查本次执行产生的 activity 日志（wf_activity_logs.trace_id）
	TraceID string `json:"trace_id,omitempty"`
	// InputParams 测试时传入的参数（JSON 字符串）
	InputParams string `json:"input_params"`
	// EnvVars 测试时使用的环境变量（JSON 字符串，可选，配合 env_name 使用）
	EnvVars string `json:"env_vars,omitempty"`
	// Status 测试结果：success / fail
	Status string `json:"status"`
	// Result 节点返回结果（JSON 字符串）
	Result string `json:"result,omitempty"`
	// ErrorMsg 失败时的错误信息
	ErrorMsg string `json:"error_msg,omitempty"`
	// DurationMs 测试执行耗时（毫秒）
	DurationMs int64 `json:"duration_ms"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
}

// NodeTestRecordStore 单节点测试记录仓储接口。
type NodeTestRecordStore interface {
	// Create 创建测试记录
	Create(ctx context.Context, def *NodeTestRecordDef) error
	// GetByID 按项目+记录 ID 查询
	GetByID(ctx context.Context, project, recordID string) (*NodeTestRecordDef, error)
	// ListByNode 列出指定节点下所有测试记录（按时间倒序）
	ListByNode(ctx context.Context, project, nodeID string) ([]*NodeTestRecordDef, error)
	// Delete 删除测试记录（按 project + record_id）
	Delete(ctx context.Context, project, recordID string) error
	// NextRecordID 生成下一个测试记录自动 ID（如 M000001）
	NextRecordID(ctx context.Context, project string) (string, error)
}
