package rulegox

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/logs"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/samber/lo"
	"log"
	"sync"
	"time"
)

// metadataRedisConfigKey ActivityMetaData.RedisConfig 的 json 字段名。Redis 配置改由 engine.SetEnvRedisConfig 登记，
// 写入 msg.Metadata 时剔除，避免密码随 Metadata 进日志。
const metadataRedisConfigKey = "redis_config"

// FlowCompletedHook 在整条流程（每次 StartWorkFlow 执行的根链/子链）执行结束时触发，
// 入参为流程级元数据、总耗时（毫秒）、流程级错误。由 commnode 包在注入 NodeLogSaver 时注册，
// 用于把“整个流程耗时”作为一条 NodeID 为空的汇总记录写入 wf_node_logs，便于按 traceId 查询整条流程耗时。
// 未注入（nil）时不触发，保持兼容。
var FlowCompletedHook func(metaData *ActivityMetaData, durationMs int64, runErr error)

type ActivityFlowConfig struct {
	RootChainDSL *types.RuleChain //根配置
	// Deprecated: RuleChainBaseInfo 不含节点，按它注册出来的是空链，注册 key 也与 flow 节点引用的
	// project:chainID 对不上，子链实际调不到。请改用 SubChains。
	SubChainDSL []*types.RuleChainBaseInfo
	// SubChains 根链 flow 节点（含嵌套）引用的子链完整 DSL，按 project:chainID 注册到引擎池
	SubChains   []*types.RuleChain
	FlowContext *paramx.FlowContext //前端传入的参数
	MsgType     string
	IsAsync     bool
	EndFunc     func(ctx context.Context, relationType string, param *paramx.FlowContext, err error)
	// UseCache 是否复用全局 rulego 引擎池中已存在的同名链（以 RootChainID 为 key）。
	// true：命中则直接复用旧实例，性能更好、配置保持稳定（适合正式环境高频复用）；
	// false：每次都基于最新 DSL 通过 rulego.New 覆盖重建，配置更新可立即生效（适合 node 测试/开发环境）。
	// 默认 false（避免缓存导致配置修改不生效）。
	UseCache bool
	// PoolKey 可选：rulego 引擎池的注册 key。为空时回退到 RootChainID。
	// 用于「发布/invoke」路径：以确定性 UUID 作为池 key 与草稿链实例隔离，
	// 但节点日志 root_chain_id 仍写入真实根链 id（如 R000005），避免被污染成 UUID。
	PoolKey string
	// RootChainReleaseID 可选：本次执行对应的根链发布版本标识（形如 R000005@3），
	// 透传给 ActivityMetaData 后写入节点日志，用于追溯当时执行的是哪个发布版本。
	RootChainReleaseID string
}

type ActivityMetaData struct {
	RootChainID string `json:"root_chain_id,omitempty"`
	// RootChainName 根链（流程）名称，由 StartWorkFlow 自 DSL 的 RuleChain.Name 填充，
	// 随 msg.Metadata 透传给各节点，用于失败告警/日志展示更直观的流程名。
	RootChainName string        `json:"root_chain_name,omitempty"`
	PoolKey       string        `json:"pool_key,omitempty"`
	TraceId       string        `json:"trace_id,omitempty"`
	Env           string        `json:"env,omitempty"`
	Project       string        `json:"project,omitempty"`
	RedisConfig   *conn.Connect `json:"redis_config,omitempty"`
	// RootChainReleaseID 本次执行对应的根链发布版本标识（形如 R000005@3），写入 wf_node_logs.root_chain_release_id。
	RootChainReleaseID string `json:"root_chain_release_id,omitempty"`
	// ChainID 当前节点所在的链 ID：由调用子链节点在进入子链前写入（子链 ID），子链结束后恢复；为空表示在根链里。
	ChainID string `json:"chain_id,omitempty"`
	// CallPath 子链调用路径：由调用子链节点在进入子链前追加自身实例 ID（嵌套用 "/" 拼接），子链结束后恢复。
	CallPath string `json:"call_path,omitempty"`
}

