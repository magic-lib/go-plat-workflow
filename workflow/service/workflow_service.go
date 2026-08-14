package service

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils/httputil"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	param "github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/builder"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
	"github.com/magic-lib/go-plat-workflow/workflow/repo"
)

// WorkflowService 工作流编排服务，整合节点管理、子链管理、DSL 组装和引擎执行。
// 所有操作都限定在指定的 project 内。
type WorkflowService struct {
	db                     *gorm.DB
	projectRepo            *repo.ProjectRepo
	nodeRepo               *repo.NodeRepo
	subChainRepo           *repo.SubChainRepo
	rootChainRepo          *repo.RootChainRepo
	releaseRepo            *repo.RootChainReleaseRepo
	testCaseRepo           *repo.TestCaseRepo
	envConfigRepo          *repo.EnvConfigRepo
	nodeTestRecordRepo     *repo.NodeTestRecordRepo
	activityRepo           *repo.ActivityRepo
	activityTestRecordRepo *repo.ActivityTestRecordRepo
	activityLogRepo        *repo.ActivityLogRepo
	mqExecutor             *workflow.MQExecutor
	dslBuilder             *builder.DSLBuilder
	engine                 *engine.WorkflowEngine
}

// NewWorkflowService 创建工作流服务实例，自动建表。
func NewWorkflowService(db *gorm.DB) (*WorkflowService, error) {
	// 自动迁移表结构
	if err := db.AutoMigrate(
		&models.ProjectModel{},
		&models.NodeModel{},
		&models.SubChainModel{},
		&models.RootChainModel{},
		&models.RootChainReleaseModel{},
		&models.TestCaseModel{},
		&models.EnvConfigModel{},
		&models.NodeTestRecordModel{},
		&models.ActivityModel{},
		&models.ActivityTestRecordModel{},
		&models.ActivityLogModel{},
	); err != nil {
		return nil, err
	}

	// AutoMigrate 不会修改已存在列的类型，显式将 result 调整为 text。
	// MySQL 5.7 下 TEXT 列不能设默认值也不能直接 NOT NULL（已有 NULL 行会报错），故仅改类型。
	if err := db.Exec(
		"ALTER TABLE wf_activity_logs MODIFY COLUMN result text",
	).Error; err != nil {
		return nil, err
	}

	// 旧数据回填：将 chain_key 为空（'' 或 NULL）的根链按 id 生成唯一业务键，
	// 避免后续建唯一索引时因重复空值失败。基于 id 保证全局唯一且幂等。
	if err := db.Exec(
		"UPDATE wf_root_chains SET chain_key = CONCAT('R', LPAD(id, 6, '0')) WHERE chain_key IS NULL OR chain_key = ''",
	).Error; err != nil {
		return nil, err
	}

	// AutoMigrate 已自动新增 chain_key 列，这里补建 project + chain_key 联合唯一索引
	//（幂等：已存在则忽略报错）。
	if err := db.Exec(
		"ALTER TABLE wf_root_chains ADD UNIQUE INDEX uk_project_chain_key (project, chain_key)",
	).Error; err != nil && !strings.Contains(err.Error(), "Duplicate") && !strings.Contains(err.Error(), "Duplicate key name") {
		return nil, err
	}

	projectRepo := repo.NewProjectRepo(db)
	nodeRepo := repo.NewNodeRepo(db)
	subChainRepo := repo.NewSubChainRepo(db)
	rootChainRepo := repo.NewRootChainRepo(db)
	releaseRepo := repo.NewRootChainReleaseRepo(db)
	testCaseRepo := repo.NewTestCaseRepo(db)
	envConfigRepo := repo.NewEnvConfigRepo(db)
	nodeTestRecordRepo := repo.NewNodeTestRecordRepo(db)
	activityRepo := repo.NewActivityRepo(db)
	activityTestRecordRepo := repo.NewActivityTestRecordRepo(db)
	activityLogRepo := repo.NewActivityLogRepo(db)

	s := &WorkflowService{
		db:                     db,
		projectRepo:            projectRepo,
		nodeRepo:               nodeRepo,
		subChainRepo:           subChainRepo,
		rootChainRepo:          rootChainRepo,
		releaseRepo:            releaseRepo,
		testCaseRepo:           testCaseRepo,
		envConfigRepo:          envConfigRepo,
		nodeTestRecordRepo:     nodeTestRecordRepo,
		activityRepo:           activityRepo,
		activityTestRecordRepo: activityTestRecordRepo,
		activityLogRepo:        activityLogRepo,
		mqExecutor:             workflow.NewMQExecutorWithLogAndEnv(activityLogRepo, envConfigRepo),
		dslBuilder:             builder.NewDSLBuilder(nodeRepo, subChainRepo, rootChainRepo),
		engine:                 engine.NewWorkflowEngine(workflow.NewEngineRootChainStore(rootChainRepo), workflow.NewEngineSubChainStore(subChainRepo)),
	}

	log.Info().Msg("WorkflowService initialized, tables migrated")
	return s, nil
}

// ============================================================
// Project 管理
// ============================================================

// CreateProject 创建项目。
func (s *WorkflowService) CreateProject(ctx context.Context, def *workflow.ProjectDef) error {
	return s.projectRepo.Create(ctx, def)
}

// GetProject 获取项目详情。
func (s *WorkflowService) GetProject(ctx context.Context, project string) (*workflow.ProjectDef, error) {
	return s.projectRepo.GetByID(ctx, project)
}

// ListProjects 列出所有可用项目。
func (s *WorkflowService) ListProjects(ctx context.Context) ([]*workflow.ProjectDef, error) {
	return s.projectRepo.List(ctx)
}

// UpdateProject 更新项目。
func (s *WorkflowService) UpdateProject(ctx context.Context, def *workflow.ProjectDef) error {
	return s.projectRepo.Update(ctx, def)
}

// DeleteProject 软删除项目。
func (s *WorkflowService) DeleteProject(ctx context.Context, project string) error {
	return s.projectRepo.Delete(ctx, project)
}

