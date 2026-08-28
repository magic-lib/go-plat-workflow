/*
 * Copyright 2023 The RuleGo Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package commnode

import (
	"context"
	"encoding/json"
	"github.com/magic-lib/go-plat-utils/goroutines"

	"github.com/magic-lib/go-plat-utils/plugins/activity"
)

// ActivityMQRequest 通过 MQ 执行单个 Activity 的请求。
// 该结构仅依赖 commnode 包内可访问的类型（activity.Activity / 基础类型），
// 由 workflow 包（持有 MQExecutor）实现 ActivityMQExecutor 接口时转换为内部类型。
type ActivityMQRequest struct {
	// Act 待执行的 Activity 定义（含 act_namespace / act_name 等）。
	Act *activity.Activity
	// Project 所属项目，用于定位环境配置与 Redis 命名空间。
	Project string
	// Env 执行环境名，用于解析 Redis 配置并构建 MQ worker。为空时无法走 MQ。
	Env string
	// RootChainID 根链 ID（用于链路追踪与日志聚合），可为空。
	RootChainID string
	// TraceID 链路追踪 ID，可为空（实现侧会兜底生成）。
	TraceID string
	// StepID 当前步骤 ID（作为 spanID 写入日志），通常为 activity 的 stepId。
	StepID string
	// Input 执行入参（map 形式），对应 activity 执行所需的参数上下文。
	Input map[string]any
}

// ActivityMQResult 通过 MQ 执行单个 Activity 的结果。
type ActivityMQResult struct {
	// Data 解析后的结果 map（便于回写到 ParamCtx）。
	Data map[string]any
	// RawData 结果的原始 JSON 字符串。
	RawData string
}

// ActivityMQExecutor 通过 MQ 同步调用分布式 worker 执行单个 Activity 的能力抽象。
// 定义在 commnode 包内（而非 workflow 包），以避免 commnode -> workflow 的循环依赖：
// commnode 仅依赖此接口，真正的实现由 workflow.MQExecutor 提供并注入。
type ActivityMQExecutor interface {
	// ExecActivityViaMQ 通过 MQ 同步执行指定 Activity，返回执行结果。
	// 入参 env 为空、或实现侧无法解析 Redis 配置时，返回明确错误，调用方应回退到本地执行。
	ExecActivityViaMQ(ctx context.Context, req *ActivityMQRequest) (*ActivityMQResult, error)
}

// defaultActivityMQExecutor 包级默认的 MQ 执行器（由 workflow 包在构造 MQExecutor 时注入）。
var defaultActivityMQExecutor ActivityMQExecutor

// SetActivityMQExecutor 注入（或清空）包级默认 MQ 执行器。
// workflow 包在创建 MQExecutor 实例时应调用此方法，使 ActivityNode 在执行时可走 MQ 远程执行。
// 传入 nil 可清除（回退到本地执行）。
func SetActivityMQExecutor(executor ActivityMQExecutor) {
	defaultActivityMQExecutor = executor
}

// ActivityStoreFetcher 获取 Activity 模板定义的能力抽象。
// 定义在 commnode 包内（而非 workflow 包），以避免 commnode -> workflow(repo) 的循环依赖：
// commnode 仅依赖此接口与自身轻量结构，真正的实现由 workflow.ActivityStore（repo.ActivityRepo）
// 经 workflow 包适配后注入。
// 由于节点编排里的 activity.Activity.Id 不能稳定对应 ActivityDef.ID，
// 这里使用 ActNamespace + ActName（对应 activity 模板唯一键 uk_project_ns_name）来定位模板，
// 从而拿到 return_values 以构造 RequestActivity 所需的 returnBindConfig。
type ActivityStoreFetcher interface {
	// GetByNamespaceName 按项目 + act_namespace + act_name 查询 activity 模板（唯一键定位）。
	GetByNamespaceName(ctx context.Context, project, actNamespace, actName string) (*ActivityTemplateDef, error)
}

// ActivityTemplateDef commnode 仅需关心的 activity 模板片段：返回值映射配置。
// 用 commnode 包内结构而非 workflow.ActivityDef，避免 commnode 反向依赖 workflow 包。
type ActivityTemplateDef struct {
	// ReturnValues 返回值设置列表（每一项从活动返回值 map 中提取指定 key 重命名输出），JSON 原始串。
	ReturnValues json.RawMessage `json:"return_values"`
}

// defaultActivityStore 包级默认的 activity 模板仓储（由 workflow 包在初始化时注入）。
var defaultActivityStore ActivityStoreFetcher

// SetActivityStore 注入（或清空）包级默认 activity 模板仓储。
// workflow 包在构造 WorkflowService（持有 ActivityRepo）时应调用此方法，
// 使 ActivityNode 在执行单个 Activity 时能查到模板的 return_values 并传入 RequestActivity。
// 传入 nil 可清除（此时 returnBindConfig 回退为 nil）。
func SetActivityStore(store ActivityStoreFetcher) {
	defaultActivityStore = store
}

// NodeLogSaver 将 node 运行日志直接落库的能力抽象。
// 定义在 commnode 包内（而非 workflow 包），避免 commnode -> workflow(repo) 的循环依赖：
// commnode 仅依赖此接口与自身 NodeLogDef 结构，真正的实现由 workflow 层适配 repo.NodeLogRepo 后注入。
// pushNodeLog 在注入后会直接将 NodeLogDef 写入数据库，而非经 redis 中转由 collector 消费。
type NodeLogSaver interface {
	// CreateNodeLog 直接创建一条 node 运行日志。
	CreateNodeLog(ctx context.Context, def *NodeLogDef) error
}

// defaultNodeLogSaver 包级默认的 node 日志落库实现（由 workflow 包在初始化时注入）。
var defaultNodeLogSaver NodeLogSaver

// SetNodeLogSaver 注入（或清空）包级默认 node 日志落库实现。
// workflow 包在构造 WorkflowService（持有 NodeLogRepo）时应调用此方法，
// 使 node 运行日志直接写入 wf_node_logs，无需经过 redis 中转。
// 传入 nil 可清除（此时 pushNodeLog 回退为 redis 推送，保持兼容）。
func SetNodeLogSaver(saver NodeLogSaver) {
	defaultNodeLogSaver = saver
}

// AlertSender 发送告警通知的能力抽象（如飞书自定义机器人）。
// 定义在 commnode 包内，避免 commnode -> workflow 的循环依赖：
// 真正的实现（读取飞书 webhook 并发消息）由 workflow 包适配后注入。
// 实现侧应在 webhook 未配置时静默 no-op（即"配置了机器人地址才发"）。
type AlertSender interface {
	// SendAlert 发送一条告警。title 为标题，content 为正文。
	SendAlert(ctx context.Context, title, content string)
}

// defaultAlertSender 包级默认告警发送器（由 workflow 包注入）。
var defaultAlertSender AlertSender

// SetAlertSender 注入（或清空）包级默认告警发送器。
// workflow 包初始化时应调用此方法，使 Node 执行失败等场景能推送告警到飞书群。
// 传入 nil 可清除（回退为静默跳过）。
func SetAlertSender(sender AlertSender) {
	defaultAlertSender = sender
}

// sendAlert 在已注入告警发送器时异步发送告警（webhook 未配置时实现侧 no-op）。
// 异步执行避免阻塞主流程；内部 recover 防止告警逻辑异常影响业务。
func sendAlert(ctx context.Context, title, content string) {
	if defaultAlertSender == nil {
		return
	}
	goroutines.GoAsync(func(params ...any) {
		defaultAlertSender.SendAlert(ctx, title, content)
	})
}