func StartWorkFlow(ctx context.Context, actConfig *ActivityFlowConfig, metaData *ActivityMetaData) error {
	if actConfig == nil {
		return fmt.Errorf("参数不能为空")
	}
	if actConfig.RootChainDSL == nil || actConfig.FlowContext == nil {
		return fmt.Errorf("根规则链DSL不能为空")
	}
	if metaData == nil {
		return fmt.Errorf("元信息不能为空")
	}
	if metaData.Env == "" {
		return fmt.Errorf("环境不能为空")
	}
	metaData.TraceId = id.GetUUID(metaData.TraceId)
	metaData.RootChainID = actConfig.RootChainDSL.RuleChain.ID
	metaData.RootChainName = actConfig.RootChainDSL.RuleChain.Name
	actConfig.FlowContext.SetTraceId(metaData.TraceId)
	actConfig.FlowContext.SetFlowId(metaData.RootChainID)

	if actConfig.EndFunc == nil {
		actConfig.EndFunc = func(ctx context.Context, relationType string, param *paramx.FlowContext, err error) {
			if err != nil {
				log.Printf("工作流%s执行失败: %v", relationType, err)
				return
			}
			log.Printf("工作流%s执行成功: %v\n", relationType, param)
		}
	}

	// 全局配置
	config := rulego.NewConfig()

	// 如果有结束节点，则开启默认失败回调
	if hasEndNode(actConfig.RootChainDSL.Metadata.Nodes) {
		config.OnEndWithFailure = true
	}

	if len(actConfig.SubChainDSL) > 0 {
		var subErr error

		lo.ForEachWhile(actConfig.SubChainDSL, func(subChainDSL *types.RuleChainBaseInfo, index int) bool {
			subChainDSL.Root = false
			_, err := rulego.New(subChainDSL.ID, []byte(conv.String(subChainDSL)), rulego.WithConfig(config), types.WithAspects(&SubChainInjectAspect{}))
			if err != nil {
				subErr = err
				return false
			}
			return true
		})
		if subErr != nil {
			return subErr
		}
	}

	// 子链按 flow 节点引用的 targetId（project:chainID）注册；池里已有同名实例时按 DSL 是否变化热更新，
	// 否则子链修改后会一直跑旧版本（rulego.New 遇到同名 key 直接返回旧实例）。
	for _, sub := range actConfig.SubChains {
		if sub == nil {
			continue
		}
		sub.RuleChain.Root = false
		subKey := metaData.Project + ":" + sub.RuleChain.ID
		if _, err := engine.LoadOrReload(subKey, []byte(conv.String(sub)), rulego.WithConfig(config)); err != nil {
			return fmt.Errorf("load sub chain %s failed: %w", subKey, err)
		}
	}

	// 引擎加载策略：
	// - UseCache=true：优先复用全局 pool 中已存在的同名链（以 RootChainID 为 key）。
	//   命中后若最新 DSL 与已加载的一致则直接复用（性能最优、配置稳定，适合正式环境）；
	//   若不一致（节点配置已更新）则通过 ReloadSelf 在原实例上热更新（重新 Init 节点），
	//   既保证配置修改立即生效，又避免整链重建开销，且不改变 pool 中的实例身份。
	// - UseCache=false（默认）：每次都基于最新 DSL 通过 rulego.New 覆盖重建，确保配置更新立即生效
	//   （适合 node 测试/开发环境，避免全局 pool 缓存旧链导致修改不生效）。
	actConfig.RootChainDSL.RuleChain.Root = true

	if actConfig.RootChainDSL.Metadata.FirstNodeIndex == 0 {
		// 默认没有设置，则尝试找一下
		fileNodeIndex := engine.FirstNodeIndexByConnection(actConfig.RootChainDSL.Metadata.Connections, actConfig.RootChainDSL.Metadata.Nodes)
		if fileNodeIndex != -1 {
			actConfig.RootChainDSL.Metadata.FirstNodeIndex = fileNodeIndex
		}
	}

	var engineIns types.RuleEngine

	// 引擎池 key：优先用 PoolKey（发布/invoke 路径的确定性 UUID，隔离草稿实例），否则回退 RootChainID。
	poolKey := metaData.RootChainID
	if metaData.PoolKey != "" {
		poolKey = metaData.PoolKey
	}

	if actConfig.UseCache {
		if engineTemp, ok := rulego.Get(poolKey); ok {
			engineIns = engineTemp
		}
	} else {
		actConfig.RootChainDSL.RuleChain.DebugMode = true
	}

	if actConfig.RootChainDSL.RuleChain.DebugMode {
		config.OnDebug = commDebugFunction
	}

	// flow 节点补 targetId 等配置（只改序列化结果，不动调用方/缓存共享的 DSL 对象）
	rootChainDSL := engine.NormalizeFlowDSL([]byte(conv.String(actConfig.RootChainDSL)))

	if engineIns == nil {
		var err error
		engineIns, err = rulego.New(poolKey, rootChainDSL, rulego.WithConfig(config))
		if err != nil {
			return err
		}
	}

	if engineIns == nil {
		return fmt.Errorf("规则引擎不能为空")
	}

	// 配置未变化则直接复用旧实例；已变化时热更新该链（重新 Init 节点），使配置立即生效。
	if !actConfig.UseCache &&
		!cond.IsSameJson(string(engineIns.DSL()), string(rootChainDSL)) {
		if err := engineIns.ReloadSelf(rootChainDSL); err != nil {
			return fmt.Errorf("reload rule chain %s failed: %w", poolKey, err)
		}
	}

	if actConfig.MsgType == "" {
		actConfig.MsgType = "ACTIVITY_EVENT"
	}
	if metaData.RedisConfig != nil {
		engine.SetEnvRedisConfig(metaData.Project, metaData.Env, metaData.RedisConfig)
	}
	newMetadata := types.NewMetadata()
	metaMap := make(map[string]any)
	_ = conv.Unmarshal(metaData, &metaMap)
	delete(metaMap, metadataRedisConfigKey)
	for k, v := range metaMap {
		newMetadata.PutValue(k, conv.String(v))
	}

	msg := types.NewMsgWithJsonData(conv.String(actConfig.FlowContext))
	msg.SetType(actConfig.MsgType)
	msg.SetMetadata(newMetadata)

	flowStartTime := time.Now() // 流程发起时刻，用于计算整条流程总耗时（flow_total 汇总日志）

	// OnEnd 每个结束分支各回调一次（并行扇出、多条失败分支时会有多次），且可能并发。
	// 这里只加锁收集结果：任一分支失败即取该失败分支，否则取第一个成功分支；
	// 全部节点执行完后在 OnAllNodeCompleted 里只调用一次 EndFunc 和 FlowCompletedHook，
	// 避免调用方被多次回调（例如重复 close channel）以及 flow_total 汇总日志重复写入。
	var (
		endMu        sync.Mutex
		endGot       bool
		endCtx       context.Context
		endMsg       types.RuleMsg
		endErr       error
		endRelation  string
		endCallOnce  sync.Once
		endRecordOne = func(ruleCtx types.RuleContext, m types.RuleMsg, err error, relationType string) {
			endMu.Lock()
			defer endMu.Unlock()
			// 已记录失败分支后不再覆盖；已记录成功分支后只允许失败分支覆盖
			if endGot && (endErr != nil || err == nil) {
				return
			}
			endGot = true
			endCtx = ruleCtx.GetContext()
			endMsg = m
			endErr = err
			endRelation = relationType
		}
	)
	endOption := types.WithOnEnd(endRecordOne)
	endCompletedOption := types.WithOnAllNodeCompleted(func() {
		endCallOnce.Do(func() {
			endMu.Lock()
			gotCtx, gotMsg, gotErr, gotRelation := endCtx, endMsg, endErr, endRelation
			endMu.Unlock()
			if gotCtx == nil {
				gotCtx = ctx
			}
			var resultParam = new(paramx.FlowContext)
			if gotMsg.GetData() != "" {
				_ = conv.Unmarshal(gotMsg.GetData(), resultParam)
			}
			actConfig.EndFunc(gotCtx, gotRelation, resultParam, gotErr)
			if FlowCompletedHook != nil {
				FlowCompletedHook(metaData, time.Since(flowStartTime).Milliseconds(), gotErr)
			}
		})
	})

	if actConfig.IsAsync {
		engineIns.OnMsg(msg, endOption, endCompletedOption, types.WithContext(ctx))
	} else {
		engineIns.OnMsgAndWait(msg, endOption, endCompletedOption, types.WithContext(ctx))
	}
	return nil
}

