package commnode

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/redis/go-redis/v9"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
)

// 循环模式
const (
	LoopModeForeach = "foreach" // 遍历数组：每个元素执行一次循环体
	LoopModeWhile   = "while"   // 条件循环：每轮执行前判断继续条件
)

// 失败策略
const (
	LoopFailStop     = "stop"     // 任一轮失败即停止，节点走 Failure
	LoopFailContinue = "continue" // 跳过失败的轮次继续执行，最后汇总错误，节点走 Success
)

// 循环限制
const (
	DefaultLoopMaxIterations = 100
	MaxLoopIterations        = 1000
	MaxLoopConcurrency       = 20
	MaxLoopIntervalMs        = 60000
)

// LoopConfig 循环节点的循环配置（DSL configuration.loop）。
type LoopConfig struct {
	// Mode 循环模式：foreach / while，默认 foreach
	Mode string `json:"mode"`
	// Range 遍历模式的数据来源，如 {{steps.N1__ab.responses.list}}，也可以写 JSON 数组。
	// 现在在节点参数配置里通过 loop_items 参数配置（优先），这里只为兼容旧配置保留
	Range string `json:"range,omitempty"`
	// Condition 条件循环的继续条件表达式，每轮执行前判断，为 true 才执行
	Condition string `json:"condition,omitempty"`
	// BreakCondition 跳出条件表达式（可选），每轮执行后判断，为 true 即结束循环
	BreakCondition string `json:"break_condition,omitempty"`
	// BodyType 循环体类型：subchain / rootchain
	BodyType string `json:"body_type"`
	// BodyID 循环体的子链 ID 或根链 ID
	BodyID string `json:"body_id"`
	// BodyTarget 循环体为子链时，保存时嵌入本链 DSL 的子链 ID（子链ID#内容哈希），执行时用它在引擎池里找子链；
	// 为空（子链未能嵌入）时按 BodyID 实时加载
	BodyTarget string `json:"body_target,omitempty"`
	// MaxIterations 最大循环次数，默认 100，上限 1000
	MaxIterations int `json:"max_iterations,omitempty"`
	// Concurrency 并发数，默认 1（串行）；只有遍历模式、且没有跳出条件时可以大于 1
	Concurrency int `json:"concurrency,omitempty"`
	// IntervalMs 串行时每轮之间的间隔（毫秒）
	IntervalMs int `json:"interval_ms,omitempty"`
	// FailPolicy 失败策略：stop / continue，默认 stop
	FailPolicy string `json:"fail_policy,omitempty"`
}

