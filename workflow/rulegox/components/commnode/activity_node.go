package commnode

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/config"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"go.uber.org/multierr"
	"log"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/action"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
)

// ActivityGroup 一组按序执行的阶段（Stage），每个阶段内的 Activity 并行执行，阶段之间串行执行。
// 例如：{{a, b}, {c}, {d}, {e, f}} 表示 a,b 先并行执行，全部完成后执行 c，
// c 完成后执行 d，d 完成后 e,f 并行执行，全部完成后结束。
type ActivityGroup [][]*activity.Activity

type ActivityNode struct {
	Configuration *CommConfiguration     `json:"configuration"`
	activities    [][]*activity.Activity // 执行阶段列表
	nodeCondition string                 // 配置该node执行的条件，如果条件判断为true，则该node可以执行，否则不执行

	// mqExecutor 包级默认 MQ 执行器（通过 commnode.SetActivityMQExecutor 注入）。
	// 非空且执行环境（metaData.Env）非空时，单个 Activity 优先走 MQ 远程执行；
	// 否则回退到本地 newAct.Execute。每个节点实例共享同一包级执行器。
	mqExecutor ActivityMQExecutor
	ruleObj    *templates.RuleExprEngine
	// nodeLogCli node 运行日志专用 redis 客户端（首次使用时基于 actMetaData.RedisConfig 惰性建立并缓存）
	nodeLogCli *redis.Client
	// nodeName node 中文名（来自 DSL 中 ruleNode.Name，由管理端 builder 写入 NodeDef.Name），
	// 在 Init 时从 SelfDefinition 解析并缓存，用于上报 node 运行日志的 node_name 字段。
	nodeName string
}

type activityCfg struct {
	Activities    [][]*activity.Activity `json:"activities"`
	NodeCondition string                 `json:"node_condition"`
}

// cloneActivityList 深拷贝一组 Activity。
func cloneActivityList(actList []*activity.Activity) []*activity.Activity {
	if len(actList) == 0 {
		return nil
	}
	cloneList := make([]*activity.Activity, len(actList))
	for i, act := range actList {
		cloneList[i] = act.Clone()
	}
	return cloneList
}

// cloneStages 深拷贝整个阶段列表。
func cloneStages(activities [][]*activity.Activity) [][]*activity.Activity {
	if len(activities) == 0 {
		return nil
	}
	cloned := make([][]*activity.Activity, len(activities))
	for i, stage := range activities {
		cloned[i] = cloneActivityList(stage)
	}
	return cloned
}

// Type 返回组合类型标识，格式为阶段内 "|" 分隔，阶段间 "||" 分隔，
// 与注册时的 key 一致，在规则链 JSON 中直接使用该字符串作为 "type" 字段即可。
func (x *ActivityNode) Type() string {
	return common.ActivityNodeTypeName
}

// New 创建新实例，深拷贝阶段列表。
func (x *ActivityNode) New() types.Node {
	if len(x.activities) == 0 {
		return &ActivityNode{
			mqExecutor: defaultActivityMQExecutor,
		}
	}
	cfg := new(CommConfiguration)
	_ = conv.Unmarshal(x.Configuration, cfg)
	return &ActivityNode{
		Configuration: cfg,
		activities:    cloneStages(x.activities),
		nodeCondition: x.nodeCondition,
		ruleObj:       x.ruleObj,
		mqExecutor:    defaultActivityMQExecutor,
	}
}

// mergeActivityList 将配置中的 Activity 列表与原型模板合并（继承命名空间等关键字段）。
func mergeActivityList(templates, overrides []*activity.Activity) []*activity.Activity {
	templateByType := make(map[string]*activity.Activity, len(templates))
	for _, tpl := range templates {
		templateByType[tpl.Type()] = tpl
	}
	merged := make([]*activity.Activity, 0, len(overrides))
	for _, cfgAct := range overrides {
		tpl, ok := templateByType[cfgAct.Type()]
		if !ok {
			merged = append(merged, cfgAct)
			continue
		}
		cfgAct.ActivityType = tpl.ActivityType
		cfgAct.ActNamespace = tpl.ActNamespace
		cfgAct.ActName = tpl.ActName
		if cfgAct.ArgTemplate == "" {
			cfgAct.ArgTemplate = tpl.ArgTemplate
		}
		merged = append(merged, cfgAct)
	}
	return merged
}

