# 更新日志

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
