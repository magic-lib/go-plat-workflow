package rulegox

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/samber/lo"
	"log"
)

type ActivityFlowConfig struct {
	RootChainDSL *types.RuleChain           //根配置
	SubChainDSL  []*types.RuleChainBaseInfo //子配置
	FlowContext  *paramx.FlowContext        //前端传入的参数
	MsgType      string
	IsAsync      bool
	EndFunc      func(ctx context.Context, param *paramx.FlowContext, err error)
	// UseCache 是否复用全局 rulego 引擎池中已存在的同名链（以 RootChainID 为 key）。
	// true：命中则直接复用旧实例，性能更好、配置保持稳定（适合正式环境高频复用）；
	// false：每次都基于最新 DSL 通过 rulego.New 覆盖重建，配置更新可立即生效（适合 node 测试/开发环境）。
	// 默认 false（避免缓存导致配置修改不生效）。
	UseCache bool
}

type ActivityMetaData struct {
	RootChainID string        `json:"root_chain_id,omitempty"`
	TraceId     string        `json:"trace_id,omitempty"`
	Env         string        `json:"env,omitempty"`
	Project     string        `json:"project,omitempty"`
	RedisConfig *conn.Connect `json:"redis_config,omitempty"`
}

func StartActivityFlow(ctx context.Context, actConfig *ActivityFlowConfig, metaData *ActivityMetaData) error {
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
	actConfig.FlowContext.SetTraceId(metaData.TraceId)
	actConfig.FlowContext.SetFlowId(metaData.RootChainID)

	if actConfig.EndFunc == nil {
		actConfig.EndFunc = func(ctx context.Context, param *paramx.FlowContext, err error) {
			if err != nil {
				log.Printf("工作流执行失败: %v", err)
				return
			}
			log.Printf("工作流执行成功: %v\n", param)
		}
	}

	// 全局配置
	config := rulego.NewConfig()

	if len(actConfig.SubChainDSL) > 0 {
		var subErr error
		lo.ForEachWhile(actConfig.SubChainDSL, func(subChainDSL *types.RuleChainBaseInfo, index int) bool {
			subChainDSL.Root = false
			_, err := rulego.New(subChainDSL.ID, []byte(conv.String(subChainDSL)), rulego.WithConfig(config))
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

	// 引擎加载策略：
	// - UseCache=true：优先复用全局 pool 中已存在的同名链（以 RootChainID 为 key）。
	//   命中后若最新 DSL 与已加载的一致则直接复用（性能最优、配置稳定，适合正式环境）；
	//   若不一致（节点配置已更新）则通过 ReloadSelf 在原实例上热更新（重新 Init 节点），
	//   既保证配置修改立即生效，又避免整链重建开销，且不改变 pool 中的实例身份。
	// - UseCache=false（默认）：每次都基于最新 DSL 通过 rulego.New 覆盖重建，确保配置更新立即生效
	//   （适合 node 测试/开发环境，避免全局 pool 缓存旧链导致修改不生效）。
	actConfig.RootChainDSL.RuleChain.Root = true
	rootChainDSL := []byte(conv.String(actConfig.RootChainDSL))

	var engineIns types.RuleEngine

	if actConfig.UseCache {
		if engineTemp, ok := rulego.Get(metaData.RootChainID); ok {
			engineIns = engineTemp
		}
	}

	if engineIns == nil {
		var err error
		engineIns, err = rulego.New(metaData.RootChainID, rootChainDSL, rulego.WithConfig(config))
		if err != nil {
			return err
		}
	}

	if engineIns == nil {
		return fmt.Errorf("规则引擎不能为空")
	}

	// 配置未变化则直接复用旧实例；已变化时热更新该链（重新 Init 节点），使配置立即生效。
	if !actConfig.UseCache {
		if string(engineIns.DSL()) != string(rootChainDSL) {
			if err := engineIns.ReloadSelf(rootChainDSL); err != nil {
				return fmt.Errorf("reload rule chain %s failed: %w", metaData.RootChainID, err)
			}
		}
	}

	if actConfig.MsgType == "" {
		actConfig.MsgType = "ACTIVITY_EVENT"
	}
	newMetadata := types.NewMetadata()
	metaMap := make(map[string]any)
	_ = conv.Unmarshal(metaData, &metaMap)
	for k, v := range metaMap {
		newMetadata.PutValue(k, conv.String(v))
	}

	msg := types.NewMsg(0, actConfig.MsgType, types.JSON, newMetadata, conv.String(actConfig.FlowContext))
	endOption := types.WithOnEnd(func(ruleCtx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		var resultParam = new(paramx.FlowContext)
		_ = conv.Unmarshal(msg.GetData(), resultParam)
		if err != nil {
			actConfig.EndFunc(ruleCtx.GetContext(), resultParam, err)
			return
		}
		actConfig.EndFunc(ruleCtx.GetContext(), resultParam, nil)
	})
	if actConfig.IsAsync {
		engineIns.OnMsg(msg, endOption, types.WithContext(ctx))
	} else {
		engineIns.OnMsgAndWait(msg, endOption, types.WithContext(ctx))
	}
	return nil
}