// LoopIterationError 某一轮的失败信息。
type LoopIterationError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// LoopResult 循环节点的输出（写入 steps.<实例ID>.responses）。
type LoopResult struct {
	// Items 每轮的结果，按轮次顺序；失败的轮次为 null
	Items []any `json:"items"`
	// Count 实际执行的轮数
	Count   int `json:"count"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
	// Errors 失败轮次的错误信息
	Errors []LoopIterationError `json:"errors"`
	// BreakIndex 因跳出条件结束时的轮次序号（从 0 开始），没有跳出为 -1
	BreakIndex int `json:"break_index"`
}

// ParseLoopConfig 从节点 configuration 中解析循环配置（不做校验）。
func ParseLoopConfig(cfg types.Configuration) *LoopConfig {
	lc := new(LoopConfig)
	raw, ok := cfg[common.LoopConfigField]
	if !ok || raw == nil {
		return lc
	}
	switch v := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), lc)
	default:
		if b, err := json.Marshal(v); err == nil {
			_ = json.Unmarshal(b, lc)
		}
	}
	return lc
}

// LoopBodyRef 返回循环节点配置里的循环体类型与 ID，供发布校验、子链收集使用。
func LoopBodyRef(cfg types.Configuration) (bodyType, bodyID string) {
	lc := ParseLoopConfig(cfg)
	return strings.TrimSpace(lc.BodyType), strings.TrimSpace(lc.BodyID)
}

// normalize 填默认值并校验配置。
func (c *LoopConfig) normalize() error {
	c.Mode = strings.TrimSpace(c.Mode)
	if c.Mode == "" {
		c.Mode = LoopModeForeach
	}
	c.BodyType = strings.TrimSpace(c.BodyType)
	c.BodyID = strings.TrimSpace(c.BodyID)
	c.BodyTarget = strings.TrimSpace(c.BodyTarget)
	if i := strings.LastIndex(c.BodyTarget, ":"); i >= 0 {
		c.BodyTarget = c.BodyTarget[i+1:]
	}
	c.FailPolicy = strings.TrimSpace(c.FailPolicy)
	if c.FailPolicy == "" {
		c.FailPolicy = LoopFailStop
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = DefaultLoopMaxIterations
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	switch c.Mode {
	case LoopModeForeach:
		if strings.TrimSpace(c.Range) == "" {
			return fmt.Errorf("循环节点：遍历模式需要在节点参数配置里配置 %s（循环数据）", common.LoopItemsKey)
		}
	case LoopModeWhile:
		if strings.TrimSpace(c.Condition) == "" {
			return fmt.Errorf("循环节点：条件循环需要配置继续条件")
		}
	default:
		return fmt.Errorf("循环节点：不支持的循环模式 %s", c.Mode)
	}
	if c.BodyType != common.LoopBodySubChain && c.BodyType != common.LoopBodyRootChain {
		return fmt.Errorf("循环节点：循环体类型只能是 %s 或 %s", common.LoopBodySubChain, common.LoopBodyRootChain)
	}
	if c.BodyID == "" {
		return fmt.Errorf("循环节点未选择循环体")
	}
	if c.MaxIterations > MaxLoopIterations {
		return fmt.Errorf("循环节点：最大循环次数不能超过 %d", MaxLoopIterations)
	}
	if c.Concurrency > MaxLoopConcurrency {
		return fmt.Errorf("循环节点：并发数不能超过 %d", MaxLoopConcurrency)
	}
	if c.Concurrency > 1 && c.Mode == LoopModeWhile {
		return fmt.Errorf("循环节点：条件循环只能串行执行")
	}
	if c.Concurrency > 1 && strings.TrimSpace(c.BreakCondition) != "" {
		return fmt.Errorf("循环节点：配置了跳出条件时只能串行执行")
	}
	if c.IntervalMs < 0 || c.IntervalMs > MaxLoopIntervalMs {
		return fmt.Errorf("循环节点：每轮间隔需在 0~%d 毫秒之间", MaxLoopIntervalMs)
	}
	if c.FailPolicy != LoopFailStop && c.FailPolicy != LoopFailContinue {
		return fmt.Errorf("循环节点：不支持的失败策略 %s", c.FailPolicy)
	}
	return nil
}

// LoopNode 循环节点（custom/Loop）。
//
// 按配置的模式重复执行循环体，每轮把循环变量写入 arguments._loop：
//   - index：轮次序号（从 0 开始）；item：当前元素（条件循环为序号）；key：遍历对象时的键；
//   - last：上一轮的结果（条件循环判断继续条件、跳出条件时常用）。
//
// 循环体为子链时，复制一份当前执行上下文交给子链执行，取子链的返回值（有返回值节点时）或结束节点的返回值作为本轮结果；
// 循环体为根链时，按 arguments 映射出入参，调用目标根链的当前发布版本，取其返回值作为本轮结果。
// 全部结束后把汇总结果（LoopResult）写入 steps.<实例ID>.responses，成功走 Success，失败走 Failure。
type LoopNode struct {
	Config     *LoopConfig
	Arguments  []*param.BindConfig
	nodeLogCli *redis.Client
	nodeName   string
}

func (x *LoopNode) Type() string {
	return common.LoopNodeTypeName
}

func (x *LoopNode) New() types.Node {
	return &LoopNode{Config: new(LoopConfig)}
}

// Destroy 清理资源（无状态，无需处理）。
func (x *LoopNode) Destroy() {
}

func (x *LoopNode) Init(_ types.Config, configuration types.Configuration) error {
	ruleNode := base.NodeUtils.GetSelfDefinition(configuration.Copy())
	cfg := ruleNode.Configuration
	if len(cfg) == 0 {
		cfg = configuration
	}
	if err := x.initFromConfig(cfg); err != nil {
		return err
	}
	if ruleNode.Name != "" {
		x.nodeName = ruleNode.Name
	}
	return nil
}

// initFromConfig 解析循环配置与参数：参数里的 loop_items 作为循环数据（优先于循环配置里的旧字段 range），
// 其余参数是循环体为根链时传给目标根链的入参。
func (x *LoopNode) initFromConfig(cfg types.Configuration) error {
	x.Config = ParseLoopConfig(cfg)
	x.Arguments = nil
	if raw, ok := cfg["arguments"]; ok && raw != nil {
		// 与 dsl_builder 一致：[]*param.BindConfig 用标准 json 解析
		b, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("循环节点：解析入参失败: %w", err)
		}
		var args []*param.BindConfig
		if err := json.Unmarshal(b, &args); err != nil {
			return fmt.Errorf("循环节点：解析入参失败: %w", err)
		}
		for _, a := range args {
			if a == nil {
				continue
			}
			switch a.Key {
			case common.LoopItemsKey:
				if v := strings.TrimSpace(conv.String(a.Value)); v != "" {
					x.Config.Range = v
				}
			default:
				x.Arguments = append(x.Arguments, a)
			}
		}
	}
	return x.Config.normalize()
}

// ValidateLoopNodeConfig 按执行时的规则校验循环节点配置（发布前校验用）。
func ValidateLoopNodeConfig(cfg types.Configuration) error {
	return (&LoopNode{}).initFromConfig(cfg)
}

// OnMsg 解析循环数据 → 逐轮执行循环体 → 写入汇总结果。
func (x *LoopNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	startTime := time.Now()
	actMetaData := getActionMeta(msg)
	currNodeId := getNodeId(ctx)
	nodeSpanId := id.GetUUID(string(currNodeId))
	nodeStep := &paramx.Step{
		Arguments: map[string]any{
			"mode":      x.Config.Mode,
			"body_type": x.Config.BodyType,
			"body_id":   x.Config.BodyID,
		},
		Status:      paramx.StepStatusPending,
		StartTimeMs: startTime.UnixMilli(),
	}

	allParam := new(paramx.FlowContext)
	if err := conv.Unmarshal(msg.GetData(), allParam); err != nil {
		x.fail(ctx, msg, allParam, currNodeId, actMetaData, nodeSpanId, startTime, nodeStep, fmt.Errorf("解析流程上下文失败: %w", err))
		return
	}

	result, err := x.run(ctx, msg, allParam, actMetaData)
	nodeStep.Responses = result
	if err != nil {
		x.fail(ctx, msg, allParam, currNodeId, actMetaData, nodeSpanId, startTime, nodeStep, err)
		return
	}
	nodeStep.Status = paramx.StepStatusSuccess
	nodeStep.EndTimeMs = time.Now().UnixMilli()
	allParam.SetStep(currNodeId, nodeStep)
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), string(currNodeId), x.nodeName, "success", "info", types.Success, allParam,
		nodeStep.Arguments, result, nil)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellSuccess(msg)
}

// loopItem 一轮的循环变量。
type loopItem struct {
	key  string
	item any
}

// run 执行整个循环，返回汇总结果；按失败策略停止时同时返回已执行部分的结果与错误。
func (x *LoopNode) run(ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, meta *rulegox.ActivityMetaData) (*LoopResult, error) {
	result := &LoopResult{Items: []any{}, Errors: []LoopIterationError{}, BreakIndex: -1}
	goCtx := ctx.GetContext()
	if goCtx == nil {
		goCtx = context.Background()
	}
	if x.Config.BodyType == common.LoopBodySubChain {
		if _, ok := rulego.Get(loopSubChainKey(meta.Project, x.Config.subChainTarget())); !ok {
			return result, fmt.Errorf("循环体子链 %s 未加载", x.Config.BodyID)
		}
	}

	if x.Config.Mode == LoopModeWhile {
		return x.runWhile(goCtx, ctx, msg, allParam, meta, result)
	}

	dataMap, _ := allParam.ToMaps()
	items, err := x.resolveItems(dataMap)
	if err != nil {
		return result, err
	}
	if len(items) > x.Config.MaxIterations {
		return result, fmt.Errorf("循环数据有 %d 条，超过最大循环次数 %d", len(items), x.Config.MaxIterations)
	}
	if x.Config.Concurrency > 1 && len(items) > 1 {
		return x.runParallel(goCtx, ctx, msg, allParam, meta, items, result)
	}

	var last any
	for i, it := range items {
		if i > 0 {
			if err := sleepCtx(goCtx, x.Config.IntervalMs); err != nil {
				return result, err
			}
		}
		vars := loopVars(i, it, last)
		res, runErr := x.runIteration(goCtx, ctx, msg, allParam, meta, vars)
		if stop, err := x.record(result, i, res, runErr); stop {
			return result, err
		}
		last = res
		if broke, err := x.checkBreak(allParam, i, it, res); err != nil {
			return result, err
		} else if broke {
			result.BreakIndex = i
			break
		}
	}
	return result, nil
}

// runWhile 条件循环：每轮执行前判断继续条件，超过最大循环次数仍满足条件则报错。
func (x *LoopNode) runWhile(goCtx context.Context, ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, meta *rulegox.ActivityMetaData, result *LoopResult) (*LoopResult, error) {
	var last any
	for i := 0; ; i++ {
		it := loopItem{item: i}
		ok, err := evalLoopCondition(x.Config.Condition, loopIterationContext(allParam, loopVars(i, it, last)))
		if err != nil {
			return result, fmt.Errorf("循环继续条件求值失败: %w", err)
		}
		if !ok {
			return result, nil
		}
		if i >= x.Config.MaxIterations {
			return result, fmt.Errorf("条件循环执行了 %d 轮仍满足继续条件，超过最大循环次数", x.Config.MaxIterations)
		}
		if i > 0 {
			if err := sleepCtx(goCtx, x.Config.IntervalMs); err != nil {
				return result, err
			}
		}
		res, runErr := x.runIteration(goCtx, ctx, msg, allParam, meta, loopVars(i, it, last))
		if stop, err := x.record(result, i, res, runErr); stop {
			return result, err
		}
		last = res
		if broke, err := x.checkBreak(allParam, i, it, res); err != nil {
			return result, err
		} else if broke {
			result.BreakIndex = i
			return result, nil
		}
	}
}

// runParallel 并发执行（遍历模式且无跳出条件）；结果按轮次顺序排列。
// 失败策略为 stop 时，第一轮失败后不再启动新的轮次，已启动的轮次跑完后返回第一个错误。
func (x *LoopNode) runParallel(goCtx context.Context, ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, meta *rulegox.ActivityMetaData, items []loopItem, result *LoopResult) (*LoopResult, error) {
	runCtx, cancel := context.WithCancel(goCtx)
	defer cancel()
	type outcome struct {
		done bool
		res  any
		err  error
	}
	outcomes := make([]outcome, len(items))
	sem := make(chan struct{}, x.Config.Concurrency)
	var wg sync.WaitGroup
	var stopOnce sync.Once
	var stopped bool
	var mu sync.Mutex
	for i, it := range items {
		mu.Lock()
		s := stopped
		mu.Unlock()
		if s || runCtx.Err() != nil {
			break
		}
		sem <- struct{}{}
		// 排队期间可能已有轮次失败（stop 策略），拿到并发名额后再检查一次
		mu.Lock()
		s = stopped
		mu.Unlock()
		if s || runCtx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int, it loopItem) {
			defer wg.Done()
			defer func() { <-sem }()
			var res any
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("循环体执行 panic: %v", r)
					}
				}()
				res, err = x.runIteration(runCtx, ctx, msg, allParam, meta, loopVars(i, it, nil))
			}()
			mu.Lock()
			outcomes[i] = outcome{done: true, res: res, err: err}
			mu.Unlock()
			if err != nil && x.Config.FailPolicy == LoopFailStop {
				stopOnce.Do(func() {
					mu.Lock()
					stopped = true
					mu.Unlock()
				})
			}
		}(i, it)
	}
	wg.Wait()
	if err := goCtx.Err(); err != nil {
		return result, fmt.Errorf("循环被取消: %w", err)
	}
	var firstErr error
	for i, o := range outcomes {
		if !o.done {
			continue
		}
		if stop, err := x.record(result, i, o.res, o.err); stop && firstErr == nil {
			firstErr = err
		}
	}
	return result, firstErr
}

// record 记录一轮的结果；按失败策略需要停止时返回 stop=true 与包装后的错误。
func (x *LoopNode) record(result *LoopResult, index int, res any, runErr error) (bool, error) {
	result.Count++
	if runErr != nil {
		result.Failed++
		result.Items = append(result.Items, nil)
		result.Errors = append(result.Errors, LoopIterationError{Index: index, Message: runErr.Error()})
		if x.Config.FailPolicy == LoopFailStop {
			return true, fmt.Errorf("循环第 %d 轮执行失败: %w", index+1, runErr)
		}
		return false, nil
	}
	result.Success++
	result.Items = append(result.Items, res)
	return false, nil
}

// checkBreak 每轮执行后判断跳出条件（未配置时不跳出）。
func (x *LoopNode) checkBreak(allParam *paramx.FlowContext, index int, it loopItem, res any) (bool, error) {
	expr := strings.TrimSpace(x.Config.BreakCondition)
	if expr == "" {
		return false, nil
	}
	ok, err := evalLoopCondition(expr, loopIterationContext(allParam, loopVars(index, it, res)))
	if err != nil {
		return false, fmt.Errorf("循环跳出条件求值失败: %w", err)
	}
	return ok, nil
}

// runIteration 执行一轮循环体。
func (x *LoopNode) runIteration(goCtx context.Context, ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, meta *rulegox.ActivityMetaData, vars map[string]any) (any, error) {
	if err := goCtx.Err(); err != nil {
		return nil, fmt.Errorf("循环被取消: %w", err)
	}
	iterCtx := loopIterationContext(allParam, vars)
	if x.Config.BodyType == common.LoopBodyRootChain {
		dataMap, _ := iterCtx.ToMaps()
		args := replaceBindConfig(templates.NewRuleExprEngine(), dataMap, x.Arguments)
		return invokeRootChainTarget(goCtx, meta, x.Config.BodyID, args)
	}
	return runLoopSubChain(goCtx, ctx, msg, loopSubChainKey(meta.Project, x.Config.subChainTarget()), iterCtx)
}

// runLoopSubChain 用 iterCtx 作为消息体执行一次子链，等全部节点执行完后返回本轮结果：
// 子链里有返回值节点时取其写入的 responses，否则取结束节点的 responses。
func runLoopSubChain(goCtx context.Context, ctx types.RuleContext, msg types.RuleMsg, poolKey string, iterCtx *paramx.FlowContext) (any, error) {
	iterCtx.SetResponses(nil)
	subMsg := msg.Copy()
	subMsg.SetData(conv.String(iterCtx))

	var (
		mu      sync.Mutex
		endData string
		endNode string
		hasEnd  bool
		endErr  error
		once    sync.Once
		done    = make(chan struct{})
	)
	ctx.TellFlow(poolKey, subMsg, types.WithContext(goCtx),
		types.WithOnEnd(func(c types.RuleContext, m types.RuleMsg, err error, relationType string) {
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if endErr == nil {
					endErr = err
				}
				return
			}
			if !hasEnd {
				hasEnd = true
				endData = m.GetData()
				if c != nil {
					endNode = c.GetSelfId()
				}
			}
		}),
		types.WithOnAllNodeCompleted(func() {
			once.Do(func() { close(done) })
		}))
	select {
	case <-done:
	case <-goCtx.Done():
		return nil, fmt.Errorf("循环被取消: %w", goCtx.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	if endErr != nil {
		return nil, endErr
	}
	if !hasEnd {
		return nil, nil
	}
	out := new(paramx.FlowContext)
	if err := conv.Unmarshal(endData, out); err != nil {
		return nil, fmt.Errorf("解析子链结果失败: %w", err)
	}
	if resp := out.GetResponses(); resp != nil {
		return resp, nil
	}
	if endNode != "" {
		if step := out.GetStep(paramx.StepId(endNode)); step != nil {
			return step.Responses, nil
		}
	}
	return nil, nil
}

// subChainTarget 循环体子链在引擎池里的 ID：优先用嵌入的子链 ID，没有嵌入时用子链 ID。
func (c *LoopConfig) subChainTarget() string {
	if c.BodyTarget != "" {
		return c.BodyTarget
	}
	return c.BodyID
}

// loopSubChainKey 子链在引擎池里的 key，与 flow 节点、rulego_flow 注册子链时一致：project:chainID。
func loopSubChainKey(project, subChainID string) string {
	return project + ":" + subChainID
}

// loopVars 一轮的循环变量（写入 arguments._loop）。
func loopVars(index int, it loopItem, last any) map[string]any {
	vars := map[string]any{
		"index": index,
		"item":  it.item,
		"last":  last,
	}
	if it.key != "" {
		vars["key"] = it.key
	}
	return vars
}

// loopIterationContext 复制一份执行上下文，并把循环变量写入 arguments._loop。
func loopIterationContext(allParam *paramx.FlowContext, vars map[string]any) *paramx.FlowContext {
	iterCtx := new(paramx.FlowContext)
	_ = conv.Unmarshal(conv.String(allParam), iterCtx)
	iterCtx.SetVariable(common.LoopVarKey, vars)
	return iterCtx
}

// resolveItems 解析遍历数据，得到每一轮的循环变量。
func (x *LoopNode) resolveItems(dataMap map[string]any) ([]loopItem, error) {
	v, err := resolveLoopValue(x.Config.Range, dataMap)
	if err != nil {
		return nil, fmt.Errorf("解析循环数据失败: %w", err)
	}
	return toLoopItems(v)
}

var loopSinglePlaceholderRe = regexp.MustCompile(`^\{\{\s*([^{}]+?)\s*}}$`)

// resolveLoopValue 解析循环数据来源：
//   - 单个 {{path}} 占位符：按路径从执行上下文取原始值（数组、对象保持原样）；
//   - JSON 数组 / 对象字面量；
//   - 含 {{}} 的模板：替换后得到字符串；
//   - 其它：作为表达式求值（如 len(steps.N1.responses.list)）。
func resolveLoopValue(expr string, dataMap map[string]any) (any, error) {
	s := strings.TrimSpace(expr)
	if s == "" {
		return nil, fmt.Errorf("未配置")
	}
	if m := loopSinglePlaceholderRe.FindStringSubmatch(s); m != nil {
		v, ok := lookupArgPath(dataMap, strings.TrimSpace(m[1]))
		if !ok {
			return nil, fmt.Errorf("取不到 %s", s)
		}
		return v, nil
	}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v, nil
		}
	}
	if strings.Contains(s, "{{") {
		return templates.NewTemplate(s, templates.DefaultPrefix, templates.DefaultSuffix).Replace(dataMap), nil
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s, nil
	}
	v, err := templates.NewRuleExprEngine().RunString(s, dataMap)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// toLoopItems 把遍历数据转成每轮的循环变量：数组按下标；对象按键排序，key 为键、item 为值；
// JSON 字符串先解析；空值视为没有数据（执行 0 轮）。
func toLoopItems(v any) ([]loopItem, error) {
	switch val := v.(type) {
	case nil:
		return nil, nil
	case []any:
		items := make([]loopItem, len(val))
		for i, it := range val {
			items[i] = loopItem{item: it}
		}
		return items, nil
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		items := make([]loopItem, len(keys))
		for i, k := range keys {
			items[i] = loopItem{key: k, item: val[k]}
		}
		return items, nil
	case string:
		s := strings.TrimSpace(val)
		if s == "" || s == "null" {
			return nil, nil
		}
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return nil, fmt.Errorf("循环数据不是数组或对象：%s", s)
		}
		if _, ok := parsed.(string); ok {
			return nil, fmt.Errorf("循环数据不是数组或对象：%s", s)
		}
		return toLoopItems(parsed)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return nil, fmt.Errorf("循环数据不是数组或对象：%v", val)
		}
		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil {
			return nil, fmt.Errorf("循环数据不是数组或对象：%v", val)
		}
		switch parsed.(type) {
		case []any, map[string]any:
			return toLoopItems(parsed)
		}
		return nil, fmt.Errorf("循环数据不是数组或对象：%s", string(b))
	}
}

// evalLoopCondition 在 iterCtx 上对条件表达式求值，结果转为 bool。
func evalLoopCondition(expr string, iterCtx *paramx.FlowContext) (bool, error) {
	dataMap, _ := iterCtx.ToMaps()
	v, err := templates.NewRuleExprEngine().RunString(strings.TrimSpace(expr), dataMap)
	if err != nil {
		return false, err
	}
	b, err := conv.Convert[bool](v)
	if err != nil {
		return false, fmt.Errorf("条件结果 %v 不是布尔值", v)
	}
	return b, nil
}

// sleepCtx 等待 ms 毫秒，期间 ctx 取消则返回错误。
func sleepCtx(ctx context.Context, ms int) error {
	if ms <= 0 {
		return nil
	}
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("循环被取消: %w", ctx.Err())
	}
}

// fail 记录失败步骤、上报失败日志与告警，并走 Failure 分支。
func (x *LoopNode) fail(ctx types.RuleContext, msg types.RuleMsg, allParam *paramx.FlowContext, currNodeId paramx.StepId,
	actMetaData *rulegox.ActivityMetaData, nodeSpanId string, startTime time.Time, nodeStep *paramx.Step, err error) {
	nodeStr := string(currNodeId)
	sendAlert(context.Background(), actMetaData.Project, actMetaData.Env, "[工作流告警] Node 执行失败",
		fmt.Sprintf("流程ID: %s\n流程名称: %s\n节点名称: %s\n节点ID: %s\n项目: %s\n环境: %s\nTraceId: %s\n错误信息: %s\n时间: %s",
			actMetaData.RootChainID, actMetaData.RootChainName, x.nodeName, nodeStr, actMetaData.Project, actMetaData.Env, actMetaData.TraceId, err.Error(), time.Now().Format("2006-01-02 15:04:05")))

	nodeStep.Status = paramx.StepStatusFail
	nodeStep.EndTimeMs = time.Now().UnixMilli()
	nodeStep.Error = &paramx.ErrorInfo{
		Code:    "500",
		Message: err.Error(),
		Stack:   err.Error(),
	}
	allParam.SetStep(currNodeId, nodeStep)
	msg.SetData(conv.String(allParam))

	nodeCli, cliErr := pushNodeLog(x.nodeLogCli, actMetaData, nodeSpanId, time.Since(startTime).Milliseconds(), nodeStr, x.nodeName, "fail", "error", types.Failure, allParam,
		nodeStep.Arguments, nodeStep.Responses, err)
	if cliErr == nil && x.nodeLogCli == nil {
		x.nodeLogCli = nodeCli
	}
	ctx.TellFailure(msg, err)
}

func init() {
	Registry.Add(&LoopNode{})
	_ = rulego.Registry.Register(&LoopNode{})
}
