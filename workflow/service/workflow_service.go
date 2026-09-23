package service

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/cond"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/utils/httputil"
	"github.com/magic-lib/go-plat-workflow/workflow/config"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	param "github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/builder"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
	"github.com/magic-lib/go-plat-workflow/workflow/repo"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
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
	nodeLogRepo            *repo.NodeLogRepo
	userRepo               *repo.UserRepo
	mqExecutor             *workflow.MQExecutor
	dslBuilder             *builder.DSLBuilder
	engine                 *engine.WorkflowEngine

	// invokeRootChainMapCache 缓存「发布在线」的根链 DSL（key = 确定性 ID，value = 解析后的 RuleChain 及其发布版本标识）。
	// key 由 project + chain_key 经 id.GetUUID 确定性生成，相同入参直接命中缓存，避免重复查库。
	// invokeRootChainMapCache 「发布在线」根链 DSL 缓存，key 为 id.GetUUID(project+"-"+chainKey)。
	invokeRootChainMapCache cmap.ConcurrentMap[string, *invokeCacheEntry]
	// chainPoolVersions 每条根链在 rulego 引擎池中已加载版本的登记表，用于错峰回收。
	// key 同上为 cacheKey，与引擎池实际 key（cacheKey@version）区分。
	chainPoolVersions cmap.ConcurrentMap[string, *chainPoolEntry]
	// invalidateBus 根链版本变更的跨副本广播（Redis pub/sub）。
	// 多副本部署时用于通知其他副本失效本地缓存，使其立即切到新版本。
	invalidateBus *invalidatePubSub
}

// cacheKeyOf 计算根链的进程内缓存 key（与 InvokeRootChain 中保持一致）。
func cacheKeyOf(project, chainKey string) string {
	return id.GetUUID(project + "-" + chainKey)
}

// NotifyRootChainChanged 根链版本发生变更（发布/回滚/设为生效/删除）时调用：
//  1. 失效本进程内的 DSL 缓存（并标记在线版本未知，下次调用重新解析到新版本）；
//  2. 通过 Redis pub/sub 广播失效事件，让其他副本同步失效 → 全副本立即切到新版本。
//
// 广播是异步的且失败仅记录日志，不影响本地变更结果；
// Redis 未配置时自动降级为仅本地失效（等同单机部署行为）。
func (s *WorkflowService) NotifyRootChainChanged(project, chainKey, reason string) {
	// 1) 本地失效
	s.ClearChainRootByKey(project, chainKey)
	// 2) 广播给其他副本
	if s.invalidateBus == nil {
		return
	}
	go s.invalidateBus.broadcast(project, chainKey, reason)
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
		&models.NodeLogModel{},
		&models.UserModel{},
		&models.UserSessionModel{},
		&models.UserProjectModel{},
		&models.ProjectSecretModel{},
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

	// wf_node_logs 的 event_id / relation_type 加长至 varchar(1000)，以支持更大数据（如完整链路上下文）。
	// 注意：模型已移除 `;index`，否则 AutoMigrate 会尝试在 varchar(1000) 上建全列索引，
	// 触发 MySQL 5.7「Specified key was too long; max key length is 3072 bytes」（utf8mb4 下 1000 字符≈4000 字节超限）。
	// 因此索引完全由下方显式 SQL 管理：先删旧索引 → 改长度（保留 relation_type 默认值）
	// → 以列名为名重建前缀(191)索引（191*4=764 字节 < 3072，安全且支持等值检索）。
	for _, col := range []string{"event_id", "relation_type"} {
		var idxs []string
		db.Raw("SELECT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_NAME = 'wf_node_logs' AND COLUMN_NAME = ?", col).Scan(&idxs)
		// 回退：GORM 对 `;index`（无显式名）生成的索引名即列名
		idxs = append(idxs, col)
		for _, ix := range idxs {
			if ix == "" {
				continue
			}
			// 删除该列上的索引；忽略「索引不存在」类错误（可能已在上一步删掉或回退名不匹配）
			if err := db.Exec("ALTER TABLE wf_node_logs DROP INDEX `" + ix + "`").Error; err != nil {
				if !strings.Contains(err.Error(), "check that column") &&
					!strings.Contains(err.Error(), "doesn't exist") &&
					!strings.Contains(err.Error(), "Unknown") {
					return nil, err
				}
			}
		}
	}
	if err := db.Exec("ALTER TABLE wf_node_logs MODIFY COLUMN event_id varchar(500)").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("ALTER TABLE wf_node_logs MODIFY COLUMN relation_type varchar(500) DEFAULT ''").Error; err != nil {
		return nil, err
	}
	// 以列名为索引名重建前缀(191)索引（与 GORM 约定一致，避免后续 AutoMigrate 重复创建）
	for _, col := range []string{"event_id", "relation_type"} {
		if err := db.Exec("ALTER TABLE wf_node_logs ADD INDEX `" + col + "` (" + col + "(191))").Error; err != nil &&
			!strings.Contains(err.Error(), "Duplicate") && !strings.Contains(err.Error(), "Duplicate key name") {
			return nil, err
		}
	}

	// AutoMigrate 已自动新增 trace_id 列；这里补建索引（幂等：已存在则忽略报错）。
	if err := db.Exec(
		"ALTER TABLE wf_node_test_records ADD INDEX idx_trace_id (trace_id)",
	).Error; err != nil && !strings.Contains(err.Error(), "Duplicate") && !strings.Contains(err.Error(), "Duplicate key name") {
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
	// 将 activity 模板仓储注入 commnode 组件，使节点执行单个 Activity 时能按
	// ActNamespace+ActName 反查模板的 return_values，正确构造 RequestActivity 的 returnBindConfig。
	workflow.SetCommnodeActivityStore(activityRepo)
	activityTestRecordRepo := repo.NewActivityTestRecordRepo(db)
	activityLogRepo := repo.NewActivityLogRepo(db)
	nodeLogRepo := repo.NewNodeLogRepo(db)
	// 将 node 日志落库实现注入 commnode 组件，使 node 运行日志直接写入 wf_node_logs（不再经 redis 中转）。
	workflow.SetCommnodeNodeLogSaver(nodeLogRepo)
	userRepo := repo.NewUserRepo(db)

	// 幂等种子：若 wf_users 为空，则根据环境变量创建一个 bootstrap 管理员账号。
	if err := ensureBootstrapAdmin(userRepo); err != nil {
		return nil, err
	}

	s := &WorkflowService{
		db:                      db,
		projectRepo:             projectRepo,
		nodeRepo:                nodeRepo,
		subChainRepo:            subChainRepo,
		rootChainRepo:           rootChainRepo,
		releaseRepo:             releaseRepo,
		testCaseRepo:            testCaseRepo,
		envConfigRepo:           envConfigRepo,
		nodeTestRecordRepo:      nodeTestRecordRepo,
		activityRepo:            activityRepo,
		activityTestRecordRepo:  activityTestRecordRepo,
		activityLogRepo:         activityLogRepo,
		nodeLogRepo:             nodeLogRepo,
		userRepo:                userRepo,
		mqExecutor:              workflow.NewMQExecutorWithLogAndEnv(activityLogRepo, envConfigRepo),
		dslBuilder:              builder.NewDSLBuilder(nodeRepo, subChainRepo, rootChainRepo),
		engine:                  engine.NewWorkflowEngine(workflow.NewEngineRootChainStore(rootChainRepo), workflow.NewEngineSubChainStore(subChainRepo)),
		invokeRootChainMapCache: cmap.New[*invokeCacheEntry](),
		chainPoolVersions:       cmap.New[*chainPoolEntry](),
	}

	// 启动后台巡检：即使用户无流量，也能按时间策略错峰回收历史版本实例。
	go s.chainPoolJanitor()

	// 启动跨副本失效广播的订阅端（仅多副本部署时需要）。
	// 由 custom.normal.root_chain_broadcast_enabled 控制：
	//   - true（默认）：多副本部署，建立 Redis 订阅，使各副本发布即时生效；
	//   - false：单机部署，完全不创建订阅与后台协程，省去 Redis 开销。
	// 关闭时 invalidateBus 保持 nil，NotifyRootChainChanged 自动退化为仅本地失效。
	if config.GetRootChainBroadcastEnabled() {
		s.invalidateBus = newInvalidatePubSub(s)
		s.invalidateBus.Start()
		log.Info().Msg("Root chain invalidate broadcast enabled (multi-instance mode)")
	} else {
		log.Info().Msg("Root chain invalidate broadcast disabled (single-instance mode)")
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

// ListProjectSecrets 列出项目下所有密钥（含明文，仅用于管理查询接口）。
func (s *WorkflowService) ListProjectSecrets(ctx context.Context, project string) ([]*workflow.SecretKeyItem, error) {
	secretRepo := s.projectRepo.SecretRepo()
	if secretRepo == nil {
		return nil, fmt.Errorf("secret repo not initialized")
	}
	return secretRepo.List(ctx, project)
}

// CreateProjectSecret 为项目新增一个密钥（密钥明文 + 备注）。
func (s *WorkflowService) CreateProjectSecret(ctx context.Context, project, secretKey, remark string) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	if secretKey == "" {
		return fmt.Errorf("secret_key is required")
	}
	secretKey = id.GetUUID(secretKey)
	secretRepo := s.projectRepo.SecretRepo()
	if secretRepo == nil {
		return fmt.Errorf("secret repo not initialized")
	}
	return secretRepo.Create(ctx, project, secretKey, remark)
}

// DeleteProjectSecret 删除项目下指定密钥（按明文匹配，内部转为记录 ID 删除）。
func (s *WorkflowService) DeleteProjectSecret(ctx context.Context, project, secretKey string) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	if secretKey == "" {
		return fmt.Errorf("secret_key is required")
	}
	secretRepo := s.projectRepo.SecretRepo()
	if secretRepo == nil {
		return fmt.Errorf("secret repo not initialized")
	}
	return secretRepo.DeleteByKey(ctx, project, secretKey)
}

// AuthProjectSecret 校验项目密钥（API_TOKEN）。
// 支持两种鉴权方式：
//  1. 带 timestamp（推荐）：校验时间戳 ±5 分钟内且 MD5 恒定时间比对一致。
//     其中"存储密钥"为列表接口返回的哈希串（id.GetUUID 结果），对外当作明文密钥使用，避免用户录入弱口令被暴力破解。
//  2. 不带 timestamp（兼容旧调用）：直接恒定时间比对存储密钥与传入值。
//
// 匹配返回 nil，否则返回错误。
func (s *WorkflowService) AuthProjectSecret(ctx context.Context, project, key string, timestamp int64) error {
	if project == "" {
		return fmt.Errorf("project is required")
	}
	if key == "" {
		return fmt.Errorf("token is required")
	}

	storedKeys, err := s.projectRepo.GetSecrets(ctx, project)
	if err != nil {
		return err
	}
	if len(storedKeys) == 0 {
		return fmt.Errorf("project has no secret_key configured, please set it first")
	}

	// 时间戳签名模式：校验时效并比对
	if timestamp > 0 {
		now := time.Now().Unix()
		if diff := now - timestamp; diff > 300 || diff < -300 {
			return fmt.Errorf("timestamp expired or invalid (allowed ±5min)")
		}
		for _, k := range storedKeys {
			expected := httputil.GenFeiShuSign(timestamp, k)
			if subtle.ConstantTimeCompare([]byte(expected), []byte(key)) == 1 {
				return nil
			}
		}
		return fmt.Errorf("token mismatch")
	}

	// 兼容旧模式：直接比对存储密钥
	for _, k := range storedKeys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			return nil
		}
	}
	return fmt.Errorf("secret_key mismatch")
}

