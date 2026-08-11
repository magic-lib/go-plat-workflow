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
	"log"
)

type ActivityFlowConfig struct {
	RootChainDSL map[string][]byte
	SubChainDSL  map[string][]byte
	Variables    map[string]any
	MsgType      string
	IsAsync      bool
	EndFunc      func(ctx context.Context, param *paramx.ParamCtx, err error)
}

// RedisConfig 不同环境下的 Redis 连接配置。
type RedisConfig struct {
	// Addr Redis 地址，如 127.0.0.1:6379
	Addr string `json:"addr"`
	// Password 密码（可选）
	Password string `json:"password,omitempty"`
	// DB 数据库序号
	DB int `json:"db"`
	// Username 用户名（Redis 6+ ACL，可选）
	Username string `json:"username,omitempty"`
}

type ActivityMetaData struct {
	RootChainID string        `json:"root_chain_id,omitempty"`
	TraceId     string        `json:"trace_id,omitempty"`
	Env         string        `json:"env,omitempty"`
	Project     string        `json:"project,omitempty"`
	RedisConfig *conn.Connect `json:"redis_config,omitempty"`
}

func StartActivityFlow(actConfig *ActivityFlowConfig, metaData *ActivityMetaData) error {
	if actConfig == nil {
		return fmt.Errorf("参数不能为空")
	}
	if metaData == nil {
		return fmt.Errorf("元信息不能为空")
	}
	if metaData.Env == "" {
		return fmt.Errorf("环境不能为空")
	}
	metaData.TraceId = id.GetUUID(metaData.TraceId)

	if actConfig.EndFunc == nil {
		actConfig.EndFunc = func(ctx context.Context, param *paramx.ParamCtx, err error) {
			if err != nil {
				log.Printf("工作流执行失败: %v", err)
				return
			}
			log.Printf("工作流执行成功: %v\n", param)
		}
	}

	if len(actConfig.RootChainDSL) != 1 {
		return fmt.Errorf("主规则链DSL目前只能为1个")
	}

	// 全局配置
	config := rulego.NewConfig()
	if len(actConfig.SubChainDSL) > 0 {
		for sudChainId, subChainDSL := range actConfig.SubChainDSL {
			_, err := rulego.New(sudChainId, subChainDSL, rulego.WithConfig(config))
			if err != nil {
				return err
			}
		}
	}

	var engine types.RuleEngine
	for rootChainId, rootChainDSL := range actConfig.RootChainDSL {
		var err error
		engine, err = rulego.New(rootChainId, rootChainDSL, rulego.WithConfig(config))
		if err != nil {
			return err
		}
	}
	if engine == nil {
		return fmt.Errorf("规则引擎不能为空")
	}

	paramInput := paramx.NewParamCtxWithVars(actConfig.Variables)

	if actConfig.MsgType == "" {
		actConfig.MsgType = "ACTIVITY_EVENT"
	}
	newMetadata := types.NewMetadata()
	metaMap := make(map[string]any)
	_ = conv.Unmarshal(metaData, &metaMap)
	for k, v := range metaMap {
		newMetadata.PutValue(k, conv.String(v))
	}

	msg := types.NewMsg(0, actConfig.MsgType, types.JSON, newMetadata, conv.String(paramInput))
	endOption := types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		var resultParam = new(paramx.ParamCtx)
		_ = conv.Unmarshal(msg.GetData(), resultParam)
		if err != nil {
			actConfig.EndFunc(ctx.GetContext(), resultParam, err)
			return
		}
		actConfig.EndFunc(ctx.GetContext(), resultParam, nil)
	})
	if actConfig.IsAsync {
		engine.OnMsg(msg, endOption)
	} else {
		engine.OnMsgAndWait(msg, endOption)
	}
	return nil
}
