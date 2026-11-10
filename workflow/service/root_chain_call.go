package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	param "github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/builder"
	"github.com/magic-lib/go-plat-workflow/workflow/common"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox/components/commnode"
	"github.com/rs/zerolog/log"
	"github.com/rulego/rulego/api/types"
	"github.com/samber/lo"
)

// rootChainCallInvoker 调用根链节点的根链调用实现：按目标根链 ID 找到 chain_key，
// 走与线上 invoke 相同的路径（当前发布版本 + 版本隔离的引擎池），同步返回目标根链的返回值。
type rootChainCallInvoker struct {
	s *WorkflowService
}

func (a rootChainCallInvoker) InvokeRootChainForNode(ctx context.Context, req *commnode.RootChainInvokeRequest) (any, error) {
	def, err := a.s.rootChainRepo.GetByProjectID(ctx, req.Project, req.ChainID)
	if err != nil {
		return nil, fmt.Errorf("get root chain %s failed: %w", req.ChainID, err)
	}
	result, err := a.s.InvokeRootChain(ctx, req.Project, def.ChainKey, req.Env, req.TraceId, req.Payload, false)
	if err != nil {
		return nil, err
	}
	return rootChainResponses(result), nil
}

// ensureBuiltinRootChainCallNode 幂等确保项目下存在内置「调用根链节点」（custom/RootChainCall）定义，
// 做法与 ensureBuiltinReturnNode 一致：入库为普通 NodeDef，node_id 由 NextNodeID 生成。
func (s *WorkflowService) ensureBuiltinRootChainCallNode(ctx context.Context, project string) error {
	exists, err := s.nodeRepo.ExistsByType(ctx, project, common.RootChainCallNodeTypeName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	nodeID, err := s.nodeRepo.NextNodeID(ctx)
	if err != nil {
		return err
	}
	def := &workflow.NodeDef{
		Project: project,
		NodeID:  nodeID,
		Name:    "调用根链节点",
		Type:    common.RootChainCallNodeTypeName,
		Kind:    "call",
		Status:  models.NodeStatusEnabled,
	}
	return s.nodeRepo.Create(ctx, def)
}

// GetRootChainCallSignature 获取根链当前发布版本的调用签名（入参列表 + 返回值定义），
// 供编排页添加「调用根链节点」时生成参数配置与可引用的返回值字段。未发布的根链返回错误。
func (s *WorkflowService) GetRootChainCallSignature(ctx context.Context, project, chainID string) (*workflow.RootChainCallSignature, error) {
	sig, _, err := s.rootChainCallSignature(ctx, project, chainID)
	return sig, err
}

// rootChainCallSignature 读取目标根链当前发布版本：入参取发布 DSL（含与它共用入参的子链）里所有 {{arguments.xxx}}，
// 返回值取发布快照的 root_responses。同时返回发布版本的全部节点（含子链节点），供循环调用检查继续向下遍历。
func (s *WorkflowService) rootChainCallSignature(ctx context.Context, project, chainID string) (*workflow.RootChainCallSignature, []*types.RuleNode, error) {
	def, err := s.rootChainRepo.GetByProjectID(ctx, project, chainID)
	if err != nil {
		return nil, nil, fmt.Errorf("目标根链 %s 不存在或已停用: %w", chainID, err)
	}
	release, err := s.releaseRepo.GetCurrent(ctx, project, chainID)
	if err != nil {
		return nil, nil, fmt.Errorf("目标根链 %s 没有线上发布版本: %w", chainID, err)
	}
	rc := &types.RuleChain{}
	if err := json.Unmarshal([]byte(release.DSLJSON), rc); err != nil {
		return nil, nil, fmt.Errorf("parse root chain %s release dsl failed: %w", chainID, err)
	}
	_, subNodes, subArgNodes, err := s.collectSubChainsWithArgNodes(ctx, project, rc)
	if err != nil {
		return nil, nil, err
	}
	allNodes := make([]*types.RuleNode, 0, len(rc.Metadata.Nodes)+len(subNodes))
	allNodes = append(allNodes, rc.Metadata.Nodes...)
	allNodes = append(allNodes, subNodes...)
	// 入参只取本链节点与共用入参的子链节点（经调用子链节点引用的子链，入参由该节点的参数配置给出）
	argNodes := append(append(make([]*types.RuleNode, 0, len(rc.Metadata.Nodes)+len(subArgNodes)), rc.Metadata.Nodes...), subArgNodes...)

	responses := make([]workflow.RootResponseItem, 0)
	if strings.TrimSpace(release.RootResponses) != "" {
		if err := json.Unmarshal([]byte(release.RootResponses), &responses); err != nil {
			return nil, nil, fmt.Errorf("parse root chain %s root_responses failed: %w", chainID, err)
		}
	}
	return &workflow.RootChainCallSignature{
		ChainID:   def.ChainID,
		ChainKey:  def.ChainKey,
		Name:      release.Name,
		Version:   release.Version,
		Arguments: builder.CollectAllInputArguments(argNodes),
		Responses: responses,
	}, allNodes, nil
}

// checkRootChainCalls 发布前校验链中的调用根链节点：
//   - 必须选择了目标根链，且目标根链有线上发布版本；
//   - 节点配置的入参覆盖目标根链当前发布版本所需的全部入参；
//   - 沿调用关系向下不能回到已在调用路径上的根链（循环调用），嵌套层数不超过 commnode.MaxRootChainCallDepth。
func (s *WorkflowService) checkRootChainCalls(ctx context.Context, project, chainID string, nodes []*types.RuleNode) error {
	return s.walkRootChainCalls(ctx, project, []string{chainID}, nodes, true)
}

func (s *WorkflowService) walkRootChainCalls(ctx context.Context, project string, path []string, nodes []*types.RuleNode, checkArgs bool) error {
	for _, n := range nodes {
		if n == nil || n.Type != common.RootChainCallNodeTypeName {
			continue
		}
		target := strings.TrimSpace(conv.String(n.Configuration[common.RootChainCallConfigKey]))
		if target == "" {
			return fmt.Errorf("调用根链节点 %s 未选择目标根链", n.Id)
		}
		if lo.Contains(path, target) {
			return fmt.Errorf("根链循环调用：%s -> %s（节点 %s）", strings.Join(path, " -> "), target, n.Id)
		}
		if len(path) >= commnode.MaxRootChainCallDepth {
			return fmt.Errorf("根链调用嵌套超过 %d 层：%s -> %s", commnode.MaxRootChainCallDepth, strings.Join(path, " -> "), target)
		}
		sig, targetNodes, err := s.rootChainCallSignature(ctx, project, target)
		if err != nil {
			return fmt.Errorf("调用根链节点 %s：%w", n.Id, err)
		}
		if checkArgs {
			if missing := missingRootChainCallArgs(n, sig.Arguments); len(missing) > 0 {
				return fmt.Errorf("调用根链节点 %s 缺少目标根链 %s 的入参：%s，请在编排页重新选择该根链后补齐", n.Id, target, strings.Join(missing, ","))
			}
		}
		next := append(append(make([]string, 0, len(path)+1), path...), target)
		if err := s.walkRootChainCalls(ctx, project, next, targetNodes, false); err != nil {
			return err
		}
	}
	return nil
}

// missingRootChainCallArgs 返回目标根链所需、但调用根链节点 arguments 中没有配置的入参名。
func missingRootChainCallArgs(n *types.RuleNode, required []string) []string {
	var args []*param.BindConfig
	if raw, ok := n.Configuration["arguments"]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &args)
		}
	}
	have := make(map[string]bool, len(args))
	for _, a := range args {
		if a != nil {
			have[a.Key] = true
		}
	}
	missing := make([]string, 0)
	for _, k := range required {
		if k != "" && !have[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// testRootChainCallNode 测试调用根链节点：把测试参数原样作为入参，执行节点定义固定的目标根链当前发布版本，
// 输出只取节点定义的返回值（没有定义返回值时为 {}），不带出目标根链的内部执行结果；
// 并在该节点的运行日志中记录本次输入（arguments）与输出（result），可按需保存测试记录。
func (s *WorkflowService) testRootChainCallNode(ctx context.Context, req *TestNodeRequest, nodeDef *workflow.NodeDef) (*TestNodeResult, error) {
	cfg := make(map[string]any)
	if len(nodeDef.Configuration) > 0 {
		if err := json.Unmarshal(nodeDef.Configuration, &cfg); err != nil {
			return nil, fmt.Errorf("parse root chain call node configuration: %w", err)
		}
	}
	target := strings.TrimSpace(conv.String(cfg[common.RootChainCallConfigKey]))
	if target == "" {
		return nil, fmt.Errorf("调用根链节点 %s 未固定目标根链，请在节点编辑页选择目标根链，或在根链编排中执行测试", nodeDef.NodeID)
	}
	input := req.InputParams
	if input == nil {
		input = map[string]any{}
	}

	traceID := id.NewUUID()
	start := time.Now()
	output, releaseID, runErr := s.invokeRootChainForTest(ctx, req.Project, target, req.EnvName, traceID, input)
	durationMs := time.Since(start).Milliseconds()

	result := &TestNodeResult{TraceID: traceID, DurationMs: durationMs}
	if runErr != nil {
		result.Status = "fail"
		result.ErrorMsg = runErr.Error()
	} else {
		result.Status = "success"
		result.Result = conv.String(commnode.PickRootChainCallResponses(rootChainResponses(output), commnode.RootChainCallResponseKeys(cfg["responses"])))
	}

	// 节点运行日志：输入为测试参数，输出为目标根链的完整输出
	logDef := &workflow.NodeLogDef{
		Project:            req.Project,
		Env:                req.EnvName,
		NodeID:             nodeDef.NodeID,
		NodeName:           nodeDef.Name,
		EventID:            result.Status,
		Level:              "info",
		Timestamp:          start.Unix(),
		DurationMs:         durationMs,
		Payload:            json.RawMessage(conv.String(input)),
		Arguments:          json.RawMessage(conv.String(input)),
		Result:             result.Result,
		ErrorMsg:           result.ErrorMsg,
		TraceID:            traceID,
		RootChainID:        target,
		RootChainReleaseID: releaseID,
		SpanID:             id.GetUUID(nodeDef.NodeID + traceID),
		RelationType:       types.Success,
		CreatedAt:          time.Now(),
	}
	if runErr != nil {
		logDef.Level = "error"
		logDef.RelationType = types.Failure
	}
	if err := s.nodeLogRepo.Create(ctx, logDef); err != nil {
		log.Ctx(ctx).Warn().Err(err).Str("node_id", nodeDef.NodeID).Msg("save root chain call node test log failed")
	}

	if req.SaveRecord {
		record := &workflow.NodeTestRecordDef{
			Project:     req.Project,
			NodeID:      req.NodeID,
			NodeName:    nodeDef.Name,
			EnvName:     req.EnvName,
			TraceID:     traceID,
			InputParams: mustJSON(input),
			EnvVars:     mustJSON(map[string]string{}),
			Status:      result.Status,
			Result:      result.Result,
			ErrorMsg:    result.ErrorMsg,
			DurationMs:  durationMs,
		}
		if err := s.CreateNodeTestRecord(ctx, record); err == nil {
			result.RecordID = record.RecordID
		}
	}
	return result, nil
}

// rootChainResponses 取根链执行结果中的返回值（FlowContext.Responses）；非 FlowContext 时原样返回。
func rootChainResponses(output any) any {
	if flowCtx, ok := output.(*paramx.FlowContext); ok && flowCtx != nil {
		return flowCtx.GetResponses()
	}
	return output
}

// invokeRootChainForTest 按根链 ID 执行其当前发布版本，返回完整输出与发布版本标识（形如 R000001@3）。
func (s *WorkflowService) invokeRootChainForTest(ctx context.Context, project, chainID, env, traceID string, input map[string]any) (any, string, error) {
	def, err := s.rootChainRepo.GetByProjectID(ctx, project, chainID)
	if err != nil {
		return nil, "", fmt.Errorf("目标根链 %s 不存在或已停用: %w", chainID, err)
	}
	releaseID := ""
	if release, err := s.releaseRepo.GetCurrent(ctx, project, chainID); err == nil && release != nil {
		releaseID = fmt.Sprintf("%s@%d", chainID, release.Version)
	}
	output, err := s.InvokeRootChain(ctx, project, def.ChainKey, env, traceID, input, false)
	if err != nil {
		return nil, releaseID, fmt.Errorf("执行目标根链 %s 失败: %w", chainID, err)
	}
	return output, releaseID, nil
}
