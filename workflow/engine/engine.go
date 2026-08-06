package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// 本包定义的错误与仓储接口，避免 engine 反向 import 父包 workflow 造成循环引用。
var (
	// ErrEngineNotLoaded 引擎未加载
	ErrEngineNotLoaded = fmt.Errorf("workflow/engine: engine not loaded")
	// ErrExecutionFailed 流程执行失败
	ErrExecutionFailed = fmt.Errorf("workflow/engine: execution failed")
)

// RootChainStore 根规则链仓储接口（engine 包内定义，避免循环依赖）。
type RootChainStore interface {
	// GetByID 按项目+根链 ID 查询
	GetByID(ctx context.Context, project, chainID string) (*RootChainDef, error)
}

// SubChainStore 子规则链仓储接口（engine 包内定义，避免循环依赖）。
type SubChainStore interface {
	// GetByID 按项目+子链 ID 查询
	GetByID(ctx context.Context, project, chainID string) (*SubChainDef, error)
}

// RootChainDef 根链定义（engine 仅使用其字段，由仓储实现填充）。
type RootChainDef struct {
	// DSLJSON 完整的 rulego RootChain DSL JSON
	DSLJSON string
	// SubChainIDs 引用的子链 ID 列表（逗号分隔）
	SubChainIDs string
}

// SubChainDef 子链定义（engine 仅使用其字段，由仓储实现填充）。
type SubChainDef struct {
	// DSLJSON 完整的 rulego SubChain DSL JSON
	DSLJSON string
	// SubChainIDs 引用的子链 ID 列表（逗号分隔）
	SubChainIDs string
}

// WorkflowEngine 封装 rulego 规则引擎的创建和执行。
// 使用 rulego 的默认 Pool 管理引擎实例。
type WorkflowEngine struct {
	rootChainStore RootChainStore
	subChainStore  SubChainStore
	mu             sync.RWMutex
	loadedChains   map[string]bool // 追踪已加载的链，key: "project:chainID"
}

// chainKey 生成项目+链ID的复合键。
func chainKey(project, chainID string) string {
	return project + ":" + chainID
}

// NewWorkflowEngine 创建工作流引擎实例。
func NewWorkflowEngine(rootChainStore RootChainStore, subChainStore SubChainStore) *WorkflowEngine {
	return &WorkflowEngine{
		rootChainStore: rootChainStore,
		subChainStore:  subChainStore,
		loadedChains:   make(map[string]bool),
	}
}

// LoadChain 从数据库加载指定项目下的根链 DSL 并注册到 rulego 引擎池。
// 同时加载根链引用的所有子链（flow 节点通过 ruleChainId 引用子链）。
func (e *WorkflowEngine) LoadChain(ctx context.Context, project, chainID string) error {
	def, err := e.rootChainStore.GetByID(ctx, project, chainID)
	if err != nil {
		return fmt.Errorf("load chain %s/%s: %w", project, chainID, err)
	}
	return e.LoadChainDSL(ctx, project, chainID, def.DSLJSON, def.SubChainIDs)
}

// LoadChainDSL 将给定的根链 DSL 注册到 rulego 引擎池（适用于发布版本等自定义来源）。
// subChainIDs 为逗号分隔的子链 ID 列表，可为空。会递归加载子链引用的子链。
func (e *WorkflowEngine) LoadChainDSL(ctx context.Context, project, chainID, dslJSON, subChainIDs string) error {
	key := chainKey(project, chainID)

	// 先加载子链（flow 节点按 ruleChainId 从 pool 查找，递归加载嵌套子链）
	if err := e.loadSubChains(ctx, project, subChainIDs, 0); err != nil {
		return err
	}

	if _, err := rulego.New(key, []byte(dslJSON)); err != nil {
		return fmt.Errorf("load chain %s into rulego pool: %w", key, err)
	}

	e.mu.Lock()
	e.loadedChains[key] = true
	e.mu.Unlock()

	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Msg("rule chain loaded into engine pool")
	return nil
}

