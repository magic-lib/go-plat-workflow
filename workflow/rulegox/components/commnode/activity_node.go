package commnode

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/goroutines"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"go.uber.org/multierr"
	"log"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/action"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
	"github.com/samber/lo"
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
	allParam := paramx.NewParamCtx()
	if err := conv.Unmarshal(allParamStr, allParam); err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	// 合并 ArgMapping 和 BindArgs 到 allParam 中
	nodeParams, err := NodeParams(allParam, x.Configuration)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	stepParamCtx := paramx.NewParamCtx()
	stepParamCtx.SetVariables(nodeParams)

	metaDataAny := msg.GetMetadata()
	metaDataMap := make(map[string]any)
	metaDataAny.ForEach(func(key string, value string) bool {
		metaDataMap[key] = value
		return true
	})
	actMetaData := new(rulegox.ActivityMetaData)
	_ = conv.Unmarshal(metaDataMap, actMetaData)

	log.Print("activityNode OnMsg:", conv.String(actMetaData))

	// ——— 按阶段依次执行 ———
	for stageIdx, stage := range x.activities {
		if len(stage) == 0 {
			continue
		}
		if len(stage) == 1 {
			// 单活动阶段：串行执行
			if err := x.execOneActivity(ctx, actMetaData, stage[0], stageIdx, "S", stepParamCtx, allParam); err != nil {
				ctx.TellFailure(msg, err)
				return
			}
		} else {
			// 多活动阶段：并发执行，等待全部完成后进入下一阶段
			if err := x.execParallelStage(ctx, actMetaData, stageIdx, stage, stepParamCtx, allParam); err != nil {
				ctx.TellFailure(msg, err)
				return
			}
		}
	}

	// 设置 Action 元数据
	if msg.Metadata == nil {
		msg.Metadata = types.NewMetadata()
	}
	lo.ForEach(flattenStages(x.activities), func(item *activity.Activity, i int) {
		x.setActionMeta(msg, item)
	})

	execStep := x.getNodeId(ctx)
	if execStep == "" {
		ctx.TellFailure(msg, fmt.Errorf("activityNode execStep is empty"))
		return
	}
	allParam.SetStepStruct(execStep, stepParamCtx)
	msg.SetData(conv.String(allParam))
	ctx.TellSuccess(msg)
}

// execParallelStage 并发执行一个阶段内的所有 Activity，等待全部完成后合并结果到 allParam。
func (x *ActivityNode) execParallelStage(ctx types.RuleContext, metaData *rulegox.ActivityMetaData, stageIdx int,
	stage []*activity.Activity, stepParamCtx, allParam *paramx.ParamCtx) error {

	if len(stage) == 0 {
		return nil
	}
	var mu sync.Mutex
	prefix := fmt.Sprintf("P%d", stageIdx) // 如 "P0", "P2"，与 idx 拼接后得到 "P00", "P01", "P20", "P21"

	var retErr error

	_, _ = goroutines.AsyncExecuteDataList[*activity.Activity](30*time.Second, stage, func(value *activity.Activity, key int) (breakFlag bool, err error) {
		mu.Lock()
		snapAllParam := deepCopyParamCtx(allParam)
		snapStepCtx := deepCopyParamCtx(stepParamCtx)
		mu.Unlock()

		if err := x.execOneActivity(ctx, metaData, value, key, prefix, snapStepCtx, snapAllParam); err != nil {
			// 执行失败，别的并行的程序不能退出，继续执行完成
			retErr = multierr.Append(retErr, err)
			return false, err
		}
		// 结果合并回全局 allParam
		mu.Lock()
		for stepId, ps := range snapAllParam.Steps {
			if ps.Arguments != nil {
				allParam.SetStepArguments(stepId, ps.Arguments)
			}
			if ps.Responses != nil {
				allParam.SetStepResponse(stepId, ps.Responses)
			}
		}
		mu.Unlock()
		return false, nil
	})

	return retErr
}

// deepCopyParamCtx 通过序列化再反序列化实现 ParamCtx 深拷贝。
func deepCopyParamCtx(src *paramx.ParamCtx) *paramx.ParamCtx {
	dst := paramx.NewParamCtx()
	_ = conv.Unmarshal(conv.String(src), dst)
	return dst
}

// execOneActivity 执行单个 Activity，结果回写 allParam。
// 执行策略：
//   - 若已注入 mqExecutor 且执行环境（metaData.Env）非空，则通过 MQ 同步调用分布式
//     worker 远程执行该 Activity（复用 workflow.MQExecutor.RequestActivity），
//     适用于生产环境依赖远程监听程序的 Activity。
//   - 否则（未注入执行器或环境为空）回退到本地 newAct.Execute，保证单测/无 MQ 场景可用。
func (x *ActivityNode) execOneActivity(ctx types.RuleContext, metaData *rulegox.ActivityMetaData, act *activity.Activity,
	idx int, prefix string, stepParamCtx, allParam *paramx.ParamCtx) error {

	newAct := act.Clone()
	stepId := paramx.StepId(fmt.Sprintf("%s%d", prefix, idx))
	if newAct.Id == "" {
		newAct.Id = string(stepId)
	}

	dataMap := stepParamCtx.StepMapsByStepId(stepId)

	// 优先走 MQ 远程执行（生产环境依赖远程 worker 的 Activity）。
	if x.mqExecutor != nil && metaData != nil && metaData.Env != "" {
		project := metaData.Project
		resp, err := x.mqExecutor.ExecActivityViaMQ(ctx.GetContext(), &ActivityMQRequest{
			Act:         newAct,
			Project:     project,
			Env:         metaData.Env,
			RootChainID: metaData.RootChainID,
			TraceID:     metaData.TraceId,
			StepID:      string(stepId),
			Input:       dataMap,
		})
		if err != nil {
			return err
		}
		// 将 MQ 返回结果按本地执行一致的格式写回 allParam：
		// 以 stepId 为 key、responses 存放远程返回值，后续逻辑与本地执行对齐。
		respData := map[string]any{
			string(stepId): map[string]any{
				"responses": resp.Data,
			},
		}
		if oneFuncData, ok := respData[string(stepId)]; ok {
			oneFuncParam := new(paramx.ParamCtx)
			_ = conv.Unmarshal(conv.String(oneFuncData), oneFuncParam)
			allParam.SetStepArguments(stepId, oneFuncParam.Arguments)
			allParam.SetStepResponse(stepId, oneFuncParam.Responses)
		}
		if stepResp, ok := allParam.GetStepResponse(stepId); ok {
			allParam.SetVariable(string(stepId), stepResp)
		}
		return nil
	}

	// 回退：本地执行（无 MQ 或环境为空时）。
	respData, err := newAct.Execute(ctx.GetContext(), dataMap)
	if err != nil {
		return err
	}

	if oneFuncData, ok := respData[newAct.Id]; ok {
		oneFuncParam := new(paramx.ParamCtx)
		_ = conv.Unmarshal(oneFuncData, oneFuncParam)
		allParam.SetStepArguments(stepId, oneFuncParam.Arguments)
		allParam.SetStepResponse(stepId, oneFuncParam.Responses)
	}

	if stepResp, ok := allParam.GetStepResponse(stepId); ok {
		allParam.SetVariable(string(stepId), stepResp)
	}
	return nil
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