// GetProjectConfig 对外配置查询：根据项目密钥鉴权后，
// 返回项目下的环境配置信息，以及可执行的 RootChains 概要列表（不含 DSL 等敏感内容）。
// 密钥不匹配时返回错误。
func (s *WorkflowService) GetProjectConfig(ctx context.Context, project, secretKey string) (*workflow.ProjectConfigResponse, error) {
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("secret_key is required")
	}
	stored, err := s.projectRepo.GetSecret(ctx, project)
	if err != nil {
		return nil, err
	}
	if stored == "" {
		return nil, fmt.Errorf("project has no secret_key configured, please set it first")
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(secretKey)) != 1 {
		return nil, fmt.Errorf("secret_key mismatch")
	}

	// 项目基本信息
	proj, err := s.projectRepo.GetByID(ctx, project)
	if err != nil {
		return nil, err
	}

	// 环境配置
	envConfigs, err := s.envConfigRepo.ListByProject(ctx, project)
	if err != nil {
		return nil, err
	}

	// RootChains 概要（仅 chain_id / name / description）
	chains, err := s.rootChainRepo.List(ctx, project)
	if err != nil {
		return nil, err
	}
	summaries := make([]*workflow.ProjectConfigSummary, 0, len(chains))
	for _, c := range chains {
		summaries = append(summaries, &workflow.ProjectConfigSummary{
			ChainID:     c.ChainID,
			Name:        c.Name,
			Description: c.Description,
		})
	}

	if envConfigs == nil {
		envConfigs = []*workflow.EnvConfigDef{}
	}
	return &workflow.ProjectConfigResponse{
		Project:     proj.Project,
		Name:        proj.Name,
		Description: proj.Description,
		EnvConfigs:  envConfigs,
		RootChains:  summaries,
	}, nil
}

// ============================================================
// Node 管理
// ============================================================

// RegisterNode 注册节点到数据库。
func (s *WorkflowService) RegisterNode(ctx context.Context, def *workflow.NodeDef) error {
	return s.nodeRepo.Create(ctx, def)
}

// GenerateNodeID 生成下一个节点的自动 ID（如 N000005）。
func (s *WorkflowService) GenerateNodeID(ctx context.Context) (string, error) {
	return s.nodeRepo.NextNodeID(ctx)
}

// BatchRegisterNodes 批量注册节点（upsert：project+node_id 冲突则更新，否则插入）。
func (s *WorkflowService) BatchRegisterNodes(ctx context.Context, defs []*workflow.NodeDef) error {
	return s.nodeRepo.BatchUpsert(ctx, defs)
}

// GetNode 获取指定项目下的单个节点。
func (s *WorkflowService) GetNode(ctx context.Context, project, nodeID string) (*workflow.NodeDef, error) {
	return s.nodeRepo.GetByID(ctx, project, nodeID)
}

// ListNodes 列出指定项目下所有可用节点。
func (s *WorkflowService) ListNodes(ctx context.Context, project string) ([]*workflow.NodeDef, error) {
	return s.nodeRepo.List(ctx, project)
}

// UpdateNode 更新节点配置。
func (s *WorkflowService) UpdateNode(ctx context.Context, def *workflow.NodeDef) error {
	return s.nodeRepo.Update(ctx, def)
}

// DeleteNode 软删除节点。
func (s *WorkflowService) DeleteNode(ctx context.Context, project, nodeID string) error {
	return s.nodeRepo.Delete(ctx, project, nodeID)
}

// ============================================================
// SubChain 管理
// ============================================================

// RegisterSubChain 注册子链到数据库。
func (s *WorkflowService) RegisterSubChain(ctx context.Context, def *workflow.SubChainDef) error {
	return s.subChainRepo.Create(ctx, def)
}

// GenerateSubChainID 生成下一个子链的自动 ID（如 F000012）。
func (s *WorkflowService) GenerateSubChainID(ctx context.Context) (string, error) {
	return s.subChainRepo.NextSubChainID(ctx)
}

// BatchRegisterSubChains 批量注册子链（upsert：project+chain_id 冲突则更新，否则插入）。
func (s *WorkflowService) BatchRegisterSubChains(ctx context.Context, defs []*workflow.SubChainDef) error {
	return s.subChainRepo.BatchUpsert(ctx, defs)
}

// GetSubChain 获取指定项目下的单个子链。
func (s *WorkflowService) GetSubChain(ctx context.Context, project, chainID string) (*workflow.SubChainDef, error) {
	return s.subChainRepo.GetByID(ctx, project, chainID)
}

// ListSubChains 列出指定项目下所有可用子链。
func (s *WorkflowService) ListSubChains(ctx context.Context, project string) ([]*workflow.SubChainDef, error) {
	return s.subChainRepo.List(ctx, project)
}

// UpdateSubChain 更新子链配置。
func (s *WorkflowService) UpdateSubChain(ctx context.Context, def *workflow.SubChainDef) error {
	return s.subChainRepo.Update(ctx, def)
}

// DeleteSubChain 软删除子链。
func (s *WorkflowService) DeleteSubChain(ctx context.Context, project, chainID string) error {
	return s.subChainRepo.Delete(ctx, project, chainID)
}

// CreateSubChainBuild 编排方式创建子链：ChainID 为空时自动生成（如 F000012）。
func (s *WorkflowService) CreateSubChainBuild(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	if req.ChainID == "" {
		nextID, err := s.subChainRepo.NextSubChainID(ctx)
		if err != nil {
			return nil, err
		}
		req.ChainID = nextID
	}
	return s.dslBuilder.BuildSubChain(ctx, req)
}

