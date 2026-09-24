# 更新日志

## 2026-09-24

### feat: 编排页 Connections 自定义关系缺失路由条件（switch_condition）校验提示

**背景**
Connections 的 relationType 允许填自定义关系（如 `K1`、`Stream`、业务分支名）。这类关系只有靠起点节点的
`switch_condition` 路由才会产生；若起点没有配置 `switch_condition`，运行时该分支**永远不会命中**，
而页面此前没有任何提示，极易漏写。`Success` / `Failure` 是默认分支，不需要路由条件。

**规则**
- `relationType` 留空视为 `Success` → 不校验；
- 值为 `Success` / `Failure`（忽略大小写）→ 不校验；
- 其余值 → 起点必须配置 `switch_condition`（实例覆盖 `node_switch_overrides` 优先，其次节点定义）。

**实现（`web/assets/common.js`）**
- `orchConnNeedsSwitch(type)`：判断该关系是否需要路由条件。
- `orchInstanceSwitchText(instanceId)`：取实例生效的 `switch_condition`（覆盖优先，回退节点定义，
  按 `instanceId.split('__')[0]` 回查 `_orchNodes`——实例摘要对象无 `configuration`）。
- `orchConnFromKind(fromId)`：区分起点类别，避免误报 ——
  `node`（Activity / CondSwitch，可配路由）、`sub`（Sub Chain，无法配置）、
  `unsupported`（其它节点类型，不支持该字段）、`unknown`（未找到定义，无法校验）。
  实例摘要无 `type` 时回查节点定义兜底。
- `refreshOrchConnWarns()`：逐行刷新警告，返回**必须修复**的问题列表（仅 `node` 类且条件为空算 error，
  其余只橙色提示不拦截）；触发点：`addOrchConnRow`、`onOrchChange`、`refreshOrchConnOptions`（改完覆盖即时生效）。
- 行内 `⚠️ 缺路由条件` 按钮（`onOrchConnWarnClick`）点击直接拉起该实例的 switch 编辑器（`orchOpenNodeSwitchEditor`）。
- Connections 区块顶部汇总条 `#orch-conn-cond-summary`（`orch.html` 新增容器）。
- 保存拦截：`generateOrchRootChain` 中若存在 error 级问题，滚动定位到首行并 `confirm` 二次确认，
  列出「起点 → 关系」清单；取消则不保存。**保留强制保存通道**，避免历史脏数据完全无法保存。

**样式（`web/assets/common.css`）**
- `.orch-cond-warn`（红）/ `[data-level="warn"]`（橙）、行高亮 `.conn-row-cond-error` / `.conn-row-cond-warn`、
  汇总条 `.orch-cond-summary`。

**fix（次日）：误报「缺路由条件」**
现象：24 条自定义关系全部报缺 `switch_condition`，但起点节点明明都配了。
根因两处：
1. **`window._orchNodes` 恒为 `undefined`** —— `_orchNodes` / `_orchSubChains` 是本文件顶层 `let` 声明，
   **不会挂到 `window`**（与 `window._orchNodeInstances` 那种显式赋值不同）。新函数里写 `window._orchNodes`
   → 节点定义永远取不到 → 一律判成空。同理 `window._orchSubChains` 导致起点是子链时也被误判。
   新增统一入口 `orchNodeDefById(nodeId)`（优先词法变量，兼容 `window._orchNodes`），
   并顺带修掉 `orchOpenNodeSwitchEditor` 里同一处历史写法。
2. **判定源不对**：应以「本页该实例实际生效的配置」为准，即 `_orchSwitchOverrides`（页面 🔀 编辑写入、
   保存到 `node_switch_overrides`）优先，其次才是节点定义默认值。改为 `orchInstanceSwitchInfo(instanceId)`
   返回 `{known, text, from}`，`from` 区分 `override` / `def`，提示文案里写明来源。
- 另：`known=false`（节点定义不在缓存中，如节点已禁用）时**只给橙色提示，绝不判为空**，避免误报。

---

