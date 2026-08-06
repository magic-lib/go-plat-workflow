# Workflow

基于 [rulego](https://github.com/rulego/rulego) 规则链引擎的工作流持久化与执行库。

## 功能

- **Node 管理**：将 rulego 节点配置（type、configuration 等）持久化到 MySQL，支持复用
- **SubChain 管理**：将子规则链 DSL 存储到数据库，可被 RootChain 引用
- **RootChain 组装**：动态选择节点和子链，定义连接关系，自动生成合法的 rulego DSL JSON
- **流程执行**：加载 DSL 到 rulego 引擎池，通过 `OnMsgAndWait` 同步执行并获取结果

## 架构

```
workflow/
├── types.go                # 核心类型和接口定义
├── models/
│   ├── node.go             # wf_nodes GORM 模型
│   ├── sub_chain.go        # wf_sub_chains GORM 模型
│   └── root_chain.go       # wf_root_chains GORM 模型
├── repo/
│   ├── node_repo.go        # 节点仓储（CRUD）
│   ├── sub_chain_repo.go   # 子链仓储（CRUD）
│   └── root_chain_repo.go  # 根链仓储（CRUD）
├── builder/
│   └── dsl_builder.go      # DSL 组装器
├── engine/
│   └── engine.go           # rulego 执行封装
├── service/
│   └── workflow_service.go # 编排服务（对外 API）
└── README.md
```

## 快速开始

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    
    "github.com/magic-lib/go-plat-workflow/workflow"
    "github.com/magic-lib/go-plat-workflow/workflow/service"
)

func main() {
    ctx := context.Background()
    
    // 1. 连接数据库
    db, _ := gorm.Open(mysql.Open("user:pass@tcp(127.0.0.1:3306)/workflow?charset=utf8mb4&parseTime=True"), &gorm.Config{})
    
    // 2. 创建 WorkflowService（自动建表）
    ws, _ := service.NewWorkflowService(db)
    defer ws.Shutdown(ctx)
    
    // 3. 注册可复用的节点
    ws.RegisterNode(ctx, &workflow.NodeDef{
        NodeID: "transform_1",
        Name:   "JSON 转换节点",
        Type:   "jsTransform",
        Configuration: json.RawMessage(`{"jsScript": "msg.newField='transformed'; return {'msg':msg,'metadata':metadata,'msgType':msgType};"}`),
        Status: 1,
    })
    
    // 4. 注册子链（可选）
    subChainDSL := `{
        "ruleChain": {"id": "sub_chain_1", "name": "数据处理子链"},
        "metadata": {
            "nodes": [
                {"id": "s1", "type": "jsTransform", "name": "增强", 
                 "configuration": {"jsScript": "msg.enhanced=true; return {'msg':msg,'metadata':metadata,'msgType':msgType};"}}
            ],
            "connections": []
        }
    }`
    ws.RegisterSubChain(ctx, &workflow.SubChainDef{
        ChainID: "sub_chain_1",
        Name:    "数据处理子链",
        DSLJSON: subChainDSL,
        Status:  1,
    })
    
    // 5. 组装 RootChain
    def, _ := ws.BuildRootChain(ctx, &workflow.BuildRequest{
        ChainID:   "my_workflow",
        ChainName: "我的工作流",
        NodeIDs:   []string{"transform_1"},
        SubChainIDs: []string{"sub_chain_1"},
        Connections: []workflow.ConnectionDef{
            {FromID: "transform_1", ToID: "sub_chain_1", Type: "Success"},
        },
    })
    
    // 6. 加载并执行
    ws.LoadChain(ctx, "my_workflow")
    result, _ := ws.ExecuteRootChain(ctx, "my_workflow", `{"input": "hello"}`)
    fmt.Println("执行结果:", result)
    
    // 7. 一步完成（组装 + 加载 + 执行）
    result, _ = ws.BuildLoadAndExecute(ctx, &workflow.BuildRequest{
        ChainID:   "quick_workflow",
        ChainName: "快速工作流",
        NodeIDs:   []string{"transform_1"},
        Connections: []workflow.ConnectionDef{},
    }, `{"input": "quick"}`)
    fmt.Println("快速执行结果:", result)
}
```

## API 参考

### WorkflowService

| 方法 | 说明 |
|---|---|
| `NewWorkflowService(db)` | 创建服务实例，自动迁移数据库表 |
| `RegisterNode(ctx, def)` | 注册节点到数据库 |
| `GetNode(ctx, nodeID)` | 获取单个节点 |
| `ListNodes(ctx)` | 列出所有可用节点 |
| `UpdateNode(ctx, def)` | 更新节点配置 |
| `DeleteNode(ctx, nodeID)` | 软删除节点 |
| `RegisterSubChain(ctx, def)` | 注册子链 |
| `GetSubChain(ctx, chainID)` | 获取单个子链 |
| `ListSubChains(ctx)` | 列出所有可用子链 |
| `UpdateSubChain(ctx, def)` | 更新子链 |
| `DeleteSubChain(ctx, chainID)` | 软删除子链 |
| `BuildRootChain(ctx, req)` | 组装 RootChain DSL |
| `GetRootChain(ctx, chainID)` | 获取单个根链 |
| `ListRootChains(ctx)` | 列出所有根链 |
| `DeleteRootChain(ctx, chainID)` | 软删除根链 |
| `LoadChain(ctx, chainID)` | 加载根链到 rulego 引擎池 |
| `ExecuteRootChain(ctx, chainID, payload)` | 同步执行根链并返回 JSON 结果 |
| `UnloadChain(ctx, chainID)` | 从引擎池卸载根链 |
| `BuildLoadAndExecute(ctx, req, payload)` | 一步完成：组装→加载→执行 |
| `Shutdown(ctx)` | 停止所有引擎并清理资源 |

### 数据结构

- **NodeDef**：节点定义，对应 rulego RuleNode（含 type、configuration 等）
- **SubChainDef**：子链定义，存储完整 SubChain DSL JSON
- **RootChainDef**：根链定义，存储组装后的 RootChain DSL JSON
- **BuildRequest**：组装请求（nodeIds + subChainIds + connections）
- **ConnectionDef**：节点连接关系（FromID + ToID + Type）

### 关键概念

- **规则链** 是 rulego 的核心抽象，由节点（nodes）和连接（connections）组成的有向无环图
- **RootChain**（根链）是入口规则链，设置 `Root: true`
- **SubChain**（子链）是可复用的子规则链，通过 `TellFlow` 在节点中被引用
- **引擎池**（Pool）管理所有加载的规则链实例，按 chainID 索引

## 数据库表

| 表名 | 用途 |
|---|---|
| `wf_nodes` | 可复用的节点配置 |
| `wf_sub_chains` | 子规则链 DSL |
| `wf_root_chains` | 组装后的根规则链 DSL |