// UpdateSubChainBuild 编排方式更新子链 DSL（保留原 ChainID）。
func (s *WorkflowService) UpdateSubChainBuild(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	if req.ChainID == "" {
		return nil, fmt.Errorf("chain_id is required")
	}
	def, err := s.dslBuilder.AssembleSubChain(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.subChainRepo.Update(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// ============================================================
// RootChain 构建与管理
// ============================================================

// ensureRootChainIDs 保证 req 的 ChainID 与 ChainKey 已就绪：
//   - ChainID 为空时基于自增主键生成 R000001 格式（若提供了 ChainKey 且已存在记录，则复用其 ChainID 保持幂等）
//   - ChainKey 为用户自定义的业务键；不传时默认等于 ChainID（保证全局唯一、可读）
func (s *WorkflowService) ensureRootChainIDs(ctx context.Context, req *workflow.BuildRequest) error {
	if req.ChainID == "" && req.ChainKey != "" {
		if exist, err := s.rootChainRepo.GetByKey(ctx, req.Project, req.ChainKey); err == nil && exist != nil {
			req.ChainID = exist.ChainID
		}
	}
	if req.ChainID == "" {
		next, err := s.rootChainRepo.NextRootChainID(ctx)
		if err != nil {
			return err
		}
		req.ChainID = next
	}
	if req.ChainKey == "" {
		req.ChainKey = req.ChainID
	}
	return nil
}

// BuildRootChain 根据 BuildRequest 组装 RootChainDSL 并存入数据库。
// ChainID 为空时自动生成 R000001 格式；ChainKey 为空时自动生成全局唯一业务键。
func (s *WorkflowService) BuildRootChain(ctx context.Context, req *workflow.BuildRequest) (*workflow.RootChainDef, error) {
	if err := s.ensureRootChainIDs(ctx, req); err != nil {
		return nil, err
	}
	return s.dslBuilder.Build(ctx, req)
}

// SaveRootChain 保存根链草稿（先物理删除旧记录再重建，幂等操作）。
// 仅影响测试环境草稿，已发布的版本快照不受影响。
func (s *WorkflowService) SaveRootChain(ctx context.Context, req *workflow.BuildRequest) (*workflow.RootChainDef, error) {
	if err := s.ensureRootChainIDs(ctx, req); err != nil {
		return nil, err
	}
	// 先删除旧记录（忽略不存在的情况）
	_ = s.rootChainRepo.Delete(ctx, req.Project, req.ChainID)
	// 重新构建并创建
	return s.dslBuilder.Build(ctx, req)
}

// GetRootChain 获取指定项目下的单个根链（按 ChainID）。
func (s *WorkflowService) GetRootChain(ctx context.Context, project, chainID string) (*workflow.RootChainDef, error) {
	return s.rootChainRepo.GetByID(ctx, project, chainID)
}

// GetRootChainByKey 按项目+ChainKey 获取根链（project 与 chain_key 联合唯一，方便用业务键直接调用主链）。
func (s *WorkflowService) GetRootChainByKey(ctx context.Context, project, chainKey string) (*workflow.RootChainDef, error) {
	return s.rootChainRepo.GetByKey(ctx, project, chainKey)
}

// ListRootChains 列出指定项目下所有可用根链。
func (s *WorkflowService) ListRootChains(ctx context.Context, project string) ([]*workflow.RootChainDef, error) {
	return s.rootChainRepo.List(ctx, project)
}

// DeleteRootChain 物理删除根链草稿（发布历史记录保留，不受影响）。
func (s *WorkflowService) DeleteRootChain(ctx context.Context, project, chainID string) error {
	return s.rootChainRepo.Delete(ctx, project, chainID)
}

// ============================================================
// TestCase 管理
// ============================================================

// SaveTestCase 保存测试用例（创建或更新）。
// 若 def.CaseID 为空则自动生成；否则按 CaseID 更新。
// 执行后的结果快照通过 def.LastResult 写入（可选）。
func (s *WorkflowService) SaveTestCase(ctx context.Context, def *workflow.TestCaseDef) (*workflow.TestCaseDef, error) {
	if def.CaseID == "" {
		caseID, err := s.testCaseRepo.NextCaseID(ctx, def.Project)
		if err != nil {
			return nil, err
		}
		def.CaseID = caseID
		if err := s.testCaseRepo.Create(ctx, def); err != nil {
			return nil, err
		}
	} else {
		if err := s.testCaseRepo.Update(ctx, def); err != nil {
			return nil, err
		}
	}
	return def, nil
}

// GetTestCase 按项目 + CaseID 查询测试用例。
func (s *WorkflowService) GetTestCase(ctx context.Context, project, caseID string) (*workflow.TestCaseDef, error) {
	return s.testCaseRepo.GetByID(ctx, project, caseID)
}

// ListTestCases 列出指定 owner（root/sub 的 chainID）下所有测试用例。
func (s *WorkflowService) ListTestCases(ctx context.Context, project, ownerID string) ([]*workflow.TestCaseDef, error) {
	return s.testCaseRepo.ListByOwner(ctx, project, ownerID)
}

// DeleteTestCase 删除测试用例。
func (s *WorkflowService) DeleteTestCase(ctx context.Context, project, caseID string) error {
	return s.testCaseRepo.Delete(ctx, project, caseID)
}

// ============================================================
// EnvConfig 管理（项目级环境配置：环境变量 / Redis / MySQL）
// ============================================================

// SaveEnvConfig 保存环境配置（创建或更新，按 project + env_name 定位）。
func (s *WorkflowService) SaveEnvConfig(ctx context.Context, def *workflow.EnvConfigDef) (*workflow.EnvConfigDef, error) {
	if def.Project == "" || def.EnvName == "" {
		return nil, fmt.Errorf("project and env_name are required")
	}
	if err := s.envConfigRepo.Upsert(ctx, def); err != nil {
		return nil, err
	}
	return s.envConfigRepo.GetByName(ctx, def.Project, def.EnvName)
}

// GetEnvConfig 按项目 + 环境名查询环境配置。
func (s *WorkflowService) GetEnvConfig(ctx context.Context, project, envName string) (*workflow.EnvConfigDef, error) {
	return s.envConfigRepo.GetByName(ctx, project, envName)
}

// ListEnvConfigs 列出指定项目下所有环境配置。
func (s *WorkflowService) ListEnvConfigs(ctx context.Context, project string) ([]*workflow.EnvConfigDef, error) {
	return s.envConfigRepo.ListByProject(ctx, project)
}

// ListAllEnvConfigs 列出系统中所有项目下的全部环境配置，
// 供活动日志/心跳收集器自动发现各环境 Redis 并监听。
func (s *WorkflowService) ListAllEnvConfigs(ctx context.Context) ([]*workflow.EnvConfigDef, error) {
	return s.envConfigRepo.ListAll(ctx)
}

// EnvConfigRepo 返回环境配置仓储实例（供 web 层收集器发现 Redis 配置复用）。
func (s *WorkflowService) EnvConfigRepo() *repo.EnvConfigRepo {
	return s.envConfigRepo
}

// DeleteEnvConfig 删除环境配置。
func (s *WorkflowService) DeleteEnvConfig(ctx context.Context, project, envName string) error {
	return s.envConfigRepo.Delete(ctx, project, envName)
}

// ============================================================
// RootChain 发布与回滚
// ============================================================

// PublishRootChain 发布根链：将当前草稿快照为新版本，并设为生产环境当前版本。
func (s *WorkflowService) PublishRootChain(ctx context.Context, project, chainID string) (*workflow.RootChainReleaseDef, error) {
	draft, err := s.rootChainRepo.GetByID(ctx, project, chainID)
	if err != nil {
		return nil, err
	}
	maxVer, err := s.releaseRepo.MaxVersion(ctx, project, chainID)
	if err != nil {
		return nil, err
	}
	release := &workflow.RootChainReleaseDef{
		Project:            draft.Project,
		ChainID:            draft.ChainID,
		Version:            maxVer + 1,
		Name:               draft.Name,
		Description:        draft.Description,
		DSLJSON:            draft.DSLJSON,
		NodeIDs:            draft.NodeIDs,
		SubChainIDs:        draft.SubChainIDs,
		ConnectionsData:    draft.ConnectionsData,
		NodeParamOverrides: draft.NodeParamOverrides,
		IsCurrent:          true,
		PublishedAt:        time.Now(),
	}
	if err := s.releaseRepo.Create(ctx, release); err != nil {
		return nil, err
	}
	// 新版本设为生产当前版本（同事务清除旧版本标记）
	if err := s.releaseRepo.SetCurrent(ctx, project, chainID, release.Version); err != nil {
		return nil, err
	}
	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Int("version", release.Version).
		Msg("root chain published")
	return release, nil
}

// ListRootChainReleases 列出根链的发布历史（版本号倒序）。
func (s *WorkflowService) ListRootChainReleases(ctx context.Context, project, chainID string) ([]*workflow.RootChainReleaseDef, error) {
	return s.releaseRepo.ListByChain(ctx, project, chainID)
}

// GetCurrentRelease 获取根链当前生产版本。
func (s *WorkflowService) GetCurrentRelease(ctx context.Context, project, chainID string) (*workflow.RootChainReleaseDef, error) {
	return s.releaseRepo.GetCurrent(ctx, project, chainID)
}

// ListCurrentReleases 列出项目下所有根链的当前生产版本。
func (s *WorkflowService) ListCurrentReleases(ctx context.Context, project string) ([]*workflow.RootChainReleaseDef, error) {
	return s.releaseRepo.ListCurrentByProject(ctx, project)
}

// RollbackRootChain 回滚：将历史发布版本设为生产环境当前版本。
func (s *WorkflowService) RollbackRootChain(ctx context.Context, project, chainID string, version int) (*workflow.RootChainReleaseDef, error) {
	if err := s.releaseRepo.SetCurrent(ctx, project, chainID, version); err != nil {
		return nil, err
	}
	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Int("version", version).
		Msg("root chain rolled back")
	return s.releaseRepo.GetByVersion(ctx, project, chainID, version)
}

// SetCurrentRelease 将指定发布版本设为生产环境当前生效版本（即切换线上生效状态）。
// 切换后尝试将该版本的 DSL 快照重新加载到 rulego 引擎池，确保线上配置尽快生效（覆盖同 key 的旧 chain）。
// 注意：数据库层面的 is_current 切换保证成功；若引擎重载因 DSL 含未注册组件（如非法的 condition 类型）而失败，
// 仅记录告警并不阻断操作，避免脏数据卡死"设为生效"流程（下次正常执行仍会按需加载/重载）。
func (s *WorkflowService) SetCurrentRelease(ctx context.Context, project, chainID string, version int) (*workflow.RootChainReleaseDef, error) {
	if err := s.releaseRepo.SetCurrent(ctx, project, chainID, version); err != nil {
		return nil, err
	}
	release, err := s.releaseRepo.GetByVersion(ctx, project, chainID, version)
	if err != nil {
		return nil, err
	}
	// 立即重载到引擎：先卸载旧 chain，再用新版本 DSL 加载（含递归子链）
	_ = s.engine.UnloadChain(ctx, project, chainID)
	if err := s.engine.LoadChainDSL(ctx, project, chainID, release.DSLJSON, release.SubChainIDs); err != nil {
		log.Ctx(ctx).Warn().
			Err(err).
			Str("project", project).
			Str("chain_id", chainID).
			Int("version", version).
			Msg("set current ok, but reload chain into engine failed (DSL may contain unregistered component)")
		return release, nil
	}
	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Int("version", version).
		Msg("root chain current release set and reloaded into engine")
	return release, nil
}

// DeleteRootChainRelease 删除指定发布版本（当前生效版本不允许删除）。
func (s *WorkflowService) DeleteRootChainRelease(ctx context.Context, project, chainID string, version int) error {
	if err := s.releaseRepo.DeleteByVersion(ctx, project, chainID, version); err != nil {
		return err
	}
	log.Ctx(ctx).Info().
		Str("project", project).
		Str("chain_id", chainID).
		Int("version", version).
		Msg("root chain release deleted")
	return nil
}

// ExecutePublishedRootChain 加载并执行生产环境当前发布版本。
// 与草稿执行互不影响：使用发布时快照的 DSL。
func (s *WorkflowService) ExecutePublishedRootChain(ctx context.Context, project, chainID, jsonPayload string) (string, error) {
	release, err := s.releaseRepo.GetCurrent(ctx, project, chainID)
	if err != nil {
		return "", err
	}
	if err := s.engine.LoadChainDSL(ctx, project, chainID, release.DSLJSON, release.SubChainIDs); err != nil {
		return "", err
	}
	return s.engine.Execute(ctx, project, chainID, jsonPayload)
}

// ============================================================
// 引擎执行
// ============================================================

// LoadChain 加载根链到 rulego 引擎池。
func (s *WorkflowService) LoadChain(ctx context.Context, project, chainID string) error {
	return s.engine.LoadChain(ctx, project, chainID)
}

// LoadSubChain 加载子链到 rulego 引擎池（递归加载其嵌套子链），可独立执行。
func (s *WorkflowService) LoadSubChain(ctx context.Context, project, chainID string) error {
	return s.engine.LoadSubChain(ctx, project, chainID)
}

// ExecuteRootChain 同步执行已加载的根链并返回结果 JSON。
func (s *WorkflowService) ExecuteRootChain(ctx context.Context, project, chainID string, jsonPayload string) (string, error) {
	return s.engine.Execute(ctx, project, chainID, jsonPayload)
}

// ExecuteSubChain 同步执行已加载的子链并返回结果 JSON（子链可独立运行）。
func (s *WorkflowService) ExecuteSubChain(ctx context.Context, project, chainID string, jsonPayload string) (string, error) {
	return s.engine.ExecuteSubChain(ctx, project, chainID, jsonPayload)
}

// LoadAndExecuteSubChain 一步完成：加载子链（含嵌套子链）到引擎池并执行。
func (s *WorkflowService) LoadAndExecuteSubChain(ctx context.Context, project, chainID, jsonPayload string) (string, error) {
	if err := s.engine.LoadSubChain(ctx, project, chainID); err != nil {
		return "", err
	}
	return s.engine.ExecuteSubChain(ctx, project, chainID, jsonPayload)
}

// UnloadChain 从引擎池中卸载根链。
func (s *WorkflowService) UnloadChain(ctx context.Context, project, chainID string) error {
	return s.engine.UnloadChain(ctx, project, chainID)
}

// ============================================================
// 便捷方法：一步完成 构建→加载→执行
// ============================================================

// BuildLoadAndExecute 一次性完成：组装 DSL → 加载到引擎池 → 执行流程。
func (s *WorkflowService) BuildLoadAndExecute(ctx context.Context, req *workflow.BuildRequest, jsonPayload string) (string, error) {
	// 1. 组装
	def, err := s.dslBuilder.Build(ctx, req)
	if err != nil {
		return "", err
	}

	// 2. 加载
	if err := s.engine.LoadChain(ctx, def.Project, def.ChainID); err != nil {
		return "", err
	}

	// 3. 执行
	return s.engine.Execute(ctx, def.Project, def.ChainID, jsonPayload)
}

// ============================================================
// 单节点测试（MQ 分布式执行）
// ============================================================

// TestNodeRequest 测试单个节点的请求参数。
type TestNodeRequest struct {
	// Project 所属项目
	Project string `json:"project"`
	// NodeID 被测节点 ID
	NodeID string `json:"node_id"`
	// EnvName 测试使用的环境名（决定 Redis 等依赖配置）
	EnvName string `json:"env_name"`
	// InputParams 测试传入的参数（key=参数名，value=参数值）
	InputParams map[string]interface{} `json:"input_params"`
	// SaveRecord 是否保存测试记录（默认 true）
	SaveRecord bool `json:"save_record"`
}

// TestNodeResult 测试单个节点返回结果。
type TestNodeResult struct {
	// Status success / fail
	Status string `json:"status"`
	// Result worker 返回的数据（转为 JSON 字符串）
	Result string `json:"result,omitempty"`
	// ErrorMsg 错误信息
	ErrorMsg string `json:"error_msg,omitempty"`
	// RecordID 保存的测试记录 ID（若 SaveRecord=true）
	RecordID string `json:"record_id,omitempty"`
}

// TestNode 测试单个节点：
// 1. 校验参数覆盖类型为 frontend / frontend+ 的必传参数是否已提供
// 2. 通过 MQ 同步调用分布式 worker 执行单个节点
// 3. 保存测试记录（可选）
func (s *WorkflowService) TestNode(ctx context.Context, req *TestNodeRequest) (*TestNodeResult, error) {
	if req.Project == "" || req.NodeID == "" {
		return nil, fmt.Errorf("project and node_id are required")
	}

	// 1. 查询节点
	nodeDef, err := s.nodeRepo.GetByID(ctx, req.Project, req.NodeID)
	if err != nil {
		return nil, err
	}

	// 2. 校验必传参数（frontend / frontend+ 策略）
	if err := validateRequiredParams(nodeDef, req.InputParams); err != nil {
		return nil, err
	}

	// 4. 构建环境变量（可选）
	envVars := make(map[string]string)
	if req.EnvName != "" {
		if envDef, e := s.envConfigRepo.GetByName(ctx, nodeDef.Project, req.EnvName); e == nil && envDef != nil {
			for _, v := range envDef.EnvVars {
				envVars[v.Key] = v.Value
			}
		}
	}

	// 5. 通过 MQ 调用分布式 worker 执行
	payload := &workflow.TestNodePayload{
		NodeID:      req.NodeID,
		Env:         req.EnvName,
		NodeDef:     nodeDef,
		InputParams: req.InputParams,
	}
	resp, err := s.mqExecutor.TestNode(ctx, payload)

	// 6. 整理结果
	result := &TestNodeResult{}
	if err != nil {
		result.Status = "fail"
		result.ErrorMsg = err.Error()
	} else {
		result.Status = "success"
		result.Result = extractResultData(resp)
	}

	// 7. 保存测试记录（除非显式关闭）
	if req.SaveRecord {
		record := &workflow.NodeTestRecordDef{
			Project:     req.Project,
			RecordID:    "", // 由 repo 自动生成
			NodeID:      req.NodeID,
			NodeName:    nodeDef.Name,
			EnvName:     req.EnvName,
			InputParams: mustJSON(req.InputParams),
			EnvVars:     mustJSON(envVars),
			Status:      result.Status,
			Result:      result.Result,
			ErrorMsg:    result.ErrorMsg,
		}
		if err := s.CreateNodeTestRecord(ctx, record); err == nil {
			result.RecordID = record.RecordID
		}
	}

	return result, nil
}

// validateRequiredParams 校验 frontend / frontend+ 策略参数是否必传且非空。
func validateRequiredParams(nodeDef *workflow.NodeDef, input map[string]interface{}) error {
	if len(nodeDef.Params) == 0 {
		return nil
	}
	var bindConfigs []param.BindConfig
	if err := json.Unmarshal(nodeDef.Params, &bindConfigs); err != nil {
		return nil // 参数定义非法不阻断测试
	}
	for _, bc := range bindConfigs {
		policy := string(bc.Policy)
		if policy != string(param.KeyPolicyFrontendOnly) && policy != string(param.KeyPolicyFrontendPriority) {
			continue
		}
		v, ok := input[bc.Key]
		if !ok || v == nil {
			return fmt.Errorf("param %q is required (policy: %s), but not provided", bc.Key, policy)
		}
		// frontend（frontend only）要求值非空；frontend+ 允许零值以外的任意值（已提供即合法）
		if policy == string(param.KeyPolicyFrontendOnly) {
			if isEmptyValue(v) {
				return fmt.Errorf("param %q is required (policy: frontend) and must not be empty", bc.Key)
			}
		}
	}
	return nil
}

func isEmptyValue(v interface{}) bool {
	switch val := v.(type) {
	case string:
		return val == ""
	case nil:
		return true
	case bool:
		return false
	case int, int32, int64:
		return val == 0
	case float32, float64:
		return val == 0
	case map[string]interface{}:
		return len(val) == 0
	case []interface{}:
		return len(val) == 0
	default:
		return false
	}
}

// getRedisConfig 从环境配置中获取 Redis 配置；若未配置环境则返回错误。
func (s *WorkflowService) getRedisConfig(ctx context.Context, project, envName string) (*workflow.RedisConfig, error) {
	if envName == "" {
		return nil, fmt.Errorf("env_name is required to resolve redis config for MQ execution")
	}
	envDef, err := s.envConfigRepo.GetByName(ctx, project, envName)
	if err != nil {
		return nil, fmt.Errorf("env config %q not found: %w", envName, err)
	}
	if envDef.RedisConfig == nil || envDef.RedisConfig.Addr == "" {
		return nil, fmt.Errorf("env config %q has no redis config (addr is empty)", envName)
	}
	return envDef.RedisConfig, nil
}

// extractResultData 从 MQ 响应中提取 Data 字段并序列化为 JSON 字符串。
func extractResultData(resp any) string {
	if resp == nil {
		return ""
	}
	// mq.Response 结构：{error_msg, event, data}
	if r, ok := resp.(*httputil.CommResponse); ok {
		if r.Data != nil {
			return conv.String(r.Data)
		}
		return conv.String(r)
	}
	return conv.String(resp)
}

func mustJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ============================================================
// 单节点测试记录 CRUD
// ============================================================

// CreateNodeTestRecord 创建单节点测试记录（自动生成 RecordID）。
func (s *WorkflowService) CreateNodeTestRecord(ctx context.Context, def *workflow.NodeTestRecordDef) error {
	if def.RecordID == "" {
		id, err := s.nodeTestRecordRepo.NextRecordID(ctx, def.Project)
		if err != nil {
			return err
		}
		def.RecordID = id
	}
	return s.nodeTestRecordRepo.Create(ctx, def)
}

// GetNodeTestRecord 按项目 + 记录 ID 查询。
func (s *WorkflowService) GetNodeTestRecord(ctx context.Context, project, recordID string) (*workflow.NodeTestRecordDef, error) {
	return s.nodeTestRecordRepo.GetByID(ctx, project, recordID)
}

// ListNodeTestRecords 列出指定节点下所有测试记录（按时间倒序）。
func (s *WorkflowService) ListNodeTestRecords(ctx context.Context, project, nodeID string) ([]*workflow.NodeTestRecordDef, error) {
	records, err := s.nodeTestRecordRepo.ListByNode(ctx, project, nodeID)
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []*workflow.NodeTestRecordDef{}
	}
	return records, nil
}

// DeleteNodeTestRecord 删除测试记录。
func (s *WorkflowService) DeleteNodeTestRecord(ctx context.Context, project, recordID string) error {
	return s.nodeTestRecordRepo.Delete(ctx, project, recordID)
}

// ClearNodeTestRecords 删除指定节点下的全部测试记录，返回删除条数。
func (s *WorkflowService) ClearNodeTestRecords(ctx context.Context, project, nodeID string) (int64, error) {
	return s.nodeTestRecordRepo.DeleteByNode(ctx, project, nodeID)
}

// ============================================================
// Activity 测试（MQ 分布式执行）
// ============================================================

// TestActivityRequest 测试单个 activity 的请求参数。
type TestActivityRequest struct {
	// Project 所属项目
	Project string `json:"project"`
	// ActivityID 被测 activity ID
	ActivityID string `json:"activity_id"`
	// EnvName 测试使用的环境名（决定 Redis 等依赖配置）
	EnvName string `json:"env_name"`
	// InputParams 测试传入的参数（key=参数名，value=参数值）
	InputParams map[string]interface{} `json:"input_params"`
	// SaveRecord 是否保存测试记录（默认 true）
	SaveRecord bool `json:"save_record"`
}

// TestActivityResult 测试单个 activity 返回结果。
type TestActivityResult struct {
	// Status success / fail
	Status string `json:"status"`
	// Result worker 返回的数据（转为 JSON 字符串）
	Result string `json:"result,omitempty"`
	// ErrorMsg 错误信息
	ErrorMsg string `json:"error_msg,omitempty"`
	// RecordID 保存的测试记录 ID（若 SaveRecord=true）
	RecordID string `json:"record_id,omitempty"`
}

// TestActivity 测试单个 activity：
// 1. 校验参数覆盖类型为 frontend / frontend+ 的必传参数是否已提供
// 2. 通过 MQ 同步调用分布式 worker 执行该 activity
// 3. 保存测试记录（可选）
func (s *WorkflowService) TestActivity(ctx context.Context, req *TestActivityRequest) (*TestActivityResult, error) {
	if req.Project == "" || req.ActivityID == "" {
		return nil, fmt.Errorf("project and activity_id are required")
	}

	// 1. 查询 activity 模板
	actDef, err := s.activityRepo.GetByID(ctx, req.Project, req.ActivityID)
	if err != nil {
		return nil, err
	}

	// 2. 校验必传参数（frontend / frontend+ 策略）
	if err := validateRequiredActivityParams(actDef, req.InputParams); err != nil {
		return nil, err
	}

	// 3. 根据 activity 的大类型（Kind）选择不同的访问方式
	kind := actDef.Kind
	if kind == "" {
		kind = workflow.ActivityKindRedis
	}

	if kind == workflow.ActivityKindHTTP {
		return s.testHTTPActivity(ctx, req, actDef)
	}

	// ---- 以下为默认的 redis 类型：通过依赖 Redis 的 MQ 远程监听方式访问 ----

	// 查询环境配置（用于构建 Redis 连接）
	redisCfg, err := s.getRedisConfig(ctx, req.Project, req.EnvName)
	if err != nil {
		return nil, err
	}

	worker, err := s.mqExecutor.BuildWorker(req.EnvName, actDef.Project, redisCfg)
	if err != nil {
		return nil, err
	}

	// 5. 通过 MQ 同步调用远程监听程序执行该 activity：
	//    - 命名空间/活动名（act_namespace/act_name）从 activity 配置获取
	//    - 测试参数（InputParams）来自前端传入
	//    - topic 为 activity/{actNamespace}/{actName}，与远程 worker 端 SubscribeActivity 订阅一致
	traceId := id.NewUUID()
	spanId := req.ActivityID
	resp, err := s.mqExecutor.RequestActivity(ctx, worker, actDef, req.InputParams, &workflow.ActivityLogValue{
		RootChainID: "TestActivity",
		TraceID:     traceId,
		SpanID:      spanId,
		Attributes: map[string]any{
			"activity_id":        req.ActivityID,
			"project":            req.Project,
			"env_name":           req.EnvName,
			"activity_type":      kind,
			"activity_name":      actDef.Name,
			"activity_namespace": actDef.ActNamespace,
			"trace_id":           traceId,
			"span_id":            spanId,
		},
	})

	// 6. 整理结果
	result := &TestActivityResult{}
	if err != nil {
		result.Status = "fail"
		result.ErrorMsg = err.Error()
	} else {
		result.Status = "success"
		result.Result = extractResultData(resp)
	}

	// 7. 保存测试记录（除非显式关闭）
	s.saveActivityTestRecord(ctx, req, actDef, result)

	return result, nil
}

// testHTTPActivity 以 HTTP 直连方式测试 activity：
// 根据 ActivityDef.HTTPConfig 构建请求（method/url/headers/body 模板），
// 用测试入参与环境变量替换 {{key}} 占位符后发起 HTTP 请求。
func (s *WorkflowService) testHTTPActivity(ctx context.Context, req *TestActivityRequest, actDef *workflow.ActivityDef) (*TestActivityResult, error) {
	var httpCfg workflow.ActivityHTTPConfig
	if actDef.HTTPConfig != "" {
		if err := json.Unmarshal([]byte(actDef.HTTPConfig), &httpCfg); err != nil {
			return nil, fmt.Errorf("http_config 解析失败: %w", err)
		}
	}
	if httpCfg.URL == "" {
		return nil, fmt.Errorf("HTTP 类型 activity 缺少 url 配置")
	}
	method := strings.ToUpper(httpCfg.Method)
	if method == "" {
		method = http.MethodPost
	}

	// 收集环境变量（用于占位符替换）
	envVars := make(map[string]string)
	if req.EnvName != "" {
		if envDef, e := s.envConfigRepo.GetByName(ctx, req.Project, req.EnvName); e == nil && envDef != nil {
			for _, v := range envDef.EnvVars {
				envVars[v.Key] = v.Value
			}
		}
	}
	// 合并入参（入参优先级高于环境变量）
	replace := map[string]string{}
	for k, v := range envVars {
		replace[k] = v
	}
	for k, v := range req.InputParams {
		replace[k] = fmt.Sprintf("%v", v)
	}

	render := func(tpl string) string {
		return renderTemplate(tpl, replace)
	}

	url := render(httpCfg.URL)
	var bodyReader io.Reader
	if method != http.MethodGet && method != http.MethodDelete && httpCfg.BodyTemplate != "" {
		bodyReader = bytes.NewBufferString(render(httpCfg.BodyTemplate))
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("构造 HTTP 请求失败: %w", err)
	}
	for k, v := range httpCfg.Headers {
		httpReq.Header.Set(render(k), render(v))
	}
	if bodyReader != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		result := &TestActivityResult{Status: "fail", ErrorMsg: err.Error()}
		s.saveActivityTestRecord(ctx, req, actDef, result)
		return result, nil
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		result := &TestActivityResult{Status: "fail", ErrorMsg: "读取响应失败: " + err.Error()}
		s.saveActivityTestRecord(ctx, req, actDef, result)
		return result, nil
	}

	result := &TestActivityResult{}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = "success"
		result.Result = string(respBody)
	} else {
		result.Status = "fail"
		result.ErrorMsg = fmt.Sprintf("HTTP 状态码 %d: %s", resp.StatusCode, string(respBody))
	}
	s.saveActivityTestRecord(ctx, req, actDef, result)
	return result, nil
}

