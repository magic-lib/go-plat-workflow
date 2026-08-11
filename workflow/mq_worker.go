package workflow

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/magic-lib/go-plat-utils/utils/httputil"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"strings"
)

// WfWorker 工作流 worker
type WfWorker struct {
	MQWorker *rulegox.MQWorker
}

// NewWfWorker 创建 MQ worker，内部构建 asynq 消费端
func NewWfWorker(projectName, env string, redisCfg *conn.Connect) (*WfWorker, error) {
	mqWorker, err := rulegox.NewMQWorker(projectName, env, redisCfg)
	if err != nil {
		return nil, err
	}
	return &WfWorker{
		MQWorker: mqWorker,
	}, nil
}
func NewWfWorkerWithMQWorker(mqWorker *rulegox.MQWorker) (*WfWorker, error) {
	if mqWorker == nil {
		return nil, fmt.Errorf("mq worker is nil")
	}
	return &WfWorker{
		MQWorker: mqWorker,
	}, nil
}

// RequestActivity 订阅指定 topic 并注册处理函数。
func (w *WfWorker) RequestActivity(ctx context.Context, actDef *ActivityDef, params any) (*httputil.CommResponse, error) {
	if w.MQWorker == nil {
		return nil, fmt.Errorf("mq worker is nil")
	}
	retString := ""
	if len(actDef.Responses) > 0 {
		retString = string(actDef.Responses)
		if strings.ToLower(retString) == "null" {
			retString = ""
		}
	}
	return w.MQWorker.RequestActivity(ctx, actDef.ActNamespace, actDef.ActName, actDef.ArgTemplate, retString, params)
}
func (w *WfWorker) SubscribeActivity(actNamespace, actName string, handler utils.ContextAnyHandler) error {
	if w.MQWorker == nil {
		return fmt.Errorf("mq worker is nil")
	}
	return w.MQWorker.SubscribeActivity(actNamespace, actName, handler)
}
