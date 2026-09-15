package workflow

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-startupcfg/startupcfg"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/samber/lo"
	"log"
)

// WfConfig 工作流 worker
type WfConfig struct {
	ConfigKey   string `json:"config_key"`
	ProjectKey  string `json:"project_key"`
	EnvKey      string `json:"env_key"`
	ApiTokenKey string `json:"api_token_key"`
}

var defaultWorkflowLogic = &WfConfig{
	ConfigKey:   "workflow-server",
	ProjectKey:  "project",
	EnvKey:      "env",
	ApiTokenKey: "api-token",
}

type WfLogic struct {
	DomainName string `json:"domain_name"`
	Project    string `json:"project"`
	Env        string `json:"env"`
	ApiToken   string `json:"api_token"`
}

type RegActivityInfo struct {
	Namespace       string                  `json:"namespace"`
	ActivityName    string                  `json:"activity_name"`
	ActivityHandler utils.ContextAnyHandler `json:"activity_handler"`
}

func NewWorkflowLogic(svcCfg *startupcfg.ConfigAPI, wf *WfConfig) (*WfLogic, error) {
	if svcCfg == nil {
		return nil, fmt.Errorf("svcCfg is nil")
	}

	if wf == nil {
		wf = defaultWorkflowLogic
	} else {
		if wf.ConfigKey == "" {
			wf.ConfigKey = defaultWorkflowLogic.ConfigKey
		}
		if wf.ProjectKey == "" {
			wf.ProjectKey = defaultWorkflowLogic.ProjectKey
		}
		if wf.EnvKey == "" {
			wf.EnvKey = defaultWorkflowLogic.EnvKey
		}
		if wf.ApiTokenKey == "" {
			wf.ApiTokenKey = defaultWorkflowLogic.ApiTokenKey
		}
	}

	svcApi := svcCfg.ServiceAPI(wf.ConfigKey)
	if svcApi == nil {
		return nil, fmt.Errorf("ConfigKey not config: %s", wf.ConfigKey)
	}

	projectName, _ := svcApi.ConfigData(wf.ProjectKey)
	env, _ := svcApi.ConfigData(wf.EnvKey)
	apiToken, _ := svcApi.AuthData(wf.ApiTokenKey)
	projectNameStr := conv.String(projectName)
	envNameStr := conv.String(env)

	if projectNameStr == "" || envNameStr == "" || apiToken == "" {
		return nil, fmt.Errorf("project or env or apiToken is empty: config_key:%s project:%s, env:%s, apiToken:%s", wf.ConfigKey, projectNameStr, envNameStr, apiToken)
	}
	if svcApi.DomainName() == "" {
		return nil, fmt.Errorf("domainName is empty,config_key:%s", wf.ConfigKey)
	}

	return &WfLogic{
		DomainName: svcApi.DomainName(),
		Project:    projectNameStr,
		Env:        envNameStr,
		ApiToken:   apiToken,
	}, nil
}

func (l *WfLogic) getWfWorker(ctx context.Context) (*WfWorker, error) {
	return NewWfWorkerFromRedisConfigAPI(ctx, l.Project, l.Env, l.DomainName, l.ApiToken)
}

// InvokeWorkerFlowAPI 调用 workflow 活动 API
func (l *WfLogic) InvokeWorkerFlowAPI(ctx context.Context, chainKey string, traceId string, payload map[string]any, isAsync bool) (any, error) {
	data, err := InvokeWorkerFlowAPI(ctx, l.Project, l.Env, l.DomainName, l.ApiToken, &InvokeRequest{
		ChainKey: chainKey,
		Payload:  payload,
		Metadata: InvokeMetadata{
			TraceID: traceId,
			IsAsync: isAsync,
		},
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (l *WfLogic) RegisterActivities(ctx context.Context, allActivities []*RegActivityInfo) error {
	w, err := l.getWfWorker(ctx)
	if err != nil {
		log.Println("RegisterActivities err:", err)
		return err
	}
	if w == nil {
		log.Println("RegisterActivities err: w is nil")
		return fmt.Errorf("w is nil")
	}

	var regErrs []error
	lo.ForEach(allActivities, func(method *RegActivityInfo, _ int) {
		if e := w.SubscribeActivity(method.Namespace, method.ActivityName, method.ActivityHandler); e != nil {
			log.Println("RegisterActivities failed namespace:", method.Namespace, " name:", method.ActivityName, " error:", e)
			regErrs = append(regErrs, e)
		}
	})
	// 所有 activity 注册完成后，再启动消费端 server。
	// 现在每个 activity 使用独立的 asynq 队列（namespace:activity/...），
	// 必须等全部注册完、队列集合确定后才能启动，否则会漏消费某些队列。
	if startErr := w.Start(); startErr != nil {
		log.Println("RegisterActivities start server error:", startErr)
		regErrs = append(regErrs, startErr)
	}
	if len(regErrs) > 0 {
		return fmt.Errorf("some activities failed to register (%d/%d): %v", len(regErrs), len(allActivities), regErrs)
	}
	return nil
}