### feat: Connections 默认分支（Success/Failure）仍配了路由条件时提示冲突

**背景**
`switch_condition` 一旦非空，节点输出就按条件走**自定义分支**（bool→True/False、字符串→同名分支），
只有在表达式结果既非 bool 又非字符串时才回落到 `Success` / `Failure`。
所以若把 relationType 改回 `Success` / `Failure`（或留空，等价于 Success）而起点仍留着路由条件，
该连线**大概率不会命中**，流程悄悄走错且很难排查。

**规则**
- `relationType` 为 `Success` / `Failure`（忽略大小写）或留空 → 检查起点**生效的** `switch_condition`；
- 按 `orchInstanceSwitchInfo` 取生效值（本链实例覆盖优先，其次节点定义）；`known=false` 不判（避免误报）；
- 非空 → 橙色 `⚠️ 路由冲突` + 行高亮。

**提示要点（区分来源，给出可操作的清除方式）**
- 来源 `override`（本链实例设置）：可在本页 🔀 清空，但**清空后会回退到节点定义值**，若节点定义也有值需一并清除；
- 来源 `def`（节点默认定义）：需到 Node 编辑页清除，仅在本页清空覆盖会回退回节点定义值。

**实现**
- `refreshOrchConnWarns` 新增反向分支（level `conflict`，与 `warn` 同为橙色、**不拦截保存**），
  冲突列表写入 `window._orchConnConflicts`；
- `updateOrchConnCondSummary(count, conflictCount)` 汇总条改为两段（缺条件红段 + 冲突橙段）；
- `generateOrchRootChain` 保存前按冲突列表 toast 提示（列出 `起点 → 关系（来源）`），**照常保存成功**。

---

### feat: 节点参数配置「引用节点」失效自动展开并提示（保存时统一检查，不阻断）

**背景**
参数来源选「引用节点」后，值形如 `{{steps.<上游节点>.arguments.<key>}}`。若该上游节点后续被删除，
或因连线调整**不再是当前节点的祖先**（移到别的流程分支），运行时根本取不到值，而页面此前仍显示为
「引用节点」，用户无从察觉，保存后问题被固化到 DSL。

**规则**
- 来源为 `upstream`（引用节点）且值非空 → 校验引用目标是否仍在 `getOrchRefNodeCandidates(instanceId)`
  （= 当前节点所有祖先，沿 Connections 反向可达）中；
- 目标不在其中（已删除 / 已移出链路）或值无法解析（历史脏数据）→ 判定失效；
- 空值视为「尚未选择」，不判失效。

**处理（只提示，不改配置）**
> 初版会「自动降级为调用传入」，用户反馈过重：若只是**临时断开连线**（后面还会接上），
> 全部被改写后要重新配置非常繁琐。因此改为**只提示、不改写**。

1. **自动展开**该实例所在的参数块（覆盖持久化的收起状态 `_orchCollapseState`，仅影响本次渲染、不落库）；
2. **保留用户原配置**：来源仍是「引用节点」、值仍是原引用字符串。
   控件走 `renderOrchRefNodeControlBroken` 专用渲染：节点下拉首项为
   `⚠️ <id>（已不在上游）` 并选中，同时**列出当前可用的上游节点**便于直接改选；
   字段下拉只有原引用值一项，保证 `storeParamPreset` 读到的仍是原值（不会被清空或串成别的节点）。
3. **行内红色提示**：`⚠️ 引用的节点 <id> 已不在该节点的上游（已删除或已移出链路），运行时可能取不到值。请重新选择来源，或重新连上连线后本提示会自动消失。`；
4. **Toast 汇总**「有 N 个参数…请确认后重新选择来源」并 `scrollIntoView` 定位到第一个失效参数。

**保存时统一检查（不阻断）**
- `generateOrchRootChain` 中新增 `collectOrchBrokenRefs()`：扫描参数面板当前 UI 状态，
  收集所有来源为 `ref_act` 且引用目标已失效的参数（读 `.param-ref-final`，回退 `.param-value-input`）。