// Init 初始化运行时实例。
// 若 chain JSON 的 node_config 中有 activities 字段，则以配置为准覆写对应阶段的
// Activity 列表（从原型模板继承 ActNamespace/ActName/ActivityType 等关键字段）。
func (x *ActivityNode) Init(_ types.Config, configuration types.Configuration) error {
	x.Configuration = new(CommConfiguration)
	if len(configuration) == 0 {
		return nil
	}
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	// 缓存 node 中文名（DSL 中 ruleNode.Name 由管理端 builder 写入 NodeDef.Name），
	// 供上报 node 运行日志时填充 node_name 字段。
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	if err := conv.Unmarshal(ruleNode.Configuration, x.Configuration); err != nil {
		return fmt.Errorf("activityNode error parsing CommConfiguration: %s, %v", conv.String(configuration), err)
	}

	cfgActs := new(activityCfg)
	_ = conv.Unmarshal(x.Configuration.NodeConfig, cfgActs)
	// 解析执行条件（node_config.node_condition）：进入前判断是否执行，不满足则跳过
	x.nodeCondition = cfgActs.NodeCondition
	if len(cfgActs.Activities) == 0 {
		return nil // 沿用注册时的原型阶段列表
	}

	for i, cfgStage := range cfgActs.Activities {
		if i < len(x.activities) {
			// 与原型中对应阶段合并
			x.activities[i] = mergeActivityList(x.activities[i], cfgStage)
		} else {
			// 配置中额外新增的阶段
			x.activities = append(x.activities, cloneActivityList(cfgStage))
		}
	}

	x.ruleObj = templates.NewRuleExprEngine()
	return nil
}

