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

	var engineIns types.RuleEngine

	if engineTemp, ok := rulego.Get(metaData.RootChainID); ok {
		engineIns = engineTemp
	}

	if engineIns == nil {
		actConfig.RootChainDSL.RuleChain.Root = true
		var err error
		rootChainDSL := []byte(conv.String(actConfig.RootChainDSL))
		engineIns, err = rulego.New(metaData.RootChainID, rootChainDSL, rulego.WithConfig(config))
		if err != nil {
			return err
		}
	}

	if engineIns == nil {
		return fmt.Errorf("规则引擎不能为空")
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