// renderTemplate 将模板中的 {{key}} 占位符替换为 replace 中的值（缺失的占位符保留原样）。
func renderTemplate(tpl string, replace map[string]string) string {
	return templateVarRegex.ReplaceAllStringFunc(tpl, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if v, ok := replace[key]; ok {
			return v
		}
		return m
	})
}

// templateVarRegex 匹配 {{key}} 形式的占位符。
var templateVarRegex = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// saveActivityTestRecord 保存 activity 测试记录（若 req.SaveRecord 为 false 则跳过）。
func (s *WorkflowService) saveActivityTestRecord(ctx context.Context, req *TestActivityRequest, actDef *workflow.ActivityDef, result *TestActivityResult) {
	if !req.SaveRecord {
		return
	}
	record := &workflow.ActivityTestRecordDef{
		Project:      req.Project,
		RecordID:     "", // 由 repo 自动生成
		ActivityID:   req.ActivityID,
		ActivityName: actDef.Name,
		EnvName:      req.EnvName,
		InputParams:  mustJSON(req.InputParams),
		Status:       result.Status,
		Result:       result.Result,
		ErrorMsg:     result.ErrorMsg,
	}
	if err := s.CreateActivityTestRecord(ctx, record); err == nil {
		result.RecordID = record.RecordID
	}
}