// OnMsg 按阶段顺序执行所有 Activity。
// 每个阶段内若只有一个 Activity 则串行执行，多个 Activity 则并发执行。
// 前序阶段的输出会累积到 allParam 中供后续阶段引用。
func (x *ActivityNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	if len(x.activities) == 0 {
		ctx.TellFailure(msg, fmt.Errorf("activityNode has no activities"))
		return
	}

	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic:%v", r)
			ctx.TellFailure(msg, err)
		}
	}()

	// 解析消息中的全局参数
	allParamStr := msg.GetData()
	allParam := new(paramx.FlowContext)
	if err := conv.Unmarshal(allParamStr, allParam); err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	stepFlowCtx, err := x.getNodeFlowContext(ctx, allParam)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	// 执行条件（node_config.node_condition）判断：配置了则在进入前先求值，
	// 不满足则跳过该节点（不执行活动），但流程继续向下传递。
	if x.nodeCondition != "" {
		condParams, _ := allParam.ToMaps()
		for k, v := range stepFlowCtx.Arguments {
			condParams[k] = v
		}
		condRes, cErr := x.ruleObj.RunString(x.nodeCondition, condParams)
		if cErr != nil {
			ctx.TellFailure(msg, fmt.Errorf("node_condition 表达式执行错误: %v", cErr))
			return
		}
		ok, bErr := conv.Convert[bool](condRes)
		if bErr != nil {
			ctx.TellFailure(msg, fmt.Errorf("node_condition 表达式结果不是布尔值: %v", condRes))
			return
		}
		if !ok {
			// 条件不满足：跳过节点执行，直接放行消息，不影响下游流程
			ctx.TellSuccess(msg)
			return
		}
	}

	metaDataAny := msg.GetMetadata()
	metaDataMap := make(map[string]any)
	metaDataAny.ForEach(func(key string, value string) bool {
		metaDataMap[key] = value
		return true
	})
	actMetaData := new(rulegox.ActivityMetaData)
	_ = conv.Unmarshal(metaDataMap, actMetaData)

	//log.Printf("[activityNode] OnMsg nodeId=%s metaData=%s", x.getNodeId(ctx), conv.String(actMetaData))

	currNodeId := getNodeId(ctx)
	nodeStr := string(currNodeId)
	// 上报 node 入参日志（落库 wf_node_logs，便于前端查看运行情况）
	//x.pushNodeLog(actMetaData, nodeStr, nodeStr, "request", "info", allParam, stepFlowCtx.Arguments, nil)
	startTime := time.Now().UnixMilli()
	nodeSpanId := id.GetUUID(nodeStr)
	err = x.execNode(ctx, nodeSpanId, actMetaData, stepFlowCtx)
	durationMs := time.Now().UnixMilli() - startTime

	nodeStep := &paramx.Step{
		Arguments:   stepFlowCtx.Arguments,
		Responses:   nil,
		Status:      paramx.StepStatusPending,
		Error:       nil,
		StartTimeMs: stepFlowCtx.Meta.StartTimeMs,
		EndTimeMs:   time.Now().UnixMilli(),
	}

	if err != nil {
		nodeStep.Status = paramx.StepStatusFail
		nodeStep.Error = &paramx.ErrorInfo{
			Code:    "500",
			Message: err.Error(),
			Stack:   err.Error(),
		}
		allParam.SetStep(currNodeId, nodeStep)
		msg.SetData(conv.String(allParam))
		log.Printf("[activityNode] node=%s 执行失败 error=%s", currNodeId, err.Error())
		// 上报 node 失败日志（含入参），error_msg 填充失败原因，payload 为全部入参
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, x.nodeName, "fail", "error", types.Failure, allParam, stepFlowCtx.Arguments, stepFlowCtx.Responses, err)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellFailure(msg, err)
		return
	}

	allDataMap, err := stepFlowCtx.ToMaps()
	if err == nil {
		dataMap := x.getActivityParam(allDataMap, x.Configuration.Responses)
		if len(dataMap) == 0 {
			// 如果没有定义，就将所有activity的返回值进行合并输出
			newDataMap := make(map[string]any)
			for _, oneStep := range stepFlowCtx.Steps {
				if cond.IsJsonMap(conv.String(oneStep.Responses)) {
					newDataMap2 := make(map[string]any)
					_ = conv.Unmarshal(conv.String(oneStep.Responses), &newDataMap2)
					for k, v := range newDataMap2 {
						newDataMap[k] = v
					}
				}
			}
			nodeStep.Responses = newDataMap
		} else {
			nodeStep.Responses = dataMap
		}

		nodeStep.Status = paramx.StepStatusSuccess
		allParam.SetStep(currNodeId, nodeStep)
		msg.SetData(conv.String(allParam))
		// 上报 node 返回值日志（落库 wf_node_logs）
		nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, x.nodeName, "success", "info", types.Success, allParam, stepFlowCtx.Arguments, dataMap, nil)
		if cliErr == nil && x.nodeLogCli == nil {
			x.nodeLogCli = nodeCli
		}
		ctx.TellSuccess(msg)
		return
	}

	dataMap := x.getActivityParam(allDataMap, x.Configuration.Responses)
	nodeStep.Responses = dataMap
	nodeStep.Status = paramx.StepStatusSuccess
	allParam.SetStep(currNodeId, nodeStep)
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, durationMs, nodeStr, x.nodeName, "response", "error", types.Success, allParam, stepFlowCtx.Arguments, dataMap, err)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellSuccess(msg)
}

type NodeLogDef struct {
	ID         uint   `json:"id"`
	Project    string `json:"project"`
	Env        string `json:"env"`
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name"`
	EventID    string `json:"event_id"`
	Level      string `json:"level"`
	Timestamp  int64  `json:"timestamp"`
	DurationMs int64  `json:"duration_ms"`
	// Payload node 执行的全部入参（全局参数 + 本节点参数），JSON 字符串
	Payload json.RawMessage `json:"payload"`
	// Arguments node 执行的输入参数（取自 payload.arguments），JSON 字符串，便于按 node 直接查看入参
	Arguments json.RawMessage `json:"arguments"`
	// Result node 执行后的返回值（按本节点 responses 配置提取），JSON 字符串
	Result string `json:"result"`
	// ErrorMsg 执行错误信息（成功为空）
	ErrorMsg string `json:"error_msg"`
	// Error 兼容 worker/组件上报时使用的 "error" 字段名
	Error string `json:"error"`
	// TraceID 本次执行的分布式追踪 ID，用于回查本次执行产生的 activity 日志（wf_activity_logs.trace_id）
	TraceID     string `json:"trace_id"`
	RootChainID string `json:"root_chain_id"`
	SpanID      string `json:"span_id"`
	// RelationType 该 node 执行完成后往下传递的连接类型（relationType），
	// 对应 rulego 的 TellSuccess/TellFailure/TellNext 等，取值如 Success/Failure/True/False 或自定义字符串。
	// 用于回查本次 node 走了哪条分支链路。
	RelationType string    `json:"relation_type"`
	CreatedAt    time.Time `json:"created_at"`
}