- 有失效项时给出 toast 提示（列出 `实例.参数 → 引用`），但**照常保存成功**。

**实现**
- 新增 `resolveOrchParamPreset(nodeId, key)`：把原内联的「暂存优先 → DSL arguments 兜底」逻辑抽出，
  供渲染与主循环复用（预扫描需要它）。
- 新增 `orchParamRefBroken` / `markOrchParamRefBroken` / `clearOrchParamRefBroken` / `collectOrchBrokenRefs`；
- `renderOrchParamOverrides` 渲染前先预扫描生成 `brokenRefMap`，用于强制展开 + 标记；
- `onParamSrcChange` 开头调用 `clearOrchParamRefBroken`，用户重新选来源后提示即消失；
- 连线恢复后 `renderOrchParamOverrides` 重渲染，目标重新进入候选 → 提示自动消失。

---

### feat: 环境配置 Redis 增加「测试连接」按钮

**背景**
环境的 Redis 配置填完直接保存，只有真正跑起来（MQ worker / 日志收集器）才发现连不上，排查成本高。
需要在保存前就能用当前填写的配置探一下是否可连。

**后端**
- 新增 `workflow.TestRedisConnect(ctx, *RedisConfig)`（`workflow/activity_collector.go`）：
  `redis.NewClient` + `PING`，只读不写；返回 `ok / ping / latency_ms / server_version / db_size`。
  显式 `Dial/Read/WriteTimeout=5s`、`MaxRetries:1` —— 地址写错时快速失败并给出明确错误，
  不等默认重试链（默认多次退避会拖到数秒并刷日志）。
- 新增 `POST /api/env-configs/test-redis`（`web/server.go` `handleTestEnvRedis`）：接收
  `{project, env_name, redis_config}`，ctx 超时 8s，**不落库**。
  连通失败按 **HTTP 200 + `{ok:false, error}`** 返回（用户输入导致的失败不是服务端异常），前端据此展示原因。
  已验证前缀路由 `POST /api/env-configs/test-redis` 与既有 `GET|DELETE /api/env-configs/{env_name}` 不冲突。

**前端**
- Redis 配置卡片底部加「🔌 测试连接」按钮 + 结果区 `#env-redis-test-result`。
  **坑**：环境配置面板在 `index.html`（项目管理）与 `orch.html`（编排）里**各有一份 DOM**，
  只改一个页面另一个就没有按钮，必须同步改。
- `common.js`：新增 `envRedisFormConfig()`（表单取值，与 `saveEnvConfig` 复用）、
  `testEnvRedisConn()`（按钮置灰「测试中…」，结果区分绿/红并 toast，`finally` 恢复按钮）；
  `resetEnvConfigForm()` 一并清空测试结果。
- `common.css`：`.redis-test-ok` / `.redis-test-fail`。

**冒烟**：本机 `127.0.0.1:6379` → `✓ 连接成功 · PING=PONG · v8.0.1 · 6ms · 867 keys`；
错端口返回 `✗ 连接失败：dial tcp …: connect: connection refused`。

---

## 2026-09-23

### fix: 修复发布/回滚后线上仍走旧版本的问题（需重启才生效）

**问题现象**
点击发布后，新的请求仍然执行旧版本流程，只有重启进程后才会走新版本。

**根因**
`InvokeRootChain`（生产 invoke 入口）存在两层**进程内**缓存，且 key 相同（均为 `cacheKey = id.GetUUID(project+"-"+chainKey)`）：

1. `invokeRootChainMapCache`（cmap）—— 缓存解析后的 DSL；
2. rulego 引擎池 —— 缓存**已编译的引擎实例**（`StartWorkFlow` 以 `PoolKey=cacheKey` 注册/获取）。

发布时 `ClearChainRootByKey` 仅清除了第 1 层，第 2 层的旧引擎实例无人清理；而 `StartWorkFlow(UseCache=true)` 命中旧实例后直接复用，
重建分支 `ReloadSelf` 又被 `!actConfig.UseCache` 拦截，导致旧引擎持续执行旧 DSL，只能靠重启清空引擎池。