// validateRequiredActivityParams 校验 activity 默认参数定义中 frontend / frontend+ 策略参数是否必传且非空。
func validateRequiredActivityParams(actDef *workflow.ActivityDef, input map[string]interface{}) error {
	if len(actDef.Arguments) == 0 {
		return nil
	}
	var bindConfigs []param.BindConfig
	if err := json.Unmarshal(actDef.Arguments, &bindConfigs); err != nil {
		return nil // 参数定义非法不阻断测试
	}
	for _, bc := range bindConfigs {
		policy := string(bc.Policy)
		if policy != string(param.KeyPolicyFrontendOnly) && policy != string(param.KeyPolicyFrontendPriority) {
			continue
		}
		v, ok := input[bc.Key]
		if !ok || v == nil {
			return fmt.Errorf("param %q is required (policy: %s), but not provided", bc.Key, policy)
		}
		if policy == string(param.KeyPolicyFrontendOnly) {
			if isEmptyValue(v) {
				return fmt.Errorf("param %q is required (policy: frontend) and must not be empty", bc.Key)
			}
		}
	}
	return nil
}

// CreateActivityTestRecord 创建 activity 测试记录（自动生成 RecordID）。
func (s *WorkflowService) CreateActivityTestRecord(ctx context.Context, def *workflow.ActivityTestRecordDef) error {
	if def.RecordID == "" {
		id, err := s.activityTestRecordRepo.NextRecordID(ctx, def.Project)
		if err != nil {
			return err
		}
		def.RecordID = id
	}
	return s.activityTestRecordRepo.Create(ctx, def)
}