// 由管理端收集器消费后落库 wf_node_logs，便于在前端查看每个 node 的运行情况。
// 若已注入 NodeLogSaver，则直接将 NodeLogDef 写入数据库（不经过 redis 中转）；
// 否则回退为 redis 推送（基于 actMetaData.RedisConfig 惰性建连并缓存），保持兼容。
func pushNodeLog(nodeLogCli *redis.Client, metaData *rulegox.ActivityMetaData, nodeSpanId string, durationMs int64, nodeID, nodeName, eventID, level, relationType string, payload any, arguments map[string]any, result any, runErr error) (*redis.Client, error) {
	if metaData == nil {
		return nodeLogCli, nil
	}
	now := time.Now()
	rec := NodeLogDef{
		Project:      metaData.Project,
		Env:          metaData.Env,
		NodeID:       nodeID,
		NodeName:     nodeName,
		EventID:      eventID,
		DurationMs:   durationMs,
		Level:        level,
		Timestamp:    now.Unix(),
		Payload:      json.RawMessage(conv.String(payload)),
		Arguments:    json.RawMessage(conv.String(arguments)),
		Result:       conv.String(result),
		TraceID:      metaData.TraceId,
		RootChainID:  metaData.RootChainID,
		SpanID:       nodeSpanId,
		RelationType: relationType,
		CreatedAt:    now,
	}
	if runErr != nil {
		rec.ErrorMsg = runErr.Error()
	}

	// 已注入落库实现：直接写入数据库
	if defaultNodeLogSaver != nil {
		goroutines.GoAsync(func(param ...any) {
			if err := defaultNodeLogSaver.CreateNodeLog(context.Background(), &rec); err != nil {
				log.Printf("[activityNode] pushNodeLog save error=%s", err.Error())
			}
		})
		return nodeLogCli, nil
	}

	// 兜底：未注入落库实现时走 redis 推送（原行为）
	if metaData.RedisConfig == nil {
		return nodeLogCli, nil
	}
	if nodeLogCli == nil {
		cli, err := rulegox.NewRedisClient(metaData.RedisConfig)
		if err != nil {
			return nodeLogCli, err
		}
		nodeLogCli = cli
	}
	key := rulegox.NodeLogKeyPrefix + rulegox.GetMQNamespace(metaData.Project, metaData.Env)
	logDataStr := conv.String(rec)
	goroutines.GoAsync(func(param ...any) {
		err := nodeLogCli.RPush(context.Background(), key, logDataStr).Err()
		if err != nil {
			log.Printf("[activityNode] pushNodeLog redis.RPush error=%s", err.Error())
		}
		// 限制单个 node 日志 list 长度，避免 redis 中无限增长
		err = nodeLogCli.LTrim(context.Background(), key, -500, -1).Err()
		if err != nil {
			log.Printf("[activityNode] pushNodeLog redis.LTrim error=%s", err.Error())
		}
	})
	return nodeLogCli, nil
}