// GetProjectConfig 对外配置查询：根据项目密钥鉴权后，
// 返回项目下的环境配置信息，以及可执行的 RootChains 概要列表（不含 DSL 等敏感内容）。
// 密钥不匹配时返回错误。token 可由 timestamp 参与签名（推荐），也可直接传密钥（兼容）。
func (s *WorkflowService) GetProjectConfig(ctx context.Context, project string) (*workflow.ProjectConfigResponse, error) {
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

// GetProjectRedisConfig 对外配置查询：校验项目密钥后，按项目 + 环境名返回该环境的 Redis 配置。
// 与 GetProjectConfig 共用 secret_key 鉴权；环境未配置或无 Redis 时返回明确错误。
func (s *WorkflowService) GetProjectRedisConfig(ctx context.Context, project, envName string) (*workflow.RedisConfig, error) {
	if envName == "" {
		return nil, fmt.Errorf("env_name is required")
	}
	return s.getRedisConfig(ctx, project, envName)
}

// ============================================================
// Node 管理
// ============================================================

// RegisterNode 注册节点到数据库。
func (s *WorkflowService) RegisterNode(ctx context.Context, def *workflow.NodeDef) error {
	// 保存前为 activities 缺失的 arg_template / ret_template 补齐元定义数据，避免落库后运行时解析参数出错
	if err := s.fillNodeActivityTemplates(ctx, def); err != nil {
		log.Warn().Err(err).Str("node_id", def.NodeID).Msg("fillNodeActivityTemplates skipped due to error")
	}
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
	def, err := s.nodeRepo.GetByID(ctx, project, nodeID)
	if err != nil {
		return nil, err
	}
	// 标注是否带路由功能（配置了 switch_condition），供前端展示
	if def != nil {
		def.HasSwitchCondition = def.HasSwitchConditionExpr()
	}
	return def, nil
}

// ListNodes 列出指定项目下的节点，可按命名空间与 tag 过滤（为空表示不过滤）。
// onlyEnabled=true 时仅返回启用状态（用于编排选择），false 时返回全部（含禁用，用于管理列表）。
func (s *WorkflowService) ListNodes(ctx context.Context, project, namespace, tag string, onlyEnabled, isAdmin bool) ([]*workflow.NodeDef, error) {
	all, err := s.nodeRepo.List(ctx, project, namespace, onlyEnabled)
	if err != nil {
		return nil, err
	}
	if all == nil {
		all = []*workflow.NodeDef{}
	}
	if tag != "" {
		filtered := make([]*workflow.NodeDef, 0, len(all))
		for _, n := range all {
			for _, t := range n.Tags {
				if t == tag {
					filtered = append(filtered, n)
					break
				}
			}
		}
		all = filtered
	}
	// 标注路由功能：配置了 switch_condition 的节点带路由分支，前端据此展示标记
	for _, n := range all {
		n.HasSwitchCondition = n.HasSwitchConditionExpr()
	}
	// 标注已发布引用：已被发布到根链（含子链传递引用）的节点，前端据此展示标记并（对普通用户）禁用编辑/删除按钮。
	// 超级管理员仍可操作，但前端会据此给出二次确认提示，避免误改/误删线上引用。故对所有角色都标注。
	if len(all) > 0 {
		idx, err := s.buildPublishedRefIndex(ctx, project)
		if err != nil {
			return nil, err
		}
		for _, n := range all {
			if _, ok := idx.nodes[n.NodeID]; ok {
				n.PublishedInRootChain = true
			}
			// 同时带上明细列表（含根链 ID/名称/版本），供列表展示引用数量与悬停明细
			n.PublishedRootChains = sortedRefs(idx.nodeChains, n.NodeID)
		}
	}
	return all, nil
}

// UpdateNode 更新节点配置。
// 节点已被发布到根链（生产快照引用）时禁止更新，避免影响线上调用。
func (s *WorkflowService) UpdateNode(ctx context.Context, def *workflow.NodeDef, isAdmin bool) error {
	published, err := s.NodePublishedInRootChain(ctx, def.Project, def.NodeID, isAdmin)
	if err != nil {
		return err
	}
	if published && !isAdmin {
		return workflow.ErrNodePublishedInRootChain
	}
	// 保存前为 activities 缺失的 arg_template / ret_template 补齐元定义数据，避免落库后运行时解析参数出错
	if err := s.fillNodeActivityTemplates(ctx, def); err != nil {
		log.Warn().Err(err).Str("node_id", def.NodeID).Msg("fillNodeActivityTemplates skipped due to error")
	}
	return s.nodeRepo.Update(ctx, def)
}

// isEmptyTemplateValue 判断 arg_template / ret_template 是否被当成「空」处理（缺失、空串、空对象、空数组）。
func isEmptyTemplateValue(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		return s == "" || s == "{}" || s == "null"
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// fillNodeActivityTemplates 在保存节点前，为 node_config.activities 中缺失 arg_template /
// ret_template 的 activity，从对应的 activity 元定义（项目内按 act_namespace+act_name 定位）
// 补齐这两个字段，确保落库的节点配置带有完整的参数/返回值模板，运行时解析不再出错。
// 仅当对应字段缺失或为空时才补齐，已显式配置的字段保留原值（支持节点级自定义覆盖）。
func (s *WorkflowService) fillNodeActivityTemplates(ctx context.Context, def *workflow.NodeDef) error {
	if len(def.Configuration) == 0 {
		return nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(def.Configuration, &cfg); err != nil {
		return err
	}
	nc, ok := cfg["node_config"].(map[string]any)
	if !ok {
		nc = map[string]any{}
		cfg["node_config"] = nc
	}
	actsRaw, ok := nc["activities"]
	if !ok {
		return nil
	}
	// activities 按阶段分组：[][]activity；也兼容扁平 []activity
	stages, ok := actsRaw.([]any)
	if !ok {
		return nil
	}
	changed := false
	for si := range stages {
		stage, ok := stages[si].([]any)
		if !ok {
			// 扁平结构：把单个 activity 当成单元素阶段处理
			if actMap, ok := stages[si].(map[string]any); ok {
				stage = []any{actMap}
			} else {
				continue
			}
		}
		for ai := range stage {
			actMap, ok := stage[ai].(map[string]any)
			if !ok {
				continue
			}
			ns, _ := actMap["act_namespace"].(string)
			name, _ := actMap["act_name"].(string)
			if ns == "" || name == "" {
				continue
			}
			actDef, err := s.activityRepo.GetByNamespaceName(ctx, def.Project, ns, name)
			if err != nil {
				// 找不到对应 activity 元定义，跳过补齐（不阻断保存）
				continue
			}
			// 补齐 arg_template（缺省或空时），来源为 activity 的 arg_template 字符串
			if v, ok := actMap["arg_template"]; !ok || isEmptyTemplateValue(v) {
				if actDef.ArgTemplate != "" {
					actMap["arg_template"] = actDef.ArgTemplate
					changed = true
				}
			}
			// 补齐 ret_template（缺省或空时），来源为 activity 的 return_values
			if v, ok := actMap["ret_template"]; !ok || isEmptyTemplateValue(v) {
				if len(actDef.ReturnValues) > 0 && string(actDef.ReturnValues) != "null" {
					var rv any
					if err := json.Unmarshal(actDef.ReturnValues, &rv); err == nil {
						actMap["ret_template"] = rv
						changed = true
					}
				}
			}
			stage[ai] = actMap
		}
		stages[si] = stage
	}
	if changed {
		b, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		def.Configuration = b
	}
	return nil
}

// DeleteNode 软删除节点。
// 节点已被发布到根链（当前生效快照引用）时禁止删除，避免影响线上调用。超级管理员不受限。
func (s *WorkflowService) DeleteNode(ctx context.Context, project, nodeID string, isAdmin bool) error {
	published, err := s.NodePublishedInRootChain(ctx, project, nodeID, isAdmin)
	if err != nil {
		return err
	}
	if published && !isAdmin {
		return workflow.ErrNodePublishedInRootChain
	}
	return s.nodeRepo.Delete(ctx, project, nodeID)
}

// ============================================================
// SubChain 管理
// ============================================================

// RegisterSubChain 注册子链到数据库。
func (s *WorkflowService) RegisterSubChain(ctx context.Context, def *workflow.SubChainDef) error {
	// 强制将 DSL 中的 ruleChain.id 还原为子链 ID，name 同步为子链名称，
	// 防止用户手动修改 DSL 导致 id 与子链不一致（id 始终固定为 ChainID）。
	normalizeSubChainDSL(def)
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

// ListSubChains 列出指定项目下的子链。
// onlyEnabled=true 时仅返回启用状态（用于编排选择），false 时返回全部（含禁用，用于管理列表）。
func (s *WorkflowService) ListSubChains(ctx context.Context, project string, onlyEnabled bool) ([]*workflow.SubChainDef, error) {
	return s.subChainRepo.List(ctx, project, onlyEnabled)
}

// UpdateSubChain 更新子链配置。
func (s *WorkflowService) UpdateSubChain(ctx context.Context, def *workflow.SubChainDef) error {
	// 同 RegisterSubChain：DSL 中 ruleChain.id 固定为子链 ID，name 同步为当前名称。
	normalizeSubChainDSL(def)
	return s.subChainRepo.Update(ctx, def)
}

// normalizeSubChainDSL 强制把 def.DSLJSON 中的 ruleChain.id 还原为子链 ID（ChainID），
// 并把 ruleChain.name 同步为子链当前名称（def.Name）。其余 DSL 内容原样保留。
// 这样无论用户在前端手动怎么改 DSL 里的 id/name，落库时都会被纠正：
// id 永远等于子链 ID，name 跟随子链名称变化，id 不会被改掉。
// 若 def.DSLJSON 为空或非法 JSON，则不做处理（由 builder 在装配时正确生成）。
func normalizeSubChainDSL(def *workflow.SubChainDef) {
	if def == nil || strings.TrimSpace(def.DSLJSON) == "" || def.ChainID == "" {
		return
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(def.DSLJSON), &root); err != nil {
		// 非法 JSON 不强行覆盖，交由后续校验/装配流程处理。
		return
	}
	rc, ok := root["ruleChain"].(map[string]interface{})
	if !ok {
		// 不存在 ruleChain 节点时创建一个，保证 id/name 存在。
		rc = map[string]interface{}{}
		root["ruleChain"] = rc
	}
	rc["id"] = def.ChainID
	rc["name"] = def.Name
	if b, err := json.Marshal(root); err == nil {
		def.DSLJSON = string(b)
	}
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
	def, err := s.dslBuilder.BuildSubChain(ctx, req)
	if err != nil {
		return nil, err
	}
	normalizeSubChainDSL(def)
	return def, nil
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
	// DSL 中 ruleChain.id 固定为子链 ID，name 同步为当前名称（安全兜底）。
	normalizeSubChainDSL(def)
	if err := s.subChainRepo.Update(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// buildSubChainDef 内部公共：装配+规范化 DSL（id=ChainID, name=ChainName）。
func (s *WorkflowService) buildSubChainDef(ctx context.Context, req *workflow.BuildSubChainRequest) (*workflow.SubChainDef, error) {
	def, err := s.dslBuilder.AssembleSubChain(ctx, req)
	if err != nil {
		return nil, err
	}
	normalizeSubChainDSL(def)
	return def, nil
}

// ============================================================
// RootChain 构建与管理
// ============================================================

// SaveRootChain 保存根链草稿（按 project+chain_id 查询，存在则更新、不存在则创建，幂等操作）。
// 不再物理删除重建，避免自增主键 id 持续增长。
// 仅影响测试环境草稿，已发布的版本快照不受影响。
func (s *WorkflowService) SaveRootChain(ctx context.Context, req *workflow.BuildRequest) (*workflow.RootChainDef, error) {
	// 构建 DSL 并 upsert（Build 内部先更新、不存在则创建）
	return s.dslBuilder.Build(ctx, req)
}

// CreateRootChain 仅录入基本信息（chain_key/name/description/status），DSL 默认为空对象 "{}"。
// 用于在编排前先建立一条 Root Chain 草稿记录，后续编排完成后再通过 SaveRootChain 更新 dsl_json。
// 注意：MySQL 的 json 列不允许空字符串，故 DSLJSON 必须显式给 "{}" 而非 ""。
func (s *WorkflowService) CreateRootChain(ctx context.Context, project, chainKey, name, description string, status int) (*workflow.RootChainDef, error) {
	def := &workflow.RootChainDef{
		Project:     project,
		ChainKey:    chainKey,
		Name:        name,
		Description: description,
		DSLJSON:     "{}",
		Status:      int8(status),
	}
	if err := s.rootChainRepo.Create(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// GetRootChain 获取指定项目下的单个根链（按 ChainID）。
func (s *WorkflowService) GetRootChain(ctx context.Context, chainID string) (*workflow.RootChainDef, error) {
	return s.rootChainRepo.GetByID(ctx, chainID)
}

// GetRootChainByKey 按项目+ChainKey 获取根链（project 与 chain_key 联合唯一，方便用业务键直接调用主链）。
func (s *WorkflowService) GetRootChainByKey(ctx context.Context, project, chainKey string) (*workflow.RootChainDef, error) {
	return s.rootChainRepo.GetByKey(ctx, project, chainKey)
}

// ListRootChains 列出指定项目下所有可用根链，并填充每个根链是否存在发布记录（用于前端隐藏删除按钮）。
func (s *WorkflowService) ListRootChains(ctx context.Context, project string) ([]*workflow.RootChainDef, error) {
	chains, err := s.rootChainRepo.List(ctx, project)
	if err != nil {
		return nil, err
	}
	for _, c := range chains {
		has, err := s.releaseRepo.HasReleases(ctx, project, c.ChainID)
		if err != nil {
			return nil, err
		}
		c.HasReleases = has

		// 解析 dsl_json，提取每个节点 arguments/responses 中引用的 {{arguments.xxx}} 入参，
		// 赋值给 MustInputParams，供前端列表展示该根链调用所需入参。
		var rc types.RuleChain
		if c.DSLJSON != "" {
			if err := json.Unmarshal([]byte(c.DSLJSON), &rc); err == nil {
				c.MustInputParams = builder.CollectAllInputArguments(rc.Metadata.Nodes)
			}
		}
	}

	return chains, nil
}

// DeleteRootChain 物理删除根链草稿；若存在发布记录则拒绝删除。
func (s *WorkflowService) DeleteRootChain(ctx context.Context, project, chainID string) error {
	has, err := s.releaseRepo.HasReleases(ctx, project, chainID)
	if err != nil {
		return err
	}
	if has {
		return workflow.ErrRootChainHasReleases
	}
	// 删除前先取回 ChainKey（删除后就查不到了），用于后续清理缓存与引擎池实例。
	var chainKey string
	if rc, qerr := s.rootChainRepo.GetByID(ctx, chainID); qerr == nil {
		chainKey = rc.ChainKey
	}
	if err := s.rootChainRepo.Delete(ctx, project, chainID); err != nil {
		return err
	}
	// 根链已删除，其各版本引擎实例不再可用，连同 DSL 缓存一并清理。
	// 同时广播给其他副本，使其也清理该链的缓存与引擎池实例。
	if chainKey != "" {
		s.ClearChainRootByKey(project, chainKey)
		s.invalidateChainPoolEntry(cacheKeyOf(project, chainKey))
		s.NotifyRootChainChanged(project, chainKey, reasonDelete)
	}
	return nil
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
	draft, err := s.rootChainRepo.GetByID(ctx, chainID)
	if err != nil {
		return nil, err
	}

	// 与当前线上版本对比：若内容完全一致则拒绝发布，避免产生重复版本。
	if current, cerr := s.releaseRepo.GetCurrent(ctx, project, chainID); cerr == nil && current != nil {
		if rootChainContentEqual(draft, current) {
			return nil, fmt.Errorf("当前草稿与线上生效版本(v%d)内容完全一致，无需重复发布", current.Version)
		}
	}

	maxVer, err := s.releaseRepo.MaxVersion(ctx, project, chainID)
	if err != nil {
		return nil, err
	}
	release := &workflow.RootChainReleaseDef{
		Project:             draft.Project,
		ChainID:             draft.ChainID,
		Version:             maxVer + 1,
		Name:                draft.Name,
		Description:         draft.Description,
		DSLJSON:             draft.DSLJSON,
		NodeIDs:             draft.NodeIDs,
		SubChainIDs:         draft.SubChainIDs,
		ConnectionsData:     draft.ConnectionsData,
		NodeParamOverrides:  draft.NodeParamOverrides,
		NodeSwitchOverrides: draft.NodeSwitchOverrides,
		NodeNameOverrides:   draft.NodeNameOverrides,
		NodeCollapseOverrides: draft.NodeCollapseOverrides,
		IsCurrent:           true,
		PublishedAt:         time.Now(),
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

// canonicalJSON 将 JSON 字符串规范化为可比较的形式（忽略空白/键序差异）。
// 空字符串与无法解析的字符串均按原样返回，保证比较结果稳定。
func canonicalJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return t
	}
	b, err := json.Marshal(v)
	if err != nil {
		return t
	}
	return string(b)
}

// rootChainContentEqual 比较两个根链的内容是否完全一致（用于发布去重）。
// draft 为即将发布的草稿（RootChainDef），current 为当前线上版本（RootChainReleaseDef）。
// 仅比较业务内容字段，忽略版本号、发布时间等元数据。
func rootChainContentEqual(draft *workflow.RootChainDef, current *workflow.RootChainReleaseDef) bool {
	if draft.Name != current.Name || draft.Description != current.Description {
		return false
	}
	newFieldsJson := []string{
		draft.DSLJSON,
		draft.ConnectionsData,
		draft.NodeParamOverrides,
	}
	oldFieldsJson := []string{
		current.DSLJSON,
		current.ConnectionsData,
		current.NodeParamOverrides,
	}

	for i := 0; i < len(newFieldsJson); i++ {
		if !cond.IsSameJson(newFieldsJson[i], oldFieldsJson[i]) {
			return false
		}
	}

	newFieldsString := []string{
		draft.NodeIDs,
		draft.SubChainIDs,
	}
	oldFieldsString := []string{
		current.NodeIDs,
		current.SubChainIDs,
	}
	for i := 0; i < len(newFieldsString); i++ {
		if newFieldsString[i] != oldFieldsString[i] {
			return false
		}
	}
	return true
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

// executeRootChainByIDTimeout 流程同步执行的超时时间，避免长时间取不到结果导致调用方永久阻塞。
const executeRootChainByIDTimeout = 3600 * time.Second

// ExecuteRootChainByID 基于已解析的根链 DSL（ruleChain）同步执行流程。
// 从根链 flow 节点提取子链 ID 并通过 project 查询子链 DSL，组装 ActivityFlowConfig 后
// 调用 rulegox.StartWorkFlow 同步执行，返回 FlowContext 序列化结果。
func (s *WorkflowService) ExecuteRootChainByID(ctx context.Context, ruleChain *types.RuleChain, jsonPayload map[string]any, project, envName, traceId string, activityFlowConfig *rulegox.ActivityFlowConfig) (*paramx.FlowContext, error) {
	// 整体执行加超时，防止流程长时间不返回导致阻塞
	execCtx, cancel := context.WithTimeout(ctx, executeRootChainByIDTimeout)
	defer cancel()

	rootChainID := ruleChain.RuleChain.ID

	// 对传进来的参数进行检查，避免后面执行缺少参数
	err := s.checkAllNodesArguments(ruleChain.Metadata.Nodes, jsonPayload)
	if err != nil {
		return nil, err
	}

	// 1. 从根链 flow 节点提取子链 ID（configuration.ruleChainId = "project:subChainID"）
	subChainIDs := make(map[string]bool)
	for _, node := range ruleChain.Metadata.Nodes {
		if node == nil || node.Type != "flow" {
			continue
		}
		if ref, ok := node.Configuration["ruleChainId"].(string); ok && ref != "" {
			if idx := strings.LastIndex(ref, ":"); idx >= 0 {
				ref = ref[idx+1:]
			}
			if ref != "" {
				subChainIDs[ref] = true
			}
		}
	}

	// 2. 查询子链 DSL
	var subChainDSL []*types.RuleChainBaseInfo
	var subChainNodes []*types.RuleNode // 收集子链节点，用于提取子链所需的入参
	for subID := range subChainIDs {
		subDef, err := s.subChainRepo.GetByID(execCtx, project, subID)
		if err != nil {
			return nil, fmt.Errorf("get sub chain %s failed: %w", subID, err)
		}
		subChain := &types.RuleChain{}
		if err := json.Unmarshal([]byte(subDef.DSLJSON), subChain); err != nil {
			return nil, fmt.Errorf("parse sub chain %s dsl failed: %w", subID, err)
		}
		subChainDSL = append(subChainDSL, &subChain.RuleChain)
		subChainNodes = append(subChainNodes, subChain.Metadata.Nodes...)
	}

	// 子链节点也可能引用 {{arguments.xxx}} / 顶层 {{name}}，按根链+子链所需参数对原始入参重新过滤，避免误删
	// 过滤多余入参：仅保留链实际需要的入参，避免恶意传入无关参数造成变量名污染。
	// originalPayload 保留原始入参，子链加载后再按根链+子链所需参数重新过滤一次。
	originalPayload := jsonPayload
	jsonPayload = s.filterPayloadArguments(originalPayload, ruleChain.Metadata.Nodes, subChainNodes)

	// 2.1 根据项目+环境解析 Redis 配置（按环境将运行数据打入对应 Redis）
	redisCfg, err := s.GetRedisConnect(execCtx, project, envName)
	if err != nil {
		return nil, fmt.Errorf("resolve redis config failed: %w", err)
	}

	// 3. 构造流程上下文：全局入参作为 arguments（供 DSL 中的 {{arguments.x}} 取值）
	flowCtx := s.getParamContext(ruleChain, jsonPayload)

	engine.MysqlLogger.Info("ExecuteRootChainByID", " ruleChain:", ruleChain, " param:", conv.String(jsonPayload), " flowCtx:", conv.String(flowCtx))

	actConfig := &rulegox.ActivityFlowConfig{}
	if activityFlowConfig != nil {
		actConfig = activityFlowConfig
	}
	actConfig.RootChainDSL = ruleChain
	actConfig.SubChainDSL = subChainDSL
	actConfig.FlowContext = flowCtx

	// 5. 执行元数据：环境 + Redis 配置（按环境将运行数据打入对应 Redis）
	metaData := rulegox.ActivityMetaData{
		Env:         envName,
		Project:     project,
		RootChainID: rootChainID,
		RedisConfig: redisCfg,
		TraceId:     id.GetUUID(traceId),
		PoolKey:     actConfig.PoolKey, // 发布/invoke 路径下用于引擎池隔离，避免与草稿实例冲突
		// 发布/invoke 路径下记录本次执行的发布版本标识（形如 R000005@3），写入节点日志。
		RootChainReleaseID: actConfig.RootChainReleaseID,
	}

	// 6. 同步执行并捕获结果
	var (
		resultParam *paramx.FlowContext
		resultErr   error
		done        = make(chan struct{})
	)
	actConfig.EndFunc = func(ctx context.Context, relationType string, param *paramx.FlowContext, err error) {
		resultParam = param
		resultErr = err
		close(done)
	}
	if err := rulegox.StartWorkFlow(execCtx, actConfig, &metaData); err != nil {
		return nil, err
	}

	// 等待执行结束或超时，避免长时间取不到结果导致永久阻塞
	select {
	case <-done:
		if resultErr != nil {
			return nil, resultErr
		}
		if resultParam == nil {
			return nil, fmt.Errorf("execute root chain %s: empty result", rootChainID)
		}
		return resultParam, nil
	case <-execCtx.Done():
		return nil, fmt.Errorf("execute root chain %s: timeout after %s: %w", rootChainID, executeRootChainByIDTimeout, execCtx.Err())
	}
}

func (s *WorkflowService) getParamContext(ruleChain *types.RuleChain, jsonPayload map[string]any) *paramx.FlowContext {
	newJson := conv.MapFromKeyList(jsonPayload)
	for k, v := range jsonPayload {
		newJson[k] = v
	}
	nodeIdList := lo.Map(ruleChain.Metadata.Nodes, func(node *types.RuleNode, index int) string {
		return node.Id
	})
	flowCtx := paramx.NewFlowContext(ruleChain.RuleChain.ID, id.NewUUID(), newJson, func(key string, val any) (any, bool) {
		if lo.Contains(nodeIdList, key) {
			return val, true
		}
		return val, false
	})
	return flowCtx
}

// checkAllNodesArguments 校验节点 configuration.arguments / responses 中形如 {{arguments.xxx}}
// 的前端入参占位符（xxx 可能含层级分隔 "."，如 N000036__70lic.mobile），是否都在 jsonPayload 中传入；
// 缺失则报错，避免后续执行时因缺少入参而失败。
//
// 判定顺序：
//  1. 按层级路径在 jsonPayload 中查找（中间 "." 视为层级分隔，等价于 json.Get(jsonPayload, item)）；
//  2. 未找到则视为 "." 被当作整体字符串键传入，从 jsonPayloadMap（conv.KeyListFromMap 扁平化结果）中再查；
//  3. 都未找到 → 该入参未传，报错。
func (s *WorkflowService) checkAllNodesArguments(nodes []*types.RuleNode, jsonPayload map[string]any) error {
	allInputArguments := builder.CollectAllInputArguments(nodes)
	jsonPayloadMap := conv.KeyListFromMap(jsonPayload)
	jsonPayloadStr := conv.String(jsonPayload)
	var firstErr []string
	for _, item := range allInputArguments {
		if item == "" {
			continue
		}

		// 1) 按层级路径在嵌套结构 jsonPayload 中查找
		if _, ok := jsonPayload[item]; ok {
			continue
		}
		// 2) 未找到：中间 "." 被当作整体字符串键传入，从扁平化结果中查找
		if _, ok := jsonPayloadMap[item]; ok {
			continue
		}
		// 说明是深层的json串了
		if getByDotPath(jsonPayloadStr, item) {
			continue
		}
		// 3) 都没找到 → 未传该参数
		firstErr = append(firstErr, item)
	}
	if len(firstErr) > 0 {
		return fmt.Errorf("argument list: %s not input", strings.Join(firstErr, ","))
	}

	return nil
}

// getByDotPath 利用 gjson 按 "." 分隔的层级路径在 JSON 字符串中查找节点是否存在。
// 例如 path="N000036__70lic.mobile" 会依次进入 .N000036__70lic.mobile，
// 仅当路径真实存在时返回 true（gjson 的 Exists 判断）。
func getByDotPath(jsonStr string, path string) bool {
	if path == "" {
		return false
	}
	return gjson.Parse(jsonStr).Get(path).Exists()
}

// filterPayloadArguments 仅保留 nodeGroups 中各节点实际引用的入参（{{arguments.xxx}} 及顶层 {{name}}），
// 过滤掉其余无关参数，避免变量名污染。nodeGroups 可传多个节点集合（如根链节点、子链节点），
// 全部按需保留。返回一个新的 map，不修改入参 payload
// 只处理第一层key
func (s *WorkflowService) filterPayloadArguments(payload map[string]any, nodeGroups ...[]*types.RuleNode) map[string]any {
	nodeList := make([]*types.RuleNode, 0)
	lo.ForEach(nodeGroups, func(nodes []*types.RuleNode, _ int) {
		nodeList = append(nodeList, nodes...)
	})
	allInputArguments := builder.CollectAllInputArguments(nodeList)
	jsonPayloadMap := conv.KeyListFromMap(payload)
	newPayload := make(map[string]any, len(allInputArguments))
	for _, item := range allInputArguments {
		if item == "" {
			continue
		}
		if v, ok := payload[item]; ok {
			newPayload[item] = v
			continue
		}
		oneItem := strings.Split(item, ".")
		if v, ok := payload[oneItem[0]]; ok {
			newPayload[oneItem[0]] = v
			continue
		}
		if v, ok := jsonPayloadMap[item]; ok {
			newPayload[item] = v
			continue
		}
	}
	return newPayload
}
func (s *WorkflowService) ClearChainRootByKey(project, chainKey string) {
	cacheKey := id.GetUUID(project + "-" + chainKey)
	// 清除「发布在线」DSL 缓存，强制下次调用重新查库拿到新版本 DSL 与版本号。
	//
	// 注意：这里【不能】调用 rulego.Del(cacheKey) 删除引擎池实例！
	// rulego 的 Pool.Del 内部会调用引擎实例的 Stop(ctx)：先等待活跃消息自然完成
	// （Pool.Del 传 context.Background()，默认最多等 10s），超时则强制取消上下文
	// 中断正在执行的流程 —— 存在打断长流程（>10s）的线上风险。
	// 正确做法：引擎池 key 带发布版本号（见 InvokeRootChain 的 PoolKey），
	// 发布后新请求自然命中新版本实例；旧版本实例由错峰回收策略（数量 + 存活时间）
	// 在优雅排空后移除，故这里【不直接清理引擎实例】。
	s.invokeRootChainMapCache.Remove(cacheKey)

	// 同时把登记表中的「在线版本」标记为未知：
	// 发布/回滚/设为生效后当前在线版本已变化，需等下次调用重新解析；
	// 在未知期间巡检保守不动，避免回滚到的版本被误当可清理项回收。
	s.markChainPoolCurrentUnknown(cacheKey)
}

// markChainPoolCurrentUnknown 将某条根链的在线版本标记为未知，并重置错峰计时。
func (s *WorkflowService) markChainPoolCurrentUnknown(cacheKey string) {
	if e, ok := s.chainPoolVersions.Get(cacheKey); ok && e != nil {
		e.mu.Lock()
		e.current = currentUnknown
		e.waitStart = time.Time{}
		e.mu.Unlock()
	}
}

// ============================================================
// rulego 引擎池版本登记与清理（错峰 FIFO）
// ============================================================
//
// 背景：InvokeRootChain 的引擎池 key 带发布版本号（<cacheKey>@<version>），
// 发布后新请求自然命中新实例，旧实例不会被 Del/Stop，因此存量流程安全跑完；
// 但代价是池中会残留历史版本实例，需要按策略回收。
//
// 回收策略（两配置项 custom.normal）：
//   - root_chain_pool_max_versions：每条根链最多保留的版本实例数（0=不限制数量）
//   - root_chain_pool_ttl_minutes：非在线版本最长存活时间（分钟，0=不限制时间）
//
// 组合语义：
//   - 仅数量（ttl=0）  ：始终维持该数量，超出部分按发布时间先后【立即】清理；
//   - 仅时间（数量=0） ：按发布时间先后【错峰】清理，每间隔 ttl 清一个，最终只剩在线版本；
//   - 两者都配        ：最终保留该数量（含在线版本），超出部分按时间先后错峰清理。
//
// 排序依据【发布时间 PublishedAt】而非版本号：回滚场景下老版本号可能重新成为在线版本，
// 只有发布时间能真实反映先后。在线版本始终受保护，永不被清理。

const (
	// poolStopGrace 移除版本实例前的优雅排空时长（30 分钟）。
	// rulego.Del 内部会以 10s 超时强制中断流程，故先用较长宽限期自行 Stop，
	// 让长流程尽量自然跑完，再 Del 摘除登记。
	// 设为 30 分钟以覆盖含人工审批、慢外部调用等长流程场景。
	poolStopGrace = 30 * time.Minute
	// poolSweepInterval 后台巡检间隔。
	poolSweepInterval = 1 * time.Minute
	// currentUnknown 在线版本号未知（版本号从 1 开始，故 -1 可作哨兵）。
	// 发生在发布/回滚/设为生效之后、下一次调用重新解析之前。
	// 此时巡检必须保守不动，否则回滚到的版本会被误当成可清理项。
	currentUnknown = -1
)

// poolVersionEntry 引擎池中某个已加载版本的登记信息。
type poolVersionEntry struct {
	// PoolKey 该版本对应的 rulego 引擎池 key（<cacheKey>@<version>）。
	PoolKey string
	// Version 发布版本号。
	Version int
	// PublishedAt 该发布版本的发布时间，用于 FIFO 排序（回滚安全）。
	PublishedAt time.Time
	// LoadedAt 载入引擎池的时刻，用于同发布时间时的稳定排序。
	LoadedAt time.Time
}

// chainPoolEntry 单条根链在引擎池中已加载版本的登记表。
type chainPoolEntry struct {
	mu sync.Mutex
	// current 当前在线版本号，永不被清理。
	current int
	// versions version -> 登记项。
	versions map[int]*poolVersionEntry
	// waitStart 队首（最早发布）待清理版本的等待起点。
	// 清理掉队首后置为当前时刻，使下一个版本从此时起再等待一个 ttl，实现错峰。
	waitStart time.Time
}

// getOrCreateChainPoolEntry 获取（或新建）某条根链的登记表。
func (s *WorkflowService) getOrCreateChainPoolEntry(cacheKey string) *chainPoolEntry {
	if e, ok := s.chainPoolVersions.Get(cacheKey); ok && e != nil {
		return e
	}
	ne := &chainPoolEntry{versions: make(map[int]*poolVersionEntry), current: currentUnknown}
	s.chainPoolVersions.Set(cacheKey, ne)
	// 极小概率并发覆盖；以 map 中最终生效者为准，避免两个登记表并存。
	if cur, ok := s.chainPoolVersions.Get(cacheKey); ok && cur != nil {
		return cur
	}
	return ne
}

// trackPoolVersion 记录本次使用的版本，并标记其为在线版本。
func (s *WorkflowService) trackPoolVersion(cacheKey string, version int, poolKey string, publishedAt time.Time) {
	e := s.getOrCreateChainPoolEntry(cacheKey)
	e.mu.Lock()
	e.current = version
	if it, ok := e.versions[version]; ok {
		it.PoolKey = poolKey
		if !publishedAt.IsZero() {
			it.PublishedAt = publishedAt
		}
	} else {
		e.versions[version] = &poolVersionEntry{
			PoolKey:     poolKey,
			Version:     version,
			PublishedAt: publishedAt,
			LoadedAt:    time.Now(),
		}
	}
	e.mu.Unlock()
}

// candidatesLocked 计算待清理候选（需在持锁时调用）。
// 返回按【发布时间升序】排列的可清理版本；在线版本不在其中。
// retain 为除在线版本外还可保留的最新版本数量。
func (e *chainPoolEntry) candidatesLocked(retain int) []*poolVersionEntry {
	cands := make([]*poolVersionEntry, 0, len(e.versions))
	for ver, it := range e.versions {
		if ver == e.current {
			continue // 在线版本永不清
		}
		cands = append(cands, it)
	}
	// FIFO：发布时间早的排前面；同发布时间用载入时刻、版本号兜底保证稳定。
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if !a.PublishedAt.Equal(b.PublishedAt) {
			return a.PublishedAt.Before(b.PublishedAt)
		}
		if !a.LoadedAt.Equal(b.LoadedAt) {
			return a.LoadedAt.Before(b.LoadedAt)
		}
		return a.Version < b.Version
	})
	// 保留最近发布的 retain 个，其余为候选
	if retain > 0 && len(cands) > retain {
		return cands[:len(cands)-retain]
	}
	if retain > 0 {
		return nil
	}
	return cands
}

// sweepChainPool 按策略巡检并清理某条根链的引擎池版本实例。
func (s *WorkflowService) sweepChainPool(cacheKey string) {
	e, ok := s.chainPoolVersions.Get(cacheKey)
	if !ok || e == nil {
		return
	}
	maxVersions, ttlMinutes := config.GetRootChainPoolPolicy()
	ttl := time.Duration(ttlMinutes) * time.Minute

	// 在线版本未知（发布/回滚/设为生效后尚未重新解析）：保守跳过，
	// 否则回滚到的版本会被误判为可清理项而被回收。
	e.mu.Lock()
	unknown := e.current == currentUnknown
	e.mu.Unlock()
	if unknown {
		return
	}

	// 除在线版本外还可保留的数量：maxVersions 含在线版本，故减 1。
	retain := 0
	if maxVersions > 0 {
		retain = maxVersions - 1
		if retain < 0 {
			retain = 0
		}
	}

	for {
		now := time.Now()
		e.mu.Lock()
		cands := e.candidatesLocked(retain)
		if len(cands) == 0 {
			e.waitStart = time.Time{} // 无可清理项，等待计时归零
			e.mu.Unlock()
			return
		}
		if e.waitStart.IsZero() {
			e.waitStart = now // 队首开始计时
		}
		head := cands[0]
		due := e.waitStart.Add(ttl)
		if now.Before(due) {
			e.mu.Unlock()
			return // 未到清理时点
		}
		delete(e.versions, head.Version)
		e.mu.Unlock()

		// 先自行优雅排空（宽限期远大于 rulego.Del 内置的 10s），再摘除池中登记。
		s.stopAndDropVersion(head.PoolKey, cacheKey, head.Version)

		e.mu.Lock()
		e.waitStart = time.Now() // 下一个队首从现在起重新计时 → 错峰 T
		e.mu.Unlock()

		// ttl=0 表示纯数量策略：本轮一直清到满足数量为止。
		if ttl > 0 {
			return
		}
	}
}

// stopAndDropVersion 优雅停止并移除某个版本的引擎实例。
func (s *WorkflowService) stopAndDropVersion(poolKey, cacheKey string, version int) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().Any("panic", rec).Str("pool_key", poolKey).Msg("Recovered panic during engine pool version cleanup")
		}
	}()

	// 1) 自行 Stop：使用远长于 rulego.Del 内置值（10s）的宽限期，
	//    尽量让正在执行的流程自然跑完，避免被强制中断。
	if ins, ok := rulego.Get(poolKey); ok && ins != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), poolStopGrace)
		ins.Stop(stopCtx)
		cancel()
	}
	// 2) 摘除池中登记项（此时已排空，内部 Stop 会很快返回）。
	rulego.Del(poolKey)

	log.Info().Str("cache_key", cacheKey).Int("version", version).Str("pool_key", poolKey).
		Msg("Idle root chain engine instance evicted by pool policy")
}

// invalidateChainPoolEntry 某条根链对应的根链已被删除时，清理其全部版本实例与登记。
func (s *WorkflowService) invalidateChainPoolEntry(cacheKey string) {
	e, ok := s.chainPoolVersions.Get(cacheKey)
	if !ok || e == nil {
		return
	}
	e.mu.Lock()
	victims := make([]*poolVersionEntry, 0, len(e.versions))
	for _, it := range e.versions {
		victims = append(victims, it)
	}
	e.versions = make(map[int]*poolVersionEntry)
	e.waitStart = time.Time{}
	e.mu.Unlock()

	for _, it := range victims {
		s.stopAndDropVersion(it.PoolKey, cacheKey, it.Version)
	}
	s.chainPoolVersions.Remove(cacheKey)
}

// chainPoolJanitor 后台巡检，保证无流量时也能按时间策略清理。
func (s *WorkflowService) chainPoolJanitor() {
	ticker := time.NewTicker(poolSweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		keys := s.chainPoolVersions.Keys()
		for _, k := range keys {
			s.sweepChainPool(k)
		}
	}
}

// invokeCacheEntry 「发布在线」根链缓存项：解析后的 DSL 及对应的发布版本标识。
type invokeCacheEntry struct {
	RuleChain *types.RuleChain
	// ReleaseID 发布版本标识，形如 R000005@3，与 RuleChain 一起缓存，
	// 保证日志记录的版本号与缓存 DSL 版本一致（发布后缓存失效前不会误报成新版本）。
	ReleaseID string
	// Version 发布版本号，与 DSL 一起缓存，用于生成带版本的引擎池 key。
	// 每次发布版本号递增，使新请求使用新的引擎实例，从而与旧版本实例彻底隔离。
	Version int
	// PublishedAt 该发布版本的发布时间，缓存后用于版本实例的 FIFO 清理排序。
	PublishedAt time.Time
}

// InvokeRootChain 通过 project + chain_key 定位「发布在线」的根链 DSL 并同步执行。
// 缓存：map[cacheKey]*invokeCacheEntry，cacheKey = id.GetUUID(project+"-"+chain_key)（确定性），
// 命中缓存直接复用已解析的 DSL，跳过查询 wf_root_chains 与 wf_root_chain_releases。
func (s *WorkflowService) InvokeRootChain(ctx context.Context, project, chainKey, envName, traceId string, payload map[string]any, isAsync bool) (any, error) {
	cacheKey := id.GetUUID(project + "-" + chainKey)

	// 1. 先查缓存
	cached, ok := s.invokeRootChainMapCache.Get(cacheKey)
	var ruleChain *types.RuleChain
	var releaseID string
	var releaseVersion int
	var releasePublishedAt time.Time
	if ok && cached != nil {
		ruleChain = cached.RuleChain
		releaseID = cached.ReleaseID
		releaseVersion = cached.Version
		releasePublishedAt = cached.PublishedAt
	}
	// 2. 未命中：查库并解析 DSL，再写入缓存
	if ruleChain == nil {
		// 2.1 通过 project + chain_key 查根链（得到 chain_id）
		rootDef, err := s.rootChainRepo.GetByKey(ctx, project, chainKey)
		if err != nil {
			return nil, fmt.Errorf("get root chain by key failed: %w", err)
		}
		// 2.2 查询发布在线版本（is_current）
		release, err := s.releaseRepo.GetCurrent(ctx, project, rootDef.ChainID)
		if err != nil {
			return nil, fmt.Errorf("get current release failed: %w", err)
		}
		// 2.3 解析 DSL
		rc := &types.RuleChain{}
		if err := json.Unmarshal([]byte(release.DSLJSON), rc); err != nil {
			return nil, fmt.Errorf("parse root chain dsl failed: %w", err)
		}
		// 注意：不可将 rc.RuleChain.ID 覆写为 cacheKey（UUID），否则该 UUID 会经
		// metaData.RootChainID 流入节点日志 root_chain_id 字段，污染数据。
		// 引擎池隔离改用 metaData.PoolKey（cacheKey@version）实现（见 StartWorkFlow）。
		// 2.4 记录发布版本标识，用于节点日志的 root_chain_release_id 字段
		releaseID = fmt.Sprintf("%s@%d", rootDef.ChainID, release.Version)
		releaseVersion = release.Version
		releasePublishedAt = release.PublishedAt
		// 2.5 写入缓存
		s.invokeRootChainMapCache.Set(cacheKey, &invokeCacheEntry{RuleChain: rc, ReleaseID: releaseID, Version: releaseVersion, PublishedAt: releasePublishedAt})
		ruleChain = rc
	}

	// 3. 执行：以真实根链 id 记录日志，以带版本号的 key 作为引擎池 key 隔离不同发布版本，
	// 并记录当时执行的发布版本号（root_chain_release_id）。
	//
	// 引擎池 key 带版本号（<cacheKey>@<version>）的作用：
	//   - 发布后版本号递增，新请求必然未命中旧实例，从而用新版本 DSL 重建引擎 → 立即走新流程；
	//   - 旧版本实例不再被任何新请求引用，且【不会被 Del/Stop】，
	//     因此正在其上执行的存量流程不受影响，可安全执行完毕后成为孤儿被回收。
	//   （若直接对旧实例调用 rulego.Del，其内部 Stop() 会在等待 10s 后强制中断长流程。）
	poolKey := fmt.Sprintf("%s@%d", cacheKey, releaseVersion)
	// 登记该版本到本链的引擎池登记表（标记在线，参与错峰回收），
	// 再按策略巡检一次：有流量时即可快速回收超量版本（尤其纯数量策略需立即生效）。
	s.trackPoolVersion(cacheKey, releaseVersion, poolKey, releasePublishedAt)
	s.sweepChainPool(cacheKey)
	return s.ExecuteRootChainByID(ctx, ruleChain, payload, project, envName, traceId, &rulegox.ActivityFlowConfig{
		IsAsync:            isAsync,
		UseCache:           true,
		PoolKey:            poolKey,
		RootChainReleaseID: releaseID,
	})
}

// ExecutePublishedRootChain 加载并执行生产环境当前发布版本。
// 与草稿执行互不影响：使用发布时快照的 DSL。
// envName / redisCfg 非空时，按环境将运行数据打入对应 Redis。
func (s *WorkflowService) ExecutePublishedRootChain(ctx context.Context, project, chainID, jsonPayload, envName string, redisCfg *conn.Connect) (string, error) {
	release, err := s.releaseRepo.GetCurrent(ctx, project, chainID)
	if err != nil {
		return "", err
	}
	if err := s.engine.LoadChainDSL(ctx, project, chainID, release.DSLJSON, release.SubChainIDs); err != nil {
		return "", err
	}
	// 记录本次执行的发布版本标识（形如 R000005@3），写入节点日志的 root_chain_release_id。
	releaseID := fmt.Sprintf("%s@%d", chainID, release.Version)
	return s.engine.ExecuteWithEnv(ctx, project, chainID, jsonPayload, envName, redisCfg, releaseID)
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
// envName / redisCfg 非空时，执行会按环境将运行数据（node 日志、Activity 结果）打入对应 Redis。
func (s *WorkflowService) BuildLoadAndExecute(ctx context.Context, req *workflow.BuildRequest, jsonPayload string, envName string, redisCfg *conn.Connect) (string, error) {
	// 1. 组装
	def, err := s.dslBuilder.Build(ctx, req)
	if err != nil {
		return "", err
	}

	// 2. 加载
	if err := s.engine.LoadChain(ctx, def.Project, def.ChainID); err != nil {
		return "", err
	}

	// 3. 执行（按环境注入 Redis 元数据；BuildLoadAndExecute 为即时编排执行，无发布版本，release 标识留空）
	return s.engine.ExecuteWithEnv(ctx, def.Project, def.ChainID, jsonPayload, envName, redisCfg, "")
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
	// TraceID 本次测试的分布式追踪 ID，用于回查本次执行产生的 activity 日志（wf_activity_logs.trace_id）
	TraceID string `json:"trace_id,omitempty"`
	// DurationMs 测试执行耗时（毫秒）
	DurationMs int64 `json:"duration_ms"`
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
	resp, inputParams, err := s.mqExecutor.TestNode(ctx, payload)

	// 6. 整理结果
	result := &TestNodeResult{}
	var execMs int64
	if resp != nil {
		result.TraceID = resp.TraceID
		execMs = resp.DurationMs
	}

	if err != nil {
		result.Status = "fail"
		result.ErrorMsg = err.Error()
	} else {
		result.Status = "success"
		result.Result = conv.String(resp.Response)
	}
	result.DurationMs = execMs

	// 7. 保存测试记录（除非显式关闭）
	if req.SaveRecord {
		record := &workflow.NodeTestRecordDef{
			Project:     req.Project,
			RecordID:    "", // 由 repo 自动生成
			NodeID:      req.NodeID,
			NodeName:    nodeDef.Name,
			EnvName:     req.EnvName,
			TraceID:     result.TraceID,
			InputParams: mustJSON(inputParams),
			EnvVars:     mustJSON(envVars),
			Status:      result.Status,
			Result:      result.Result,
			ErrorMsg:    result.ErrorMsg,
			DurationMs:  execMs,
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

// GetRedisConnect 根据项目+环境名解析出 Redis 连接（conn.Connect），供执行引擎按环境打入对应 Redis。
// 环境未配置或无 Redis 时返回明确错误。
func (s *WorkflowService) GetRedisConnect(ctx context.Context, project, envName string) (*conn.Connect, error) {
	redisCfg, err := s.getRedisConfig(ctx, project, envName)
	if err != nil {
		return nil, err
	}
	host := redisCfg.Addr
	port := "6379"
	// Addr 形如 host:port 或 host
	if idx := strings.Index(host, ":"); idx >= 0 {
		port = host[idx+1:]
		host = host[:idx]
	}
	return &conn.Connect{
		Driver:   "redis",
		Host:     host,
		Port:     port,
		Username: redisCfg.Username,
		Password: redisCfg.Password,
		Database: fmt.Sprintf("%d", redisCfg.DB),
	}, nil
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
	InputParams map[string]any `json:"input_params"`
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
	// TraceID 本次测试的分布式追踪 ID，用于回查本次执行产生的 activity 日志（wf_activity_logs.trace_id）
	TraceID string `json:"trace_id,omitempty"`
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
	resp, params, err := s.mqExecutor.RequestActivity(ctx, worker, actDef, req.InputParams, &workflow.ActivityLogValue{
		RootChainID: "TestActivity",
		TraceID:     traceId,
		SpanID:      spanId,
		Attributes: map[string]any{
			"activity_id":        req.ActivityID,
			"project":            req.Project,
			"env_name":           req.EnvName,
			"activity_type":      kind,
			"activity_namespace": actDef.ActNamespace,
			"activity_name":      actDef.ActName,
			"activity_label":     actDef.Name,
			"trace_id":           traceId,
			"span_id":            spanId,
		},
	})

	req.InputParams = params

	// 6. 整理结果
	result := &TestActivityResult{}
	if err != nil {
		result.Status = "fail"
		result.ErrorMsg = err.Error()
	} else {
		result.Status = "success"
		result.Result = conv.String(resp.Data)
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

	client := &http.Client{Timeout: config.DefaultTimeout}
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

// ListNodeLogs 列出指定 node 的运行日志（按时间倒序，支持分页），返回日志列表与总条数。
func (s *WorkflowService) ListNodeLogs(ctx context.Context, project, nodeID string, limit, offset int) ([]*workflow.NodeLogDef, int64, error) {
	records, total, err := s.nodeLogRepo.ListByNode(ctx, project, nodeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if records == nil {
		records = []*workflow.NodeLogDef{}
	}
	return records, total, nil
}

// NodeLogStats 统计 node 日志：按 node + 天聚合，返回每个 node 每天的访问量与错误量。
// 常用于前端「统计」页柱状图。env 为空表示不限定环境；nodeID 为空表示统计全部 node；days 为最近 N 天。
func (s *WorkflowService) NodeLogStats(ctx context.Context, project, env, nodeID string, days int) ([]workflow.NodeLogDayStat, error) {
	stats, err := s.nodeLogRepo.StatsByDay(ctx, project, env, nodeID, days)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = []workflow.NodeLogDayStat{}
	}
	return stats, nil
}

// ActivityLogRepo 返回 activity 日志仓储实例（供 web 层构造收集器复用同一仓储）。
func (s *WorkflowService) ActivityLogRepo() *repo.ActivityLogRepo {
	return s.activityLogRepo
}

// NodeLogRepo 返回 node 运行日志仓储实例（供 web 层构造收集器复用同一仓储）。
func (s *WorkflowService) NodeLogRepo() *repo.NodeLogRepo {
	return s.nodeLogRepo
}

// UserRepo 返回用户/会话/授权仓储实例（供 web 层鉴权与用户管理复用）。
func (s *WorkflowService) UserRepo() *repo.UserRepo {
	return s.userRepo
}

// ensureBootstrapAdmin 在 wf_users 为空时，按环境变量创建一个管理员账号（幂等）。
func ensureBootstrapAdmin(userRepo *repo.UserRepo) error {
	ctx := context.Background()
	cnt, err := userRepo.CountUsers(ctx)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	username := osGetenv("WF_BOOTSTRAP_USER", "admin")
	rawPwd := osGetenv("WF_BOOTSTRAP_PASSWORD", "admin123")
	hash, err := bcrypt.GenerateFromPassword([]byte(rawPwd), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = userRepo.CreateUser(ctx, &models.UserModel{
		Username:     username,
		PasswordHash: string(hash),
		Nickname:     "Admin",
		Role:         "admin",
		Status:       1,
	})
	if err != nil {
		return err
	}
	log.Info().Msgf("bootstrap admin user created: %s", username, " password:", rawPwd)
	return nil
}

// osGetenv 读取环境变量，缺失时返回默认值。
func osGetenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ListNodeLogsGlobal 按条件全局查询 node 运行日志（按时间倒序，支持分页），返回日志列表与总条数。
// 常用于按 trace_id 回查某次执行涉及的所有 node 记录。
func (s *WorkflowService) ListNodeLogsGlobal(ctx context.Context, project string, f *workflow.NodeLogFilter) ([]*workflow.NodeLogDef, int64, error) {
	f2 := f
	if f2 == nil {
		f2 = &workflow.NodeLogFilter{}
	}
	records, total, err := s.nodeLogRepo.ListByFilter(ctx, project, f2)
	if err != nil {
		return nil, 0, err
	}
	if records == nil {
		records = []*workflow.NodeLogDef{}
	}
	return records, total, nil
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
func (s *WorkflowService) ListActivities(ctx context.Context, project string, tag string, env string, isAdmin bool) ([]*workflow.ActivityDef, error) {
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
	// 标注已发布引用：已被发布到根链（含子链传递引用）的 activity 禁止编辑/删除，前端据此禁用按钮。
	// 标注已发布引用：超级管理员仍可操作，但前端会据此给出二次确认提示，避免误改/误删线上引用。故对所有角色都标注。
	if len(activities) > 0 {
		idx, err := s.buildPublishedRefIndex(ctx, project)
		if err != nil {
			return nil, err
		}
		// 「activity → 引用它的 Node」索引，用于列表展示改动影响面。
		// 该列仅为辅助参考，构建失败时降级为空列表，不影响主列表返回。
		refNodeMap := map[string][]*workflow.RefNodeInfo{}
		if refNodes, berr := s.buildActivityRefNodes(ctx, project, idx); berr == nil {
			refNodeMap = refNodes
		} else {
			log.Warn().Err(berr).Str("project", project).Msg("Build activity ref node index failed, skip ref_nodes")
		}

		for _, a := range activities {
			key := a.ActNamespace + "\x00" + a.ActName
			if _, ok := idx.activities[key]; ok {
				a.PublishedInRootChain = true
			}
			// 同时带上明细列表（含根链 ID/名称/版本），供列表展示引用数量与悬停明细
			a.PublishedRootChains = sortedRefs(idx.activityChains, key)
			// 引用该 activity 的节点明细（含节点是否已在发布链中）
			a.RefNodes = refNodeMap[key]
		}
	}
	return activities, nil
}

// UpdateActivity 更新 activity 模板。
// activity 已被发布到根链（当前生效快照引用）时禁止更新，避免影响线上调用。超级管理员不受限。
func (s *WorkflowService) UpdateActivity(ctx context.Context, def *workflow.ActivityDef, isAdmin bool) error {
	published, err := s.ActivityPublishedInRootChain(ctx, def.Project, def.ActNamespace, def.ActName, isAdmin)
	if err != nil {
		return err
	}
	if published {
		return workflow.ErrActivityPublishedInRootChain
	}
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
// activity 已被发布到根链（当前生效快照引用）时禁止删除，避免影响线上调用。超级管理员不受限。
func (s *WorkflowService) DeleteActivity(ctx context.Context, project, activityID string, isAdmin bool) error {
	// 先查出 activity 的 namespace+name 用于发布引用检查
	existing, err := s.activityRepo.GetByID(ctx, project, activityID)
	if err != nil {
		return err
	}
	if existing != nil {
		published, err := s.ActivityPublishedInRootChain(ctx, project, existing.ActNamespace, existing.ActName, isAdmin)
		if err != nil {
			return err
		}
		if published {
			return workflow.ErrActivityPublishedInRootChain
		}
	}
	return s.activityRepo.Delete(ctx, project, activityID)
}

// Shutdown 停止所有引擎并清理资源。
func (s *WorkflowService) Shutdown(ctx context.Context) error {
	return s.engine.Shutdown(ctx)
}