// ListActivityTestRecords 列出指定 activity 下所有测试记录（按时间倒序）。
func (s *WorkflowService) ListActivityTestRecords(ctx context.Context, project, activityID string) ([]*workflow.ActivityTestRecordDef, error) {
	records, err := s.activityTestRecordRepo.ListByActivity(ctx, project, activityID)
	if err != nil {
		return nil, err
	}
	if records == nil {
		records = []*workflow.ActivityTestRecordDef{}
	}
	return records, nil
}

// DeleteActivityTestRecord 删除 activity 测试记录。
func (s *WorkflowService) DeleteActivityTestRecord(ctx context.Context, project, recordID string) error {
	return s.activityTestRecordRepo.Delete(ctx, project, recordID)
}

// ListActivityLogs 列出指定 activity 的执行日志，支持按字段过滤、关键词搜索与分页，返回日志列表与总条数。
func (s *WorkflowService) ListActivityLogs(ctx context.Context, project, actName string, filter *workflow.ActivityLogFilter) ([]*workflow.ActivityLogDef, int64, error) {
	records, total, err := s.activityLogRepo.ListByActivity(ctx, project, actName, filter)
	if err != nil {
		return nil, 0, err
	}
	if records == nil {
		records = []*workflow.ActivityLogDef{}
	}
	return records, total, nil
}

