# go-plat-workflow

流程配置 / 可视化编排工作流引擎。支持在 Web 页面编排 Root Chain（根链），
将多个 Activity（业务活动）按连线组合成流程，发布后由调用方按 `ChainKey` 执行。

---

## Go 调用方法（实际工程示例）

本库设计为一侧**注册业务 Activity（worker 侧）**、另一侧**调用流程（调用方）**的协作模型：

```
调用方 ──HTTP──▶ workflow worker server ──MQ(asynq)──▶ 业务 worker 执行 Activity ──▶ 结果回传
```

- **业务 worker**：在你的服务进程里注册 Activity 处理函数，并启动消费端。
- **调用方**：通过 `InvokeWorkerFlowAPI` 传入 `ChainKey` 触发已发布的流程；流程中每个 Activity 节点在业务 worker 上执行。
- 二者需在同一 `project` / `env` / `namespace` 下才能对应上。

### 1. 引入依赖

```bash
go get github.com/magic-lib/go-plat-workflow
```

### 2. 初始化 WorkflowLogic（读取配置中心配置）

`NewWorkflowLogic` 从配置中心（`startupcfg.ConfigAPI`）按 `ConfigKey` 读出一组配置：
`project` / `env` / `api-token` / `domain`（指向 workflow worker server）。


```yaml
api:
  workflow-server:
    domain: http://localhost:8686
    auth:
      api-token: xxxxxx
    configs:
      project: aaaaaa
      env: test
```

```go
import (
    "github.com/magic-lib/go-plat-startupcfg/startupcfg"
    "github.com/magic-lib/go-plat-workflow/workflow"
)

const WorkflowServerConfigKey = "workflow-server" // 对应配置中心里的一组配置

// svcCfg 为你的配置中心实例（*startupcfg.ConfigAPI）
wfLogic, err := workflow.NewWorkflowLogic(svcCfg, &workflow.WfConfig{
    ConfigKey: WorkflowServerConfigKey,
})
if err != nil {
    // 处理初始化失败（project/env/api-token/domain 缺失等）
    return err
}
```

### 3. 注册 Activity（worker 侧，进程启动时执行一次）

把你的业务方法登记成 Activity，worker 在流程执行到对应节点时调用它们。
每个 Activity 用一个字符串名字标识（与编排页里配置的名字一致）。

```go
import (
    "context"
    "github.com/magic-lib/go-plat-utils/utils"
    "github.com/magic-lib/go-plat-workflow/workflow"
)

const ActivityNamespace = "your-namespace" // 与编排时填写的命名空间一致

// 你的业务方法（任意普通方法即可，签名：func(ctx, *Req) (*Resp, error)）
func (l *bankInfoLogic) GetM3BankKycListCache(ctx context.Context, req *GetM3BankKycListReq) (*BankKycListResp, error) {
    // ... 实际业务逻辑 ...
    return &BankKycListResp{}, nil
}

// 收集所有 Activity 到一个 map：名字 -> handler
func RegisterRuleMethod() map[string]utils.ContextAnyHandler {
    m := make(map[string]utils.ContextAnyHandler)
    m["M3_KYC_BANK_LIST"] = utils.ContextMethodToAnyHandlerOrNil[*GetM3BankKycListReq, *BankKycListResp](
        bankInfoLogic.GetM3BankKycListCache,
    )
    m["GET_DEFAULT_ACCOUNT"] = utils.ContextMethodToAnyHandlerOrNil[*SetDefaultAccountNoReq, *GetDefaultAccountNoResp](
        setDefaultLogic.GetDefaultAccount,
    )
    // ... 更多 Activity ...
    return m
}

// 在进程启动处（如 main.go 的初始化函数）注册并启动消费端
func startActivityService(svcCtx *svc.ServiceContext) {
    allMethod := RegisterRuleMethod()
    if err := wfLogic.RegisterNamespaceActivities(context.Background(), ActivityNamespace, allMethod); err != nil {
        // 注册失败（会与 worker server 建立连接、订阅队列、启动 asynq server）
        log.Printf("注册活动失败: %v", err)
        return
    }
}
```

`RegisterNamespaceActivities(ctx, namespace, handlerMap)` 会：
1. 把每个 `handler` 订阅到 `namespace` 下的对应 Activity（各自独立 asynq 队列）；
2. 全部订阅完成后自动 `Start()` 启动消费端 server（必须等全部注册完才能启动，否则会漏消费部分队列）。

> 若需要更细粒度控制，也可使用 `RegisterActivities(ctx, []*workflow.RegActivityInfo{...})` 自行构造
> `RegActivityInfo{Namespace, ActivityName, ActivityHandler}`（自带 `backoffretry` 重试注册）。

### 4. 调用流程（调用方）

通过 `ChainKey` 执行一条已发布的 Root Chain，返回流程结果（会阻塞等待 worker 执行完成，
默认最长等待 60s，对应 `config.DefaultTimeout`；如仅想异步触发，把 `Metadata.IsAsync = true`）。

```go
import (
    "context"
    "github.com/magic-lib/go-plat-workflow/workflow"
)

req := &workflow.InvokeRequest{
    ChainKey: "your-chain-key", // 编排页发布的根链 Key
    Metadata: workflow.InvokeMetadata{
        TraceID: "可选-链路追踪ID", // 不传会自动生成；用于按 traceId 查询整条流程耗时(node_log)
        IsAsync: false,           // true 表示异步触发，立即返回不等待结果
    },
    Payload: map[string]any{
        // 流程「调用传入」类型的参数（对应编排里 {{arguments.xxx}}）
        "user_id": 123456,
    },
    // Result: 可选，指定结果反序列化目标（按需）
}

result, err := wfLogic.InvokeWorkerFlowAPI(ctx, req)
if err != nil {
    // 处理失败（如 wait result timeout / activity 执行报错）
    return err
}
// result 即流程根节点返回值（RootResponse），类型 any，按需断言
```

### 5. 关键类型

```go
// 初始化配置：ConfigKey 指向配置中心里的一组配置（project/env/api-token/domain）
type WfConfig struct {
    ConfigKey   string // 默认 "workflow-server"
    ProjectKey  string // 该组内 project 字段名，默认 "project"
    EnvKey      string // 默认 "env"
    ApiTokenKey string // 默认 "api-token"
}

// 调用入参：通过 ChainKey 执行发布在线的根链
type InvokeRequest struct {
    ChainKey string         // 必填：根链 Key
    Metadata InvokeMetadata // 链路追踪 / 异步标记
    Payload  map[string]any // 流程入参（调用传入型参数）
    Result   any            // 可选：结果反序列化目标
}

type InvokeMetadata struct {
    TraceID string // 链路追踪 ID（用于查 node_log 耗时）
    IsAsync bool   // true=异步触发，立即返回
}
```

### 6. 注意事项

- **命名空间 / 环境一致性**：调用方与注册 Activity 的业务 worker 必须 `project`、`env`、`namespace` 完全一致，
  否则 worker 收不到任务、调用方会报 `wait result timeout for task`（任务一直停在队列里）。
- **先注册后调用**：流程执行到某 Activity 节点时，对应 worker 必须已经 `RegisterNamespaceActivities` 并 `Start()`，
  否则该任务无人消费、超时。
- **超时**：默认 60s（`config.DefaultTimeout`）。Activity 执行超过该时长、或 worker 侧未消费，都会超时返回错误。
- **TraceId**：建议传入，配合 node_log 可查询整条流程（含每个节点、以及 `flow_total` 汇总）的执行耗时。