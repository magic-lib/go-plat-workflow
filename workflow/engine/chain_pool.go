package engine

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// flowNodeType rulego 子链调用节点（components/flow.ChainNode）的类型。
const flowNodeType = "flow"

// NormalizeFlowNodes 补齐 flow 节点调用子链所需的配置，返回是否有改动：
//   - rulego 的 ChainNode 只认 targetId；管理端 builder 历史上只写了 ruleChainId（project:chainID），
//     已发布的版本快照里也只有 ruleChainId，这里用它补上 targetId，否则执行时报 ruleChain id= not found。
//   - extend 未配置时设为 true：合并模式（extend=false）会把输出包成 []WrapperMsg，
//     下游节点按 FlowContext 解析会失败；extend=true 时子链各分支的输出原样转发给下游。
func NormalizeFlowNodes(chain *types.RuleChain) bool {
	if chain == nil {
		return false
	}
	changed := false
	for _, node := range chain.Metadata.Nodes {
		if node == nil || node.Type != flowNodeType {
			continue
		}
		if node.Configuration == nil {
			node.Configuration = types.Configuration{}
		}
		if target, _ := node.Configuration["targetId"].(string); target == "" {
			if ref, _ := node.Configuration["ruleChainId"].(string); ref != "" {
				node.Configuration["targetId"] = ref
				changed = true
			}
		}
		if _, ok := node.Configuration["extend"]; !ok {
			node.Configuration["extend"] = true
			changed = true
		}
	}
	return changed
}

// NormalizeFlowDSL 对 DSL JSON 做 NormalizeFlowNodes，不修改调用方持有的 RuleChain 对象
// （缓存里的 DSL 会被并发请求共享，直接改其 Configuration map 会并发写 map）。
// 解析失败时原样返回，交给 rulego 报错。
func NormalizeFlowDSL(dsl []byte) []byte {
	chain := new(types.RuleChain)
	if err := json.Unmarshal(dsl, chain); err != nil {
		return dsl
	}
	if !NormalizeFlowNodes(chain) {
		return dsl
	}
	out, err := json.Marshal(chain)
	if err != nil {
		return dsl
	}
	return out
}

// LoadOrReload 按 key 把 DSL 加载到 rulego 默认引擎池。
// rulego.New 遇到已存在的 key 会直接返回旧实例、不会更新 DSL，所以这里先查池：
// 已存在且 DSL 有变化时 ReloadSelf 热更新，没有变化直接复用；不存在才新建。
// 加载前会先 NormalizeFlowDSL，保证 flow 节点能调到子链。
func LoadOrReload(key string, dsl []byte, opts ...types.RuleEngineOption) (types.RuleEngine, error) {
	dsl = NormalizeFlowDSL(dsl)
	if ins, ok := rulego.Get(key); ok {
		if !cond.IsSameJson(string(ins.DSL()), string(dsl)) {
			if err := ins.ReloadSelf(dsl); err != nil {
				return nil, fmt.Errorf("reload rule chain %s failed: %w", key, err)
			}
		}
		return ins, nil
	}
	return rulego.New(key, dsl, opts...)
}

// envRedisConfigs 进程内按 project+env 保存 Redis 连接配置（含密码）。
// Redis 配置不随 msg.Metadata 透传：Metadata 会被调试日志、出错日志整份写进 MySQL，密码会一起落库。
// 执行入口登记，节点按 metadata 里的 project/env 取回。
var envRedisConfigs sync.Map

func envRedisConfigKey(project, env string) string {
	return project + "|" + env
}

// SetEnvRedisConfig 登记 project+env 的 Redis 配置；cfg 为 nil 时忽略。
func SetEnvRedisConfig(project, env string, cfg *conn.Connect) {
	if cfg == nil {
		return
	}
	envRedisConfigs.Store(envRedisConfigKey(project, env), cfg)
}

// EnvRedisConfig 返回已登记的 project+env 的 Redis 配置，未登记返回 nil。
func EnvRedisConfig(project, env string) *conn.Connect {
	if v, ok := envRedisConfigs.Load(envRedisConfigKey(project, env)); ok {
		return v.(*conn.Connect)
	}
	return nil
}