func (x *ActivityNode) getNodeFlowContext(ctx types.RuleContext, allParam *paramx.FlowContext) (*paramx.FlowContext, error) {
	currNodeId := getNodeId(ctx)
	if currNodeId == "" {
		return nil, fmt.Errorf("activityNode currNodeId is empty")
	}

	// 合并 ArgMapping 和 BindArgs 到 allParam 中
	nodeParams, err := NodeParams(allParam, currNodeId, x.Configuration.ArgTemplate, x.Configuration.Arguments)
	if err != nil {
		return nil, err
	}
	allParam.SetStepArguments(currNodeId, nodeParams) // 设置当前节点参数

	stepFlowCtx := paramx.NewFlowContext(string(currNodeId), id.NewUUID(), nodeParams)
	stepFlowCtx.SetTraceId(allParam.Meta.TraceId)
	stepFlowCtx.Meta.StartTimeMs = time.Now().UnixMilli()

	return stepFlowCtx, nil
}

func (x *ActivityNode) execNode(ctx types.RuleContext, nodeSpanId string, actMetaData *rulegox.ActivityMetaData, stepFlowCtx *paramx.FlowContext) error {

	// ——— 按阶段依次执行 ———
	for _, stage := range x.activities {
		if len(stage) == 0 {
			continue
		}
		if len(stage) == 1 {
			// 单活动阶段：串行执行
			if err := x.execOneActivity(ctx, nodeSpanId, actMetaData, stage[0], stepFlowCtx); err != nil {
				return err
			}
		} else {
			// 多活动阶段：并发执行，等待全部完成后进入下一阶段
			if err := x.execParallelStage(ctx, nodeSpanId, actMetaData, stage, stepFlowCtx); err != nil {
				return err
			}
		}
	}

	return nil
}

// execParallelStage 并发执行一个阶段内的所有 Activity，等待全部完成后合并结果到 allParam。
func (x *ActivityNode) execParallelStage(ctx types.RuleContext, nodeSpanId string, metaData *rulegox.ActivityMetaData, stage []*activity.Activity, stepFlowCtx *paramx.FlowContext) error {

	if len(stage) == 0 {
		return nil
	}

	var retErr error

	_, _ = goroutines.AsyncExecuteDataList[*activity.Activity](30*time.Second, stage, func(value *activity.Activity, key int) (breakFlag bool, err error) {
		if err := x.execOneActivity(ctx, nodeSpanId, metaData, value, stepFlowCtx); err != nil {
			// 执行失败，别的并行的程序不能退出，继续执行完成
			retErr = multierr.Append(retErr, err)
			return false, err
		}
		return false, nil
	})

	return retErr
}