// ActivityLogRepo 返回 activity 日志仓储实例（供 web 层构造收集器复用同一仓储）。
func (s *WorkflowService) ActivityLogRepo() *repo.ActivityLogRepo {
	return s.activityLogRepo
}

// ============================================================
// RootChain MQ 分布式执行
// ============================================================

// ExecuteRootChainByMQRequest 通过 MQ 执行 rootChain 的请求。
type ExecuteRootChainByMQRequest struct {
	// Project 项目
	Project string `json:"project"`
	// ChainID 根链 ID
	ChainID string `json:"chain_id"`
	// Payload 执行输入（JSON 字符串）
	Payload string `json:"payload"`
	// EnvName 环境名（决定 Redis 等依赖配置）
	EnvName string `json:"env_name"`
	// UseRelease 是否使用已发布版本
	UseRelease bool `json:"use_release,omitempty"`
}

// ExecuteRootChainByMQ 通过 MQ 同步调用分布式 worker 执行某个 rootChain 并返回结果。
func (s *WorkflowService) ExecuteRootChainByMQ(ctx context.Context, req *ExecuteRootChainByMQRequest) (string, error) {
	if req.Project == "" || req.ChainID == "" {
		return "", fmt.Errorf("project and chain_id are required")
	}
	redisCfg, err := s.getRedisConfig(ctx, req.Project, req.EnvName)
	if err != nil {
		return "", err
	}

	envVars := make(map[string]string)
	if req.EnvName != "" {
		if envDef, e := s.envConfigRepo.GetByName(ctx, req.Project, req.EnvName); e == nil && envDef != nil {
			for _, v := range envDef.EnvVars {
				envVars[v.Key] = v.Value
			}
		}
	}

	payload := &workflow.ExecuteRootChainPayload{
		Project:    req.Project,
		ChainID:    req.ChainID,
		Payload:    req.Payload,
		EnvVars:    envVars,
		UseRelease: req.UseRelease,
	}
	resp, err := s.mqExecutor.ExecuteRootChain(ctx, payload, redisCfg)
	if err != nil {
		return "", err
	}
	return extractResultData(resp), nil
}