> 注：`ExecutePublishedRootChain`（`UseRelease=true` 路径）每次请求都会重新加载并重载，本身不受影响。该症状仅出现在 `InvokeRootChain` 路径。

**修复方案**

1. **引擎池 key 带版本号**：`PoolKey` 由 `cacheKey` 改为 `<cacheKey>@<version>`。
   发布后版本号递增 → 新请求必然未命中旧实例 → 用新 DSL 重建引擎 → **立即走新流程**；
   旧实例保留在池中且**不会被 Stop**，正在其上执行的存量流程可安全跑完 → **不会打断任何进行中的流程**。
2. **引擎池版本错峰回收**：新增登记表，按策略回收历史版本实例（详见下方 feat）。
3. **跨副本广播失效**：多副本部署时通过 Redis pub/sub 通知各副本失效本地缓存（详见下方 feat）。

**关键约束**
绝不能直接 `rulego.Del(key)` 清理引擎池：其内部 `Stop(context.Background())` 仅等待 **10s** 即 `ForceStop`，
会强制取消上下文**中断正在执行的长流程**。本方案改用「引擎池 key 版本号化 + 优雅排空」规避该风险。

---

### feat: 引擎池历史版本错峰回收（数量 + 存活时间双策略）

发布/回滚会在引擎池中留下历史版本实例（保证存量流程跑完的代价），需按策略回收，避免无限增长。

**新增配置项**（`app.yaml` → `custom.normal`）

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `root_chain_pool_max_versions` | `5` | 每条根链最多保留的版本实例数（含在线版本）。`0` = 不限制数量，仅按时间清理 |
| `root_chain_pool_ttl_minutes` | `30` | 非在线版本最长存活时间（分钟）。`0` = 不限制时间，仅按数量维持 |

**组合语义**

| 配置 | 行为 |
|---|---|
| 仅数量（ttl=0） | 始终维持该数量，超出部分按发布时间先后**立即**清理 |
| 仅时间（数量=0） | 按发布时间**错峰**清理，**最终只剩在线版本** |
| 两者都配 | 最终保留该数量（含在线），超出部分按时间错峰清理 → 多留版本，**回滚到已存在的版本会更快** |

**清理规则**
- 排序依据 **`release.PublishedAt`（发布时间）而非版本号**：回滚场景下老版本号可能重新成为在线版本，只有发布时间能真实反映先后。
- **错峰**：如需清理 3 个且 TTL=30min，则最早发布的等待 30min 后清理，再过 30min 清下一个，依次进行（非同时过期）。
- **在线版本永不被清理**，保证始终至少保留一个可用版本。
- 优雅排空`poolStopGrace = 30min`：回收前先用长宽限期 `Stop` 让 in-flight 流程自然跑完，再摘除登记，
  规避 `rulego.Del` 内置 10s 超时强制中断的问题。

> 注意：配置在进程内只读取一次（`sync.Once`），修改 `app.yaml` 后需**重启**生效。

---

### feat: 跨副本根链失效广播（Redis pub/sub）

**问题**
DSL 缓存与引擎池都是进程内缓存。多副本部署时，发布请求只会打到其中一个副本，
只有该副本失效缓存并切到新版本，其余副本仍执行旧版本，直到被回收策略清理或重启。

**方案**
新增 `workflow/service/rootchain_pubsub.go`，频道 `workflow:rootchain:invalidate`。

- **复用现有配置，无新增配置项**：直接使用 `env_config` 中各环境已配置的 Redis。
  - 广播端：向该 project 下所有配置了 Redis 的 env 各发一次（按 `addr|db` 去重）；
  - 订阅端：`ListAll` 全部 env 的 Redis，去重后各建一个订阅，每 3 分钟自动对齐 env 增删。
- **防循环**：事件携带 `sender_id`（hostname-pid），订阅端忽略自己发出的消息；且订阅端只失效、不再广播。
- **降级**：Redis 未配置或不可用时仅记录日志并跳过，等同单机行为，不影响主流程。