// loadSubChains 加载逗号分隔的子链列表到引擎池（已加载的跳过）。
// depth 用于防止子链循环引用导致的无限递归加载，达到最大深度后停止。
func (e *WorkflowEngine) loadSubChains(ctx context.Context, project, subChainIDs string, depth int) error {
	const maxSubChainDepth = 10
	if subChainIDs == "" || depth >= maxSubChainDepth {
		return nil
	}
	for _, scID := range strings.Split(subChainIDs, ",") {
		scID = strings.TrimSpace(scID)
		if scID == "" {
			continue
		}
		scKey := chainKey(project, scID)
		if _, loaded := rulego.Get(scKey); loaded {
			continue
		}
		scDef, err := e.subChainStore.GetByID(ctx, project, scID)
		if err != nil {
			return fmt.Errorf("load sub chain %s/%s: %w", project, scID, err)
		}
		if _, err := rulego.New(scKey, []byte(scDef.DSLJSON)); err != nil {
			return fmt.Errorf("load sub chain %s into rulego pool: %w", scKey, err)
		}
		log.Ctx(ctx).Info().
			Str("project", project).
			Str("chain_id", scID).
			Msg("sub chain loaded into engine pool")
		// 递归加载该子链引用的子链
		if scDef.SubChainIDs != "" {
			if err := e.loadSubChains(ctx, project, scDef.SubChainIDs, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadSubChain 从数据库加载指定项目下的子链 DSL 并注册到 rulego 引擎池。
// 同时递归加载该子链引用的所有嵌套子链。子链可独立加载后通过 ExecuteSubChain 执行。
func (e *WorkflowEngine) LoadSubChain(ctx context.Context, project, chainID string) error {
	def, err := e.subChainStore.GetByID(ctx, project, chainID)
	if err != nil {
		return fmt.Errorf("load sub chain %s/%s: %w", project, chainID, err)
	}
	return e.LoadChainDSL(ctx, project, chainID, def.DSLJSON, def.SubChainIDs)
}

// ExecuteSubChain 同步执行已加载的子链并返回结果 JSON。
// 子链需先通过 LoadSubChain 加载到引擎池（或已被其父链加载）。
func (e *WorkflowEngine) ExecuteSubChain(ctx context.Context, project, chainID string, jsonPayload string) (string, error) {
	return e.Execute(ctx, project, chainID, jsonPayload)
}

// Execute 同步执行已加载的规则链并返回结果。
func (e *WorkflowEngine) Execute(ctx context.Context, project, chainID string, jsonPayload string) (string, error) {
	key := chainKey(project, chainID)

	e.mu.RLock()
	loaded := e.loadedChains[key]
	e.mu.RUnlock()
	if !loaded {
		return "", fmt.Errorf("%w: chain %s not loaded, call LoadChain first", ErrEngineNotLoaded, key)
	}

	engine, ok := rulego.Get(key)
	if !ok {
		return "", fmt.Errorf("%w: chain %s not found in pool", ErrEngineNotLoaded, key)
	}

	msg := types.NewMsgWithJsonData(jsonPayload)

	type result struct {
		data string
		err  error
	}
	resultCh := make(chan result, 1)

	engine.OnMsgAndWait(msg,
		types.WithContext(ctx),
		types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
			if err != nil {
				resultCh <- result{err: fmt.Errorf("%w: %v", ErrExecutionFailed, err)}
				return
			}
			resultCh <- result{data: msg.GetData()}
		}),
	)

	select {
	case r := <-resultCh:
		if r.err != nil {
			log.Ctx(ctx).Error().
				Str("key", key).
				Err(r.err).
				Msg("chain execution failed")
			return "", r.err
		}
		log.Ctx(ctx).Info().
			Str("key", key).
			Int("result_size", len(r.data)).
			Msg("chain execution completed")
		return r.data, nil
	case <-ctx.Done():
		return "", fmt.Errorf("%w: context cancelled: %v", ErrExecutionFailed, ctx.Err())
	}
}

// UnloadChain 从引擎池中移除并停止指定项目下的规则链。
func (e *WorkflowEngine) UnloadChain(ctx context.Context, project, chainID string) error {
	key := chainKey(project, chainID)

	e.mu.Lock()
	delete(e.loadedChains, key)
	e.mu.Unlock()

	rulego.Del(key)

	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Msg("rule chain unloaded from engine pool")
	return nil
}

// IsLoaded 检查指定规则链是否已加载。
func (e *WorkflowEngine) IsLoaded(project, chainID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.loadedChains[chainKey(project, chainID)]
}

// Shutdown 关闭引擎，停止并卸载所有已加载的规则链。
func (e *WorkflowEngine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.loadedChains = make(map[string]bool)
	e.mu.Unlock()

	rulego.Stop()

	log.Ctx(ctx).Info().Msg("workflow engine shutdown completed")
	return nil
}