// ============================================================
// Activity 模板管理
// ============================================================

// CreateActivity 创建 activity 模板（自动生成 ActivityID）。
// ActivityID 基于数据库自增主键 id 组合（A + 6 位零填充 id），插入后再回写，
// 因此与真实自增主键绑定，删除产生的空洞不会被复用，避免与外部已用 ID 冲突。
func (s *WorkflowService) CreateActivity(ctx context.Context, def *workflow.ActivityDef) error {
	// 项目 + 命名空间 + 活动名称 必须全局唯一，避免重复 activity
	exists, err := s.activityRepo.ExistsByNamespaceName(ctx, def.Project, def.ActNamespace, def.ActName, "")
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("activity 已存在：项目=%s, 命名空间=%s, 活动名称=%s 的组合已存在", def.Project, def.ActNamespace, def.ActName)
	}
	if def.Kind == "" {
		def.Kind = workflow.ActivityKindRedis
	}
	// 先用临时占位 activity_id 插入，取回自增主键 id，再组合业务 activity_id 回写。
	tmpID := fmt.Sprintf("__tmp_%d", time.Now().UnixNano())
	if def.ActivityID != "" {
		// 调用方显式指定了 ID（极少用），直接插入并跳过自增组合逻辑
		_, err = s.activityRepo.Create(ctx, def)
		return err
	}
	def.ActivityID = tmpID
	id, err := s.activityRepo.Create(ctx, def)
	if err != nil {
		return err
	}
	bizID := fmt.Sprintf("A%06d", id)
	// 回写业务 activity_id
	if err := s.activityRepo.UpdateActivityID(ctx, def.Project, tmpID, bizID); err != nil {
		return err
	}
	def.ActivityID = bizID
	return nil
}

// GenerateActivityID 生成下一个 activity 的自动 ID（如 A000001）。
func (s *WorkflowService) GenerateActivityID(ctx context.Context) (string, error) {
	return s.activityRepo.NextActivityID(ctx)
}

// GetActivity 获取指定项目下的单个 activity。
func (s *WorkflowService) GetActivity(ctx context.Context, project, activityID string) (*workflow.ActivityDef, error) {
	return s.activityRepo.GetByID(ctx, project, activityID)
}

// ListActivities 列出指定项目下所有可用 activity，可按 tag 与环境(env)过滤（为空表示不过滤）。
// env 用于限定测试状态统计与心跳计算的范围。
func (s *WorkflowService) ListActivities(ctx context.Context, project string, tag string, env string) ([]*workflow.ActivityDef, error) {
	activities, err := s.activityRepo.List(ctx, project)
	if err != nil {
		return nil, err
	}
	if activities == nil {
		activities = []*workflow.ActivityDef{}
	}
	if tag != "" {
		filtered := make([]*workflow.ActivityDef, 0, len(activities))
		for _, a := range activities {
			for _, t := range a.Tags {
				if t == tag {
					filtered = append(filtered, a)
					break
				}
			}
		}
		activities = filtered
	}
	// 填充每个 activity 的测试状态汇总（用于列表图标展示）。
	// 未指定环境时不统计：跨环境聚合（某环境通过即显示通过）会产生误导，前端也不展示该标记。
	if env != "" && len(activities) > 0 {
		ids := make([]string, 0, len(activities))
		for _, a := range activities {
			ids = append(ids, a.ActivityID)
		}
		if statusMap, err := s.activityTestRecordRepo.ListTestStatusByActivities(ctx, project, env, ids); err == nil {
			for _, a := range activities {
				a.TestStatus = statusMap[a.ActivityID]
			}
		}
	}
	return activities, nil
}

// UpdateActivity 更新 activity 模板。
func (s *WorkflowService) UpdateActivity(ctx context.Context, def *workflow.ActivityDef) error {
	// 项目 + 命名空间 + 活动名称 必须全局唯一（排除自身）
	exists, err := s.activityRepo.ExistsByNamespaceName(ctx, def.Project, def.ActNamespace, def.ActName, def.ActivityID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("activity 已存在：项目=%s, 命名空间=%s, 活动名称=%s 的组合已存在", def.Project, def.ActNamespace, def.ActName)
	}
	return s.activityRepo.Update(ctx, def)
}

// DeleteActivity 删除 activity 模板。
func (s *WorkflowService) DeleteActivity(ctx context.Context, project, activityID string) error {
	return s.activityRepo.Delete(ctx, project, activityID)
}

// Shutdown 停止所有引擎并清理资源。
func (s *WorkflowService) Shutdown(ctx context.Context) error {
	return s.engine.Shutdown(ctx)
}
