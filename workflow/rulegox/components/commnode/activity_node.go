package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"go.uber.org/multierr"
	"log"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/action"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
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

	// mqExecutor 包级默认 MQ 执行器（通过 commnode.SetActivityMQExecutor 注入）。
	// 非空且执行环境（metaData.Env）非空时，单个 Activity 优先走 MQ 远程执行；
	// 否则回退到本地 newAct.Execute。每个节点实例共享同一包级执行器。
	mqExecutor ActivityMQExecutor
	ruleObj    *templates.RuleExprEngine
}

type activityCfg struct {
	Activities [][]*activity.Activity `json:"activities"`
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

// flattenStages 将阶段列表展平为一维 Activity 切片。
func flattenStages(activities [][]*activity.Activity) []*activity.Activity {
	var result []*activity.Activity
	for _, stage := range activities {
		result = append(result, stage...)
	}
	return result
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
	if err := conv.Unmarshal(ruleNode.Configuration, x.Configuration); err != nil {
		return fmt.Errorf("activityNode error parsing CommConfiguration: %s, %v", conv.String(configuration), err)
	}

	cfgActs := new(activityCfg)
	_ = conv.Unmarshal(x.Configuration.NodeConfig, cfgActs)
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

	metaDataAny := msg.GetMetadata()
	metaDataMap := make(map[string]any)
	metaDataAny.ForEach(func(key string, value string) bool {
		metaDataMap[key] = value
		return true
	})
	actMetaData := new(rulegox.ActivityMetaData)
	_ = conv.Unmarshal(metaDataMap, actMetaData)

	log.Print("activityNode OnMsg:", conv.String(actMetaData))

	err = x.execNode(ctx, actMetaData, stepFlowCtx)

	currNodeId := x.getNodeId(ctx)

	nodeStep := &paramx.Step{
		Arguments:   stepFlowCtx.Arguments,
		Responses:   nil,
		Status:      "",
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
		ctx.TellFailure(msg, err)
		return
	}

	allDataMap, err := stepFlowCtx.ToMaps()
	if err == nil {
		dataMap := x.getActivityParam(allDataMap, x.Configuration.Responses)
		nodeStep.Responses = dataMap
		nodeStep.Status = paramx.StepStatusSuccess
		allParam.SetStep(currNodeId, nodeStep)
		msg.SetData(conv.String(allParam))
		ctx.TellSuccess(msg)
		return
	}

	msg.SetData(conv.String(allParam))
	ctx.TellSuccess(msg)
}

func (x *ActivityNode) getNodeFlowContext(ctx types.RuleContext, allParam *paramx.FlowContext) (*paramx.FlowContext, error) {
	currNodeId := x.getNodeId(ctx)
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

func (x *ActivityNode) execNode(ctx types.RuleContext, actMetaData *rulegox.ActivityMetaData, stepFlowCtx *paramx.FlowContext) error {

	// ——— 按阶段依次执行 ———
	for stageIdx, stage := range x.activities {
		if len(stage) == 0 {
			continue
		}
		if len(stage) == 1 {
			// 单活动阶段：串行执行
			if err := x.execOneActivity(ctx, actMetaData, stage[0], stepFlowCtx); err != nil {
				return err
			}
		} else {
			// 多活动阶段：并发执行，等待全部完成后进入下一阶段
			if err := x.execParallelStage(ctx, actMetaData, stageIdx, stage, stepFlowCtx); err != nil {
				return err
			}
		}
	}

	return nil
}

// execParallelStage 并发执行一个阶段内的所有 Activity，等待全部完成后合并结果到 allParam。
func (x *ActivityNode) execParallelStage(ctx types.RuleContext, metaData *rulegox.ActivityMetaData, stageIdx int,
	stage []*activity.Activity, stepFlowCtx *paramx.FlowContext) error {

	if len(stage) == 0 {
		return nil
	}

	var retErr error

	_, _ = goroutines.AsyncExecuteDataList[*activity.Activity](30*time.Second, stage, func(value *activity.Activity, key int) (breakFlag bool, err error) {
		if err := x.execOneActivity(ctx, metaData, value, stepFlowCtx); err != nil {
			// 执行失败，别的并行的程序不能退出，继续执行完成
			retErr = multierr.Append(retErr, err)
			return false, err
		}
		return false, nil
	})

	return retErr
}

// deepCopyParamCtx 通过序列化再反序列化实现 ParamCtx 深拷贝。
func deepCopyFlowCtx(src *paramx.FlowContext) *paramx.FlowContext {
	dst := &paramx.FlowContext{}
	_ = conv.Unmarshal(conv.String(src), dst)
	return dst
}
func deepCopyStep(src *paramx.Step) *paramx.Step {
	dst := &paramx.Step{}
	_ = conv.Unmarshal(conv.String(src), dst)
	return dst
}

// toMapValue 将 Activity 返回的 arguments（可能为 map 或结构体）安全转为 map[string]any。
// 非 map 类型（如标量）回退为空 map，避免 SetStepArguments 接收非法类型。
func toMapValue(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	m := make(map[string]any)
	if err := conv.Unmarshal(conv.String(v), &m); err == nil {
		return m
	}
	return map[string]any{}
}

// execOneActivity 执行单个 Activity，结果回写 allParam。
// 执行策略：
//   - 若已注入 mqExecutor 且执行环境（metaData.Env）非空，则通过 MQ 同步调用分布式
//     worker 远程执行该 Activity（复用 workflow.MQExecutor.RequestActivity），
//     适用于生产环境依赖远程监听程序的 Activity。
//   - 否则（未注入执行器或环境为空）回退到本地 newAct.Execute，保证单测/无 MQ 场景可用。
func (x *ActivityNode) execOneActivity(ctx types.RuleContext, metaData *rulegox.ActivityMetaData, act *activity.Activity, stepParamCtx *paramx.FlowContext) error {

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
		}
		resp, err := oneWorker.RequestActivity(ctx.GetContext(), newAct, dataMap, metaDataTemp.ToHeader(nil))
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
	actParam := make(map[string]any)
	for _, item := range bindConfig {
		exp := conv.String(item.Value)
		if item.Type == "formula" {
			expAny, err := x.ruleObj.RunString(exp, allParam)
			if err == nil {
				log.Printf("activityNode getActivityParam: RunString err: %v", err)
				actParam[item.Key] = expAny
				continue
			}
		}
		ruleObj := templates.NewTemplate(exp, "{{", "}}")
		tempVal := ruleObj.Replace(allParam)
		actParam[item.Key] = tempVal
	}
	return actParam
}

// getNodeId 获取当前节点的 ID（优先用 ctx 中的 SelfId）
func (x *ActivityNode) getNodeId(ctx types.RuleContext) paramx.StepId {
	if id := ctx.GetSelfId(); id != "" {
		return paramx.StepId(id)
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