func commDebugFunction(chainId string, flowType string, nodeId string, msg types.RuleMsg, relationType string, err error) {
	traceId := msg.GetMetadata().GetValue("trace_id")
	logData := logs.LogData{
		LogCommData: logs.LogCommData{
			LogId:      traceId,
			LogKey:     "",
			Env:        "",
			Method:     flowType,
			LogTime:    time.Now(),
			CreateTime: time.Now(),
			Extends:    nil,
		},
		Message: []any{msg},
	}

	code := 0
	if err != nil {
		code = 500
	}
	paramCtx := new(paramx.FlowContext)
	_ = conv.Unmarshal(msg.GetData(), paramCtx)
	nodeIdTag := paramx.StepId(nodeId)
	req := paramCtx.Arguments
	resp := paramCtx.Responses

	if one, ok := paramCtx.Steps[nodeIdTag]; ok {
		req = one.Arguments
		resp = one.Responses
	}

	engine.MysqlLogger.Info(logData, map[string]any{
		"chain_id":      chainId,
		"node_id":       nodeId,
		"relation_type": relationType,
		"error":         err,
		"code":          code,
		"request":       req,
		"response":      resp,
	}, msg.GetMetadata().Values(), msg.GetData())
}

func hasEndNode(nodeList []*types.RuleNode) bool {
	if len(nodeList) == 0 {
		return false
	}
	for _, n := range nodeList {
		if n != nil && n.Type == types.NodeTypeEnd {
			return true
		}
	}
	return false
}