**新增配置项**

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `root_chain_broadcast_enabled` | `true` | 是否启用跨副本失效广播。多副本部署需开启；**单机部署可设为 `false`**，不创建 Redis 订阅与后台协程 |

> 默认 `true` 的原因：多副本若漏配会静默退化成"只切一个副本"的严重问题且不易察觉；而单机设为 `true` 仅多一个降级订阅，无害。

**接入点**
统一入口 `WorkflowService.NotifyRootChainChanged(project, chainKey, reason)`（本地失效 + 异步广播）：
发布（`publish`）、回滚（`rollback`）、设为生效（`set_current`）、删除根链（`delete`）。
关闭广播时 `invalidateBus` 为 `nil`，自动退化为仅本地失效。

---

### fix: 修复回滚到的版本被误清理的问题

发布/回滚/设为生效后，登记表中的「在线版本」仍停留在旧值，导致**回滚到的版本被判定为非在线**而成为清理候选——
与"多留几个版本以便回滚更快"的目标相悖（`max_versions=1` 或 `ttl=0` 时会被立即清理）。

**修复**：新增在线版本未知哨兵 `currentUnknown = -1`。版本变更后标记为未知，
巡检遇到未知状态**保守跳过**，待下次调用重新解析出真实在线版本后恢复巡检。

---

### feat: Nodes / Activities 列表展示「已发布引用」明细

**背景**
此前 node / activity 只有布尔值 `published_in_root_chain`：被引用时按钮置灰，但**看不出被哪些根链引用**，
需要人工逐个反查根链发布快照才能处理，排查成本高。

**后端改动**

- 新增类型（`workflow/types.go`，均为列表接口实时计算、不入库）：
  - `PublishedRootChainRef`：`chain_id` / `name` / `version`；
  - `RefNodeInfo`：`node_id` / `name` / `published`；
  - `NodeDef.PublishedRootChains`、`ActivityDef.PublishedRootChains`、`ActivityDef.RefNodes`。
- `publishedRefIndex` 增加明细索引 `nodeChains` / `activityChains`（键 → 根链集合，去重），
  配套 `addRef`（按根链去重）与 `sortedRefs`（按 `ChainID` 排序，保证前端展示顺序稳定）。
- `ListNodes` 填充 `published_root_chains`；`ListActivities` 填充 `published_root_chains` 与 `ref_nodes`。
- 新增 `buildActivityRefNodes`：构建「activity → 引用它的 Node」索引（`nodeActivityRefs` 解析节点配置），
  并用 `idx.nodes` 标注每个 Node 自身是否已发布，用于区分线上 / 草稿影响面。
  构建失败仅 `Warn` 降级为空列表，**不影响主列表返回**。
- 新增 `NodePublishedRootChains(ctx, project, nodeID)` 便于单节点查询。

> `PublishedRootChains` 与 `RefNodes` 是两个不同维度：前者是**最终生效的根链**，后者是**直接使用该 activity 的节点**，
> 用于在修改 activity 前评估影响范围。

**前端改动**

- Nodes 列表新增「已发布引用」列（7 列 → 8 列）；Activities 列表新增「已发布引用」「引用 Nodes」两列（8 列 → 10 列）。
- 计数 + 悬停浮层（复用 `attachCountPopover`）：
  - 已发布引用：列出根链 ID、名称与版本号（`vN`）；
  - 引用 Nodes：列出 Node ID、名称，并标注 `🔒已发布` / `草稿`，顶部提示「其中 N 个已发布到根链，修改将影响线上」。
- **fix**：修正 Nodes 标签筛选失效问题——原实现取 `r.cells[5]`（实为命名空间列），标签列应为 `cells[4]`。
- Nodes 表格列宽调整：Node ID 与操作列由百分比改为固定像素（`110px` / `300px`），
  避免宽屏留白与窄屏按钮被压缩显示不全；并移除操作列 `<td>` 上的 flex（改为内层 `<div class="actions">`），
  修复 `table-layout: fixed` 下操作列按钮溢出的问题。