// execOneActivity 执行单个 Activity，结果回写 allParam。
// 执行策略：
//   - 若已注入 mqExecutor 且执行环境（metaData.Env）非空，则通过 MQ 同步调用分布式
//     worker 远程执行该 Activity（复用 workflow.MQExecutor.RequestActivity），
//     适用于生产环境依赖远程监听程序的 Activity。
//   - 否则（未注入执行器或环境为空）回退到本地 newAct.Execute，保证单测/无 MQ 场景可用。
func (x *ActivityNode) execOneActivity(ctx types.RuleContext, nodeSpanId string, metaData *rulegox.ActivityMetaData, act *activity.Activity, stepParamCtx *paramx.FlowContext) error {

	if metaData == nil || metaData.RedisConfig == nil {
		return fmt.Errorf("activityNode execOneActivity: RedisConfig is nil")
	}

	oneWorker, err := GetMQWorker(metaData.Project, metaData.Env, metaData.RedisConfig, func(project, env string, redisCfg *conn.Connect) (*rulegox.MQWorker, error) {
		return rulegox.NewMQWorker(project, env, redisCfg)
	})
	if err != nil {
		return err
	}

	newAct := act.Clone()
	if newAct.Id == "" {
		return fmt.Errorf("activityNode execOneActivity: Activity Id is empty")
	}
	stepId := paramx.StepId(newAct.Id)

	allDataMap, err := stepParamCtx.ToMaps()
	if err != nil {
		return err
	}
	// 按类型对 activity 参数绑定做运行时计算（float64 类型转换 / formula 公式求值）。
	// string/int64 交由下游 param.MergeArgumentsByBinding 按类型处理，此处保持不变。
	dataMap := x.getActivityParam(allDataMap, newAct.Arguments)
	stepParamCtx.SetStepArguments(stepId, dataMap)

	// 优先走 MQ 远程执行（生产环境依赖远程 worker 的 Activity）。
	if oneWorker != nil && metaData.Env != "" {
		metaDataTemp := &rulegox.MetaDataHeader{
			RootChainID: metaData.RootChainID,
			TraceID:     metaData.TraceId,
			SpanID:      newAct.Id,
			NodeSpanID:  nodeSpanId,
		}
		// 构造 returnBindConfig：从 activity 模板定义（按 ActNamespace+ActName 唯一确定）的
		// return_values 解析得到，用于 RequestActivity 从 worker 返回值中按配置提取/重命名。
		// 节点编排里的 activity.Activity.Id 不能稳定对应 ActivityDef.ID，故使用 namespace+name 反查。
		// 若 store 未注入或无匹配模板，则回退为 nil（与历史行为一致）。
		var returnValues = make([]*config.ReturnValue, 0)
		if newAct.ActNamespace != "" && newAct.ActName != "" &&
			defaultActivityStore != nil && metaData.Project != "" {
			if actDef, fErr := defaultActivityStore.GetByNamespaceName(ctx.GetContext(), metaData.Project, newAct.ActNamespace, newAct.ActName); fErr == nil && len(actDef.ReturnValues) > 0 {
				_ = conv.Unmarshal(string(actDef.ReturnValues), &returnValues)
			}
		}

		resp, err := oneWorker.RequestActivity(ctx.GetContext(), newAct, dataMap, metaDataTemp.ToHeader(nil), returnValues)
		if err != nil {
			return err
		}
		stepParamCtx.SetStepResponse(stepId, resp.Data)
		return nil
	}

	// 回退：本地执行（无 MQ 或环境为空时）。
	respData, err := newAct.Execute(ctx.GetContext(), dataMap)
	if err != nil {
		return err
	}
	stepParamCtx.SetStepResponse(stepId, respData)
	return nil
}

func (x *ActivityNode) getActivityParam(allParam map[string]any, bindConfig []*param.BindConfig) map[string]any {
	return GetActivityParam(x.ruleObj, allParam, bindConfig)
}

func GetActivityParam(ruleEngine *templates.RuleExprEngine, allParam map[string]any, bindConfig []*param.BindConfig) map[string]any {
	actParam := make(map[string]any)
	for _, item := range bindConfig {
		exp := conv.String(item.Value)
		if item.Type == "formula" {
			expAny, err := ruleEngine.RunString(exp, allParam)
			if err == nil {
				log.Printf("activityNode getActivityParam: RunString err: %v", err)
				actParam[item.Key] = expAny
				continue
			}
		}
		ruleObj := templates.NewTemplate(exp, templates.DefaultPrefix, templates.DefaultSuffix)
		tempVal := ruleObj.Replace(allParam)
		actParam[item.Key], _ = conv.ConvertForTypeString(item.Type, tempVal)
	}
	return actParam
}

// getNodeId 获取当前节点的 ID（优先用 ctx 中的 SelfId）
func getNodeId(ctx types.RuleContext) paramx.StepId {
	if idTemp := ctx.GetSelfId(); idTemp != "" {
		return paramx.StepId(idTemp)
	}
	return ""
}

// setActionMeta 将 Action 的元数据写入 msg.Metadata
func (x *ActivityNode) setActionMeta(msg types.RuleMsg, act *activity.Activity) {
	if act == nil {
		return
	}
	actionFun, err := action.GetAction(act.ActNamespace, act.ActName)
	if err != nil {
		return
	}
	actMeta := actionFun.MetaData()
	var meta map[string]any
	if err := conv.Unmarshal(actMeta, &meta); err != nil {
		return
	}
	for k, v := range meta {
		msg.Metadata.PutValue(k, conv.String(v))
	}
}

// Desc returns the component description
func (x *ActivityNode) Desc() string {
	return "ActivityNode: " + x.Type() + " to Failure/Success."
}

// Destroy 清理资源
// Destroy cleans up resources.
func (x *ActivityNode) Destroy() {
}

func init() {
	Registry.Add(&ActivityNode{})
	_ = rulego.Registry.Register(&ActivityNode{})
}
