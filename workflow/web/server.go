// Package web 提供 Workflow 管理 Web 界面和 REST API。
package web

import (
	"context"
	"embed"
	"encoding/json"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/service"
	"github.com/rulego/rulego/api/types"
)

//go:embed index.html orch.html assets
var webAssets embed.FS

// WebServer 工作流管理 Web 服务。
type WebServer struct {
	svc       *service.WorkflowService
	collector *workflow.ActivityCollector
	mux       *http.ServeMux
}

// NewWebServer 创建 Web 服务实例。
// 活动日志/心跳收集器会定时扫描系统中所有项目×环境的 EnvConfig，
// 自动为每个配置了 Redis 的环境建立监听任务；配置被移除时对应任务自动关闭。
func NewWebServer(db *gorm.DB) (*WebServer, error) {
	svc, err := service.NewWorkflowService(db)
	if err != nil {
		return nil, err
	}

	ws := &WebServer{svc: svc, mux: http.NewServeMux()}

	// 启动活动日志/心跳收集器（自动发现各环境 Redis 配置）
	ws.collector = workflow.NewActivityCollector(svc.ActivityLogRepo(), svc.NodeLogRepo(), svc)
	ws.collector.Start()

	ws.registerRoutes()
	return ws, nil
}

// Handler 返回 HTTP Handler（外层包上鉴权中间件）。
func (ws *WebServer) Handler() http.Handler {
	return ws.authMiddleware(ws.mux)
}

// Shutdown 关闭工作流引擎与收集器，释放资源。
func (ws *WebServer) Shutdown(ctx context.Context) error {
	if ws.collector != nil {
		ws.collector.Stop()
	}
	return ws.svc.Shutdown(ctx)
}

func (ws *WebServer) registerRoutes() {
	// 静态页面
	ws.mux.HandleFunc("/", ws.serveIndex)
	// 子链编排独立页面
	ws.mux.HandleFunc("GET /orch", ws.serveOrch)
	// 公共静态资源（CSS / JS）
	ws.mux.HandleFunc("GET /assets/", ws.serveAssets)

	// 健康检查
	ws.mux.HandleFunc("GET /api/health", ws.handleHealth)

	// 登录 / 登出 / 当前用户
	ws.mux.HandleFunc("POST /api/login", ws.handleLogin)
	ws.mux.HandleFunc("POST /api/logout", ws.handleLogout)
	ws.mux.HandleFunc("GET /api/me", ws.handleMe)
	ws.mux.HandleFunc("POST /api/me/password", ws.handleChangeMyPassword)

	// 用户管理（仅 admin）
	ws.mux.HandleFunc("GET /api/users", ws.handleListUsers)
	ws.mux.HandleFunc("POST /api/users", ws.handleCreateUser)
	ws.mux.HandleFunc("PUT /api/users/{user_id}", ws.handleUpdateUser)
	ws.mux.HandleFunc("DELETE /api/users/{user_id}", ws.handleDeleteUser)
	ws.mux.HandleFunc("POST /api/users/{user_id}/projects", ws.handleBindUserProject)

	// Projects API（无需 project 参数）
	ws.mux.HandleFunc("GET /api/projects", ws.handleListProjects)
	ws.mux.HandleFunc("POST /api/projects", ws.handleCreateProject)
	ws.mux.HandleFunc("PUT /api/projects/{project}", ws.handleUpdateProject)
	ws.mux.HandleFunc("DELETE /api/projects/{project}", ws.handleDeleteProject)
	// 对外配置查询（需传入项目密钥，返回环境配置 + 可执行 RootChains 列表）
	ws.mux.HandleFunc("POST /api/projects/{project}/config", ws.handleGetProjectConfig)
	// 项目密钥管理（仅 admin）：列出/新增/删除项目的多个访问密钥
	ws.mux.HandleFunc("GET /api/projects/{project}/secrets", ws.handleListProjectSecrets)
	ws.mux.HandleFunc("POST /api/projects/{project}/secrets", ws.handleCreateProjectSecret)
	ws.mux.HandleFunc("DELETE /api/projects/{project}/secrets", ws.handleDeleteProjectSecret)

	// Nodes API（project 通过查询参数 ?project=xxx 传入）
	ws.mux.HandleFunc("GET /api/nodes", ws.handleListNodes)
	ws.mux.HandleFunc("POST /api/nodes", ws.handleCreateNode)
	ws.mux.HandleFunc("GET /api/nodes/{node_id}", ws.handleGetNode)
	ws.mux.HandleFunc("PUT /api/nodes/{node_id}", ws.handleUpdateNode)
	ws.mux.HandleFunc("DELETE /api/nodes/{node_id}", ws.handleDeleteNode)
	// 单节点测试（MQ 分布式执行）
	ws.mux.HandleFunc("POST /api/nodes/{node_id}/test", ws.handleTestNode)
	ws.mux.HandleFunc("GET /api/nodes/{node_id}/test-records", ws.handleListNodeTestRecords)
	// node 运行日志（收集器落库 wf_node_logs，记录每个 node 执行时的入参与返回值）
	ws.mux.HandleFunc("GET /api/nodes/{node_id}/logs", ws.handleListNodeLogs)
	ws.mux.HandleFunc("DELETE /api/node-test-records/{record_id}", ws.handleDeleteNodeTestRecord)
	ws.mux.HandleFunc("DELETE /api/nodes/{node_id}/test-records", ws.handleClearNodeTestRecords)

	// SubChains API
	ws.mux.HandleFunc("GET /api/sub-chains", ws.handleListSubChains)
	ws.mux.HandleFunc("POST /api/sub-chains", ws.handleCreateSubChain)
	ws.mux.HandleFunc("GET /api/sub-chains/{chain_id}", ws.handleGetSubChain)
	ws.mux.HandleFunc("PUT /api/sub-chains/{chain_id}", ws.handleUpdateSubChain)
	ws.mux.HandleFunc("DELETE /api/sub-chains/{chain_id}", ws.handleDeleteSubChain)
	// SubChains 编排构建（创建时 chain_id 自动生成）
	ws.mux.HandleFunc("POST /api/sub-chains/build", ws.handleBuildSubChain)
	ws.mux.HandleFunc("PUT /api/sub-chains/{chain_id}/build", ws.handleUpdateSubChainBuild)
	ws.mux.HandleFunc("POST /api/sub-chains/{chain_id}/execute", ws.handleExecuteSubChain)

	// RootChains API
	ws.mux.HandleFunc("GET /api/root-chains", ws.handleListRootChains)
	ws.mux.HandleFunc("POST /api/root-chains", ws.handleSaveRootChain)
	ws.mux.HandleFunc("POST /api/root-chains/create", ws.handleCreateRootChain)
	ws.mux.HandleFunc("DELETE /api/root-chains/{chain_id}", ws.handleDeleteRootChain)

	// RootChains 发布与回滚
	ws.mux.HandleFunc("POST /api/root-chains/{chain_id}/publish", ws.handlePublishRootChain)
	ws.mux.HandleFunc("GET /api/root-chains/{chain_id}/releases", ws.handleListRootChainReleases)
	ws.mux.HandleFunc("POST /api/root-chains/{chain_id}/rollback", ws.handleRollbackRootChain)
	ws.mux.HandleFunc("POST /api/root-chains/{chain_id}/set-current", ws.handleSetCurrentRelease)
	ws.mux.HandleFunc("DELETE /api/root-chains/{chain_id}/releases/{version}", ws.handleDeleteRootChainRelease)
	ws.mux.HandleFunc("GET /api/releases/current", ws.handleListCurrentReleases)

	// 工作流执行
	ws.mux.HandleFunc("POST /api/workflow/execute", ws.handleExecuteWorkflow)

	// TestCase API（测试用例：保存/加载/删除执行配置，挂载在 root/sub 上）
	ws.mux.HandleFunc("GET /api/test-cases", ws.handleListTestCases)
	ws.mux.HandleFunc("POST /api/test-cases", ws.handleSaveTestCase)
	ws.mux.HandleFunc("GET /api/test-cases/{case_id}", ws.handleGetTestCase)
	ws.mux.HandleFunc("DELETE /api/test-cases/{case_id}", ws.handleDeleteTestCase)

	// EnvConfig API（项目级环境配置：环境变量 / Redis / MySQL）
	ws.mux.HandleFunc("GET /api/env-configs", ws.handleListEnvConfigs)
	ws.mux.HandleFunc("POST /api/env-configs", ws.handleSaveEnvConfig)
	ws.mux.HandleFunc("GET /api/env-configs/{env_name}", ws.handleGetEnvConfig)
	ws.mux.HandleFunc("DELETE /api/env-configs/{env_name}", ws.handleDeleteEnvConfig)

	// Activity API（activity 模板管理，project 通过 ?project= 传入）
	ws.mux.HandleFunc("GET /api/activities", ws.handleListActivities)
	ws.mux.HandleFunc("POST /api/activities", ws.handleCreateActivity)
	ws.mux.HandleFunc("GET /api/activities/{activity_id}", ws.handleGetActivity)
	ws.mux.HandleFunc("PUT /api/activities/{activity_id}", ws.handleUpdateActivity)
	ws.mux.HandleFunc("DELETE /api/activities/{activity_id}", ws.handleDeleteActivity)
	// Activity 测试（MQ 分布式执行）
	ws.mux.HandleFunc("POST /api/activities/{activity_id}/test", ws.handleTestActivity)
	ws.mux.HandleFunc("GET /api/activities/{activity_id}/test-records", ws.handleListActivityTestRecords)
	ws.mux.HandleFunc("DELETE /api/activity-test-records/{record_id}", ws.handleDeleteActivityTestRecord)
	// Activity 执行日志（收集器落库，支持按字段搜索）
	ws.mux.HandleFunc("GET /api/activities/{activity_id}/logs", ws.handleListActivityLogs)
	// 跨 Activity 日志查询（不限定单个 activity，按 trace_id 等字段查询全项目执行日志）
	ws.mux.HandleFunc("GET /api/activity-logs", ws.handleListActivityLogsGlobal)
	// 跨 Node 日志查询（按 trace_id 等字段全局查询 node 运行日志）
	ws.mux.HandleFunc("GET /api/node-logs", ws.handleListNodeLogsGlobal)
}

// projectParam 从 URL query 中提取 project 参数，未传则返回空字符串。
func projectParam(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("project"))
}

// onlyEnabledParam 从 URL query 中解析 only_enabled 参数，默认 false（返回全部，含禁用）。
// 仅当显式传 only_enabled=true 时才只返回启用状态的记录（用于编排选择时过滤禁用项）。
func onlyEnabledParam(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("only_enabled")), "true")
}

// ============================================================
// 静态页面
// ============================================================

func (ws *WebServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(webAssets, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// serveOrch 返回子链编排独立页面（orch.html）
func (ws *WebServer) serveOrch(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(webAssets, "orch.html")
	if err != nil {
		http.Error(w, "orch.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// serveAssets 返回公共静态资源（assets 目录下的 css/js 等）
// 开发期优先读取磁盘 assets/ 目录，便于改完前端无需重新编译即可热更新；
// 找不到文件时回退到编译期嵌入的 webAssets（生产兜底）。
func (ws *WebServer) serveAssets(w http.ResponseWriter, r *http.Request) {
	// /assets/xxx -> assets/xxx
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}

	// 浏览器不缓存静态资源，避免强缓存导致前端改动不生效
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	var data []byte
	var err error
	// 优先磁盘（相对可执行文件的工作目录）
	if d, e := os.ReadFile(filepath.Join("workflow", "web", "assets", name)); e == nil {
		data = d
	} else if d, e := os.ReadFile(filepath.Join("assets", name)); e == nil {
		data = d
	} else {
		// 回退 embed
		data, err = fs.ReadFile(webAssets, "assets/"+name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	switch {
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Write(data)
}

// ============================================================
// Projects API
// ============================================================

func (ws *WebServer) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := ws.svc.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if projects == nil {
		projects = []*workflow.ProjectDef{}
	}
	// viewer 仅能看到被授权的项目
	if u := currentUser(r); u != nil && u.Role != "admin" {
		allowed, e := ws.svc.UserRepo().ListProjectsByUser(r.Context(), u.ID)
		if e != nil {
			writeError(w, http.StatusInternalServerError, e.Error())
			return
		}
		out := make([]*workflow.ProjectDef, 0, len(projects))
		allowedSet := make(map[string]struct{}, len(allowed))
		for _, p := range allowed {
			allowedSet[p] = struct{}{}
		}
		for _, p := range projects {
			if _, ok := allowedSet[p.Project]; ok {
				out = append(out, p)
			}
		}
		projects = out
	}
	writeJSON(w, http.StatusOK, projects)
}

// handleCreateProject 新建项目。任意登录用户均可创建；创建者自动绑定到该项目，
// 普通用户（viewer）创建后即可在自己的项目列表中看到并访问它。
func (ws *WebServer) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		// 兜底：从会话 Cookie 重新解析当前用户
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			if _, su, serr := ws.svc.UserRepo().GetSession(r.Context(), c.Value); serr == nil && su != nil {
				u = su
			}
		}
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return
	}
	var def workflow.ProjectDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if def.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if def.Status == 0 {
		def.Status = 1
	}
	def.CreatedBy = u.Username // 记录创建者，普通用户在已有项目列表中仅展示自己的项目
	if err := ws.svc.CreateProject(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 创建者自动绑定到该项目（普通用户创建后立即可见，admin 绑不绑定都不影响全项目可见）。
	if err := ws.svc.UserRepo().BindProject(r.Context(), u.ID, def.Project); err != nil {
		writeError(w, http.StatusInternalServerError, "create project ok but bind owner failed: "+err.Error())
		return
	}
	log.Info().Str("project", def.Project).Uint("owner", u.ID).Msg("project created via web")
	writeJSON(w, http.StatusCreated, def)
}

func (ws *WebServer) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !ws.requireProjectAccess(w, r, project) {
		return
	}
	var def workflow.ProjectDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	if err := ws.svc.UpdateProject(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Msg("project updated via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !ws.requireProjectAccess(w, r, project) {
		return
	}
	if err := ws.svc.DeleteProject(r.Context(), project); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Msg("project deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// handleGetProjectConfig 对外配置查询：需传入正确的项目密钥，
// 返回项目下的环境配置与可执行的 RootChains 概要列表（不含 DSL 等敏感内容）。
func (ws *WebServer) handleGetProjectConfig(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	var req struct {
		SecretKey string `json:"secret_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	cfg, err := ws.svc.GetProjectConfig(r.Context(), project, req.SecretKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// requireAdmin 已在 auth.go 中定义（返回 *models.UserModel，nil 表示校验失败并已写出错误响应）。

// handleListProjectSecrets 列出项目下所有密钥（含备注）。
func (ws *WebServer) handleListProjectSecrets(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if ws.requireAdmin(w, r) == nil {
		return
	}
	secrets, err := ws.svc.ListProjectSecrets(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if secrets == nil {
		secrets = []*workflow.SecretKeyItem{}
	}
	writeJSON(w, http.StatusOK, secrets)
}

// handleCreateProjectSecret 为项目新增密钥（密钥明文 + 备注）。
func (ws *WebServer) handleCreateProjectSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if ws.requireAdmin(w, r) == nil {
		return
	}
	var req struct {
		SecretKey string `json:"secret_key"`
		Remark    string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if err := ws.svc.CreateProjectSecret(r.Context(), project, req.SecretKey, req.Remark); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Info().Str("project", project).Msg("project secret created via web")
	writeJSON(w, http.StatusCreated, map[string]string{"message": "created"})
}

// handleDeleteProjectSecret 删除项目下指定密钥（按明文匹配）。
func (ws *WebServer) handleDeleteProjectSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if ws.requireAdmin(w, r) == nil {
		return
	}
	var req struct {
		SecretKey string `json:"secret_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if err := ws.svc.DeleteProjectSecret(r.Context(), project, req.SecretKey); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Info().Str("project", project).Msg("project secret deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// 健康检查
// ============================================================

func (ws *WebServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ============================================================
// Nodes API
// ============================================================

func (ws *WebServer) handleListNodes(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	nodes, err := ws.svc.ListNodes(r.Context(), project, r.URL.Query().Get("namespace"), r.URL.Query().Get("tag"), onlyEnabledParam(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		nodes = []*workflow.NodeDef{}
	}
	// 注入每个 node 内各 activity 的心跳存活信息（按所选环境隔离），供前端聚合显示。
	// 未选择具体环境时不注入：跨环境聚合会产生误导性结果，前端也不展示。
	env := r.URL.Query().Get("env")
	if ws.collector != nil && env != "" {
		for _, n := range nodes {
			acts := extractNodeActivities(n.Configuration)
			if len(acts) == 0 {
				continue
			}
			hbs := make([]*workflow.NodeActivityHeartbeat, 0, len(acts))
			for _, a := range acts {
				ratio, count := ws.collector.HeartbeatRatio(project, env, a.ActNamespace, a.ActName)
				hbs = append(hbs, &workflow.NodeActivityHeartbeat{
					ActNamespace: a.ActNamespace,
					ActName:      a.ActName,
					Ratio:        ratio,
					Count:        count,
				})
			}
			n.NodeHeartbeats = hbs
		}
	}
	writeJSON(w, http.StatusOK, nodes)
}

// nodeActivityRef 描述 node 配置中引用的一个 activity（命名空间 + 名称）。
type nodeActivityRef struct {
	ActNamespace string
	ActName      string
}

// extractNodeActivities 从节点配置中解析出该节点编排引用的所有 activity。
// 兼容多种历史/现结构：node_config.activities（二维数组）、node_config.stages（二维数组）、
// 扁平 activities 数组（mode 忽略，全部当作串行）、node_config.act_namespace+act_name（单 activity 回退）。
func extractNodeActivities(cfgJSON json.RawMessage) []nodeActivityRef {
	if len(cfgJSON) == 0 {
		return nil
	}
	var cfg struct {
		NodeConfig struct {
			Activities [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"activities"`
			Stages [][]struct {
				ActNamespace string `json:"act_namespace"`
				ActName      string `json:"act_name"`
			} `json:"stages"`
			ActNamespace string `json:"act_namespace"`
			ActName      string `json:"act_name"`
		} `json:"node_config"`
		Activities []struct {
			ActNamespace string `json:"act_namespace"`
			ActName      string `json:"act_name"`
		} `json:"activities"`
	}
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []nodeActivityRef
	add := func(ns, nm string) {
		if ns == "" || nm == "" {
			return
		}
		key := ns + "|" + nm
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, nodeActivityRef{ActNamespace: ns, ActName: nm})
	}
	if len(cfg.NodeConfig.Activities) > 0 {
		for _, stage := range cfg.NodeConfig.Activities {
			for _, a := range stage {
				add(a.ActNamespace, a.ActName)
			}
		}
	} else if len(cfg.NodeConfig.Stages) > 0 {
		for _, stage := range cfg.NodeConfig.Stages {
			for _, a := range stage {
				add(a.ActNamespace, a.ActName)
			}
		}
	} else if len(cfg.Activities) > 0 {
		for _, a := range cfg.Activities {
			add(a.ActNamespace, a.ActName)
		}
	} else if cfg.NodeConfig.ActNamespace != "" && cfg.NodeConfig.ActName != "" {
		add(cfg.NodeConfig.ActNamespace, cfg.NodeConfig.ActName)
	}
	return out
}

func (ws *WebServer) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.NodeDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	if def.NodeID == "" {
		id, err := ws.svc.GenerateNodeID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate node_id: "+err.Error())
			return
		}
		def.NodeID = id
	}
	if def.Status == 0 {
		def.Status = 1
	}
	if err := ws.svc.RegisterNode(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("node_id", def.NodeID).Msg("node created via web")
	writeJSON(w, http.StatusCreated, def)
}

func (ws *WebServer) handleGetNode(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	def, err := ws.svc.GetNode(r.Context(), project, nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.NodeDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	def.NodeID = nodeID
	if err := ws.svc.UpdateNode(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("node_id", nodeID).Msg("node updated via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteNode(r.Context(), project, nodeID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("node_id", nodeID).Msg("node deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// SubChains API
// ============================================================

func (ws *WebServer) handleListSubChains(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	chains, err := ws.svc.ListSubChains(r.Context(), project, onlyEnabledParam(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if chains == nil {
		chains = []*workflow.SubChainDef{}
	}
	writeJSON(w, http.StatusOK, chains)
}

func (ws *WebServer) handleCreateSubChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.SubChainDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	if def.ChainID == "" {
		id, err := ws.svc.GenerateSubChainID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate chain_id: "+err.Error())
			return
		}
		def.ChainID = id
	}
	if def.Status == 0 {
		def.Status = 1
	}
	if err := ws.svc.RegisterSubChain(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", def.ChainID).Msg("sub chain created via web")
	writeJSON(w, http.StatusCreated, def)
}

func (ws *WebServer) handleGetSubChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	def, err := ws.svc.GetSubChain(r.Context(), project, chainID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleUpdateSubChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.SubChainDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	def.ChainID = chainID
	if err := ws.svc.UpdateSubChain(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Msg("sub chain updated via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteSubChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteSubChain(r.Context(), project, chainID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Msg("sub chain deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (ws *WebServer) handleExecuteSubChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req struct {
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Payload == "" {
		req.Payload = "{}"
	}
	result, err := ws.svc.LoadAndExecuteSubChain(r.Context(), project, chainID, req.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = ws.svc.UnloadChain(r.Context(), project, chainID)
	log.Info().Str("project", project).Str("chain_id", chainID).Msg("sub chain executed via web")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project":     project,
		"chain_id":    chainID,
		"result":      result,
		"use_release": false,
	})
}

// ============================================================
// RootChains API
// ============================================================

func (ws *WebServer) handleListRootChains(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	chains, err := ws.svc.ListRootChains(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if chains == nil {
		chains = []*workflow.RootChainDef{}
	}
	writeJSON(w, http.StatusOK, chains)
}

func (ws *WebServer) handleDeleteRootChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteRootChain(r.Context(), project, chainID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Msg("root chain deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (ws *WebServer) handleSaveRootChain(w http.ResponseWriter, r *http.Request) {
	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		req.Project = projectParam(r)
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}

	buildReq := &workflow.BuildRequest{
		Project:            req.Project,
		ChainID:            req.ChainID,
		ChainKey:           req.ChainKey,
		ChainName:          req.ChainName,
		NodeIDs:            req.NodeIDs,
		SubChainIDs:        req.SubChainIDs,
		Connections:        req.Connections,
		DebugMode:          req.DebugMode,
		NodeParamOverrides: req.NodeParamOverrides,
	}

	def, err := ws.svc.SaveRootChain(r.Context(), buildReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info().Str("project", def.Project).Str("chain_id", def.ChainID).Msg("root chain saved via web")
	writeJSON(w, http.StatusOK, def)
}

// handleCreateRootChain 仅录入 Root Chain 基本信息（dsl 留空），用于编排前先建草稿记录
// POST /api/root-chains/create  body: { project, chain_key, name, description, status }
func (ws *WebServer) handleCreateRootChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Project     string `json:"project"`
		ChainKey    string `json:"chain_key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      int    `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	project := projectParam(r)
	if project == "" {
		project = strings.TrimSpace(req.Project)
	}
	if project == "" {
		writeError(w, http.StatusBadRequest, "project required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	def, err := ws.svc.CreateRootChain(r.Context(), project, strings.TrimSpace(req.ChainKey), strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", def.Project).Str("chain_id", def.ChainID).Msg("root chain created via web")
	writeJSON(w, http.StatusOK, def)
}

// ============================================================
// SubChains 编排构建
// ============================================================

type buildSubChainRequest struct {
	Project     string                   `json:"project"`
	ChainID     string                   `json:"chain_id"`
	ChainName   string                   `json:"chain_name"`
	Description string                   `json:"description"`
	NodeIDs     []string                 `json:"node_ids"`
	SubChainIDs []string                 `json:"sub_chain_ids"`
	Connections []workflow.ConnectionDef `json:"connections"`
	DebugMode   bool                     `json:"debug_mode"`
}

func (r *buildSubChainRequest) toBuildRequest(project string) *workflow.BuildSubChainRequest {
	return &workflow.BuildSubChainRequest{
		Project:     project,
		ChainID:     r.ChainID,
		ChainName:   r.ChainName,
		Description: r.Description,
		NodeIDs:     r.NodeIDs,
		SubChainIDs: r.SubChainIDs,
		Connections: r.Connections,
		DebugMode:   r.DebugMode,
	}
}

// handleBuildSubChain 编排方式创建子链（chain_id 自动生成）。
func (ws *WebServer) handleBuildSubChain(w http.ResponseWriter, r *http.Request) {
	var req buildSubChainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		req.Project = projectParam(r)
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.ChainName == "" {
		writeError(w, http.StatusBadRequest, "chain_name is required")
		return
	}
	def, err := ws.svc.CreateSubChainBuild(r.Context(), req.toBuildRequest(req.Project))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", def.Project).Str("chain_id", def.ChainID).Msg("sub chain built via web")
	writeJSON(w, http.StatusOK, def)
}

// handleUpdateSubChainBuild 编排方式更新子链 DSL（保留原 chain_id）。
func (ws *WebServer) handleUpdateSubChainBuild(w http.ResponseWriter, r *http.Request) {
	chainID := r.PathValue("chain_id")
	var req buildSubChainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		req.Project = projectParam(r)
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.ChainName == "" {
		writeError(w, http.StatusBadRequest, "chain_name is required")
		return
	}
	req.ChainID = chainID
	def, err := ws.svc.UpdateSubChainBuild(r.Context(), req.toBuildRequest(req.Project))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", def.Project).Str("chain_id", def.ChainID).Msg("sub chain re-built via web")
	writeJSON(w, http.StatusOK, def)
}

// ============================================================
// RootChains 发布与回滚
// ============================================================

func (ws *WebServer) handlePublishRootChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	release, err := ws.svc.PublishRootChain(r.Context(), project, chainID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Int("version", release.Version).Msg("root chain published via web")
	writeJSON(w, http.StatusOK, release)
}

func (ws *WebServer) handleListRootChainReleases(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	releases, err := ws.svc.ListRootChainReleases(r.Context(), project, chainID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if releases == nil {
		releases = []*workflow.RootChainReleaseDef{}
	}
	writeJSON(w, http.StatusOK, releases)
}

type rollbackRequest struct {
	Version int `json:"version"`
}

func (ws *WebServer) handleRollbackRootChain(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "version must be positive")
		return
	}
	release, err := ws.svc.RollbackRootChain(r.Context(), project, chainID, req.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Int("version", req.Version).Msg("root chain rolled back via web")
	writeJSON(w, http.StatusOK, release)
}

func (ws *WebServer) handleListCurrentReleases(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	releases, err := ws.svc.ListCurrentReleases(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if releases == nil {
		releases = []*workflow.RootChainReleaseDef{}
	}
	writeJSON(w, http.StatusOK, releases)
}

func (ws *WebServer) handleSetCurrentRelease(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "version must be positive")
		return
	}
	release, err := ws.svc.SetCurrentRelease(r.Context(), project, chainID, req.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Int("version", req.Version).Msg("root chain current release set via web")
	writeJSON(w, http.StatusOK, release)
}

func (ws *WebServer) handleDeleteRootChainRelease(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	chainID := r.PathValue("chain_id")
	versionStr := r.PathValue("version")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	version, err := strconv.Atoi(versionStr)
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, "invalid version")
		return
	}
	if err := ws.svc.DeleteRootChainRelease(r.Context(), project, chainID, version); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("chain_id", chainID).Int("version", version).Msg("root chain release deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// Workflow 执行
// ============================================================

type executeRequest struct {
	Project            string                            `json:"project"`
	ChainID            string                            `json:"chain_id"`
	ChainKey           string                            `json:"chain_key"`
	ChainName          string                            `json:"chain_name"`
	NodeIDs            []string                          `json:"node_ids"`
	TraceId            string                            `json:"trace_id"`
	SubChainIDs        []string                          `json:"sub_chain_ids"`
	Connections        []workflow.ConnectionDef          `json:"connections"`
	Payload            json.RawMessage                   `json:"payload"`
	DebugMode          bool                              `json:"debug_mode"`
	UseRelease         bool                              `json:"use_release"`
	EnvName            string                            `json:"env_name"`
	NodeParamOverrides map[string]map[string]interface{} `json:"node_param_overrides"`
}

// parsePayload 将请求中的 payload 解析为 map。
// 兼容两种格式：JSON 对象（新）与 JSON 字符串包裹的 JSON 对象（旧）。
func parsePayload(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return map[string]any{}, nil
	}
	// 先尝试直接解析为对象
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		return m, nil
	}
	// 兼容旧格式：payload 是 JSON 字符串，先解出字符串再解析
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s == "" {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (ws *WebServer) handleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.ChainID == "" && req.ChainKey == "" {
		writeError(w, http.StatusBadRequest, "chain_id or chain_key is required")
		return
	}
	// 解析 payload（兼容 JSON 对象与旧版 JSON 字符串两种格式）
	payloadMap, err := parsePayload(req.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse payload failed: "+err.Error())
		return
	}
	// 执行环境为必填：后台执行时按环境将数据打入对应的 Redis（节点日志、Activity 结果等）
	if req.EnvName == "" {
		writeError(w, http.StatusBadRequest, "env_name is required")
		return
	}
	redisCfg, err := ws.svc.GetRedisConnect(r.Context(), req.Project, req.EnvName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "resolve redis config failed: "+err.Error())
		return
	}

	// 若仅提供了 ChainKey，先解析出真实的 ChainID（ChainID 与 ChainKey 均可用于调用主链）
	if req.ChainID == "" && req.ChainKey != "" {
		def, err := ws.svc.GetRootChainByKey(r.Context(), req.Project, req.ChainKey)
		if err != nil {
			writeError(w, http.StatusNotFound, "chain_key not found: "+err.Error())
			return
		}
		req.ChainID = def.ChainID
	}

	// 生产模式：执行当前发布版本（忽略编排配置，使用发布快照）
	if req.UseRelease {
		//chainID 必须为release中chainId随机生成的那个，不然如果测试时，就会把正式的利用 ws.svc.UnloadChain 卸载掉。

		payloadStr, _ := json.Marshal(payloadMap)
		result, err := ws.svc.ExecutePublishedRootChain(r.Context(), req.Project, req.ChainID, string(payloadStr), req.EnvName, redisCfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = ws.svc.UnloadChain(r.Context(), req.Project, req.ChainID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"project":     req.Project,
			"chain_id":    req.ChainID,
			"result":      result,
			"use_release": true,
			"env_name":    req.EnvName,
		})
		return
	}

	// 草稿模式：按 ChainID 查 root chain DSL，解析后通过 ExecuteRootChainByID 执行
	def, err := ws.svc.GetRootChain(r.Context(), req.Project, req.ChainID)
	if err != nil {
		writeError(w, http.StatusNotFound, "root chain not found: "+err.Error())
		return
	}

	var ruleChain types.RuleChain
	if err := json.Unmarshal([]byte(def.DSLJSON), &ruleChain); err != nil {
		writeError(w, http.StatusInternalServerError, "parse root chain dsl failed: "+err.Error())
		return
	}

	req.TraceId = id.GetUUID(req.TraceId)
	result, err := ws.svc.ExecuteRootChainByID(r.Context(), &ruleChain, payloadMap, req.Project, req.EnvName, req.TraceId)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project":  req.Project,
		"chain_id": req.ChainID,
		"trace_id": req.TraceId,
		"result":   result,
		"env_name": req.EnvName,
	})
}

// ============================================================
// TestCase API
// ============================================================

type testCaseRequest struct {
	Project            string `json:"project"`
	CaseID             string `json:"case_id,omitempty"`
	OwnerID            string `json:"owner_id"`
	OwnerType          string `json:"owner_type"` // root / sub
	Name               string `json:"name"`
	ChainID            string `json:"chain_id"`
	ChainName          string `json:"chain_name,omitempty"`
	NodeIDs            string `json:"node_ids,omitempty"`
	SubChainIDs        string `json:"sub_chain_ids,omitempty"`
	ConnectionsData    string `json:"connections_data,omitempty"`
	Payload            string `json:"payload,omitempty"`
	DebugMode          bool   `json:"debug_mode,omitempty"`
	UseRelease         bool   `json:"use_release,omitempty"`
	NodeParamOverrides string `json:"node_param_overrides,omitempty"`
	LastResult         string `json:"last_result,omitempty"`
}

func (r *testCaseRequest) toDef(project string) *workflow.TestCaseDef {
	return &workflow.TestCaseDef{
		Project:            project,
		CaseID:             r.CaseID,
		OwnerID:            r.OwnerID,
		OwnerType:          r.OwnerType,
		Name:               r.Name,
		ChainID:            r.ChainID,
		ChainName:          r.ChainName,
		NodeIDs:            r.NodeIDs,
		SubChainIDs:        r.SubChainIDs,
		ConnectionsData:    r.ConnectionsData,
		Payload:            r.Payload,
		DebugMode:          r.DebugMode,
		UseRelease:         r.UseRelease,
		NodeParamOverrides: r.NodeParamOverrides,
		LastResult:         r.LastResult,
	}
}

func (ws *WebServer) handleListTestCases(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	ownerID := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if ownerID == "" {
		writeError(w, http.StatusBadRequest, "owner_id query parameter is required")
		return
	}
	cases, err := ws.svc.ListTestCases(r.Context(), project, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cases == nil {
		cases = []*workflow.TestCaseDef{}
	}
	writeJSON(w, http.StatusOK, cases)
}

func (ws *WebServer) handleGetTestCase(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	caseID := r.PathValue("case_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	c, err := ws.svc.GetTestCase(r.Context(), project, caseID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (ws *WebServer) handleSaveTestCase(w http.ResponseWriter, r *http.Request) {
	var req testCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		req.Project = projectParam(r)
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.OwnerID == "" || req.OwnerType == "" || req.ChainID == "" {
		writeError(w, http.StatusBadRequest, "owner_id, owner_type and chain_id are required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	def, err := ws.svc.SaveTestCase(r.Context(), req.toDef(req.Project))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", def.Project).Str("case_id", def.CaseID).Str("owner_id", def.OwnerID).Msg("test case saved via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteTestCase(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	caseID := r.PathValue("case_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteTestCase(r.Context(), project, caseID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("case_id", caseID).Msg("test case deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// EnvConfig API（项目级环境配置：环境变量 / Redis / MySQL）
// ============================================================

type envConfigRequest struct {
	Project     string                `json:"project"`
	EnvName     string                `json:"env_name"`
	Description string                `json:"description,omitempty"`
	EnvVars     []workflow.EnvVar     `json:"env_vars,omitempty"`
	RedisConfig *workflow.RedisConfig `json:"redis_config,omitempty"`
	MySQLConfig *workflow.MySQLConfig `json:"mysql_config,omitempty"`
}

func (r *envConfigRequest) toDef(project string) *workflow.EnvConfigDef {
	return &workflow.EnvConfigDef{
		Project:     project,
		EnvName:     r.EnvName,
		Description: r.Description,
		EnvVars:     r.EnvVars,
		RedisConfig: r.RedisConfig,
		MySQLConfig: r.MySQLConfig,
	}
}

func (ws *WebServer) handleListEnvConfigs(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	cfgs, err := ws.svc.ListEnvConfigs(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfgs == nil {
		cfgs = []*workflow.EnvConfigDef{}
	}
	writeJSON(w, http.StatusOK, cfgs)
}

func (ws *WebServer) handleGetEnvConfig(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	envName := r.PathValue("env_name")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	c, err := ws.svc.GetEnvConfig(r.Context(), project, envName)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (ws *WebServer) handleSaveEnvConfig(w http.ResponseWriter, r *http.Request) {
	var req envConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		req.Project = projectParam(r)
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.EnvName == "" {
		writeError(w, http.StatusBadRequest, "env_name is required")
		return
	}
	def, err := ws.svc.SaveEnvConfig(r.Context(), req.toDef(req.Project))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", def.Project).Str("env_name", def.EnvName).Msg("env config saved via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteEnvConfig(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	envName := r.PathValue("env_name")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteEnvConfig(r.Context(), project, envName); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("env_name", envName).Msg("env config deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// 单节点测试（MQ 分布式执行）
// ============================================================

func (ws *WebServer) handleTestNode(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req service.TestNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	req.Project = project
	req.NodeID = nodeID
	if req.InputParams == nil {
		req.InputParams = map[string]any{}
	}
	if r.URL.Query().Get("save_record") == "false" {
		req.SaveRecord = false
	} else {
		req.SaveRecord = true
	}
	result, err := ws.svc.TestNode(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (ws *WebServer) handleListNodeTestRecords(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	records, err := ws.svc.ListNodeTestRecords(r.Context(), project, nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []*workflow.NodeTestRecordDef{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (ws *WebServer) handleDeleteNodeTestRecord(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	recordID := r.PathValue("record_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteNodeTestRecord(r.Context(), project, recordID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (ws *WebServer) handleClearNodeTestRecords(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	deleted, err := ws.svc.ClearNodeTestRecords(r.Context(), project, nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "cleared", "deleted": deleted})
}

// ============================================================
// 辅助函数
// ============================================================

// ============================================================
// Activity API
// ============================================================

func (ws *WebServer) handleListActivities(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	env := r.URL.Query().Get("env")
	activities, err := ws.svc.ListActivities(r.Context(), project, r.URL.Query().Get("tag"), env)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if activities == nil {
		activities = []*workflow.ActivityDef{}
	}
	// 注入心跳存活比例（由收集器实时计算，按所选环境隔离），供前端进度条展示。
	// 未选择具体环境时不注入：跨环境聚合会产生误导性结果，前端也不展示。
	if ws.collector != nil && env != "" {
		for _, a := range activities {
			ratio, count := ws.collector.HeartbeatRatio(project, env, a.ActNamespace, a.ActName)
			a.Heartbeat = &workflow.ActivityHeartbeatInfo{Ratio: ratio, Count: count}
		}
	}
	writeJSON(w, http.StatusOK, activities)
}

// handleListActivityLogs 查询指定 activity 的执行日志，支持按字段过滤与关键词搜索。
func (ws *WebServer) handleListActivityLogs(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	activityID := r.PathValue("activity_id")
	if activityID == "" {
		writeError(w, http.StatusBadRequest, "activity_id is required")
		return
	}
	q := r.URL.Query()
	filter := &workflow.ActivityLogFilter{
		Level:        q.Get("level"),
		ActNamespace: q.Get("act_namespace"),
		ActName:      q.Get("act_name"),
		EventID:      q.Get("event_id"),
		Env:          q.Get("env"),
		Keyword:      q.Get("keyword"),
		RootChainID:  q.Get("root_chain_id"),
		TraceID:      q.Get("trace_id"),
		SpanID:       q.Get("span_id"),
	}
	if v := q.Get("start"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			filter.Start = n
		}
	}
	if v := q.Get("end"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			filter.End = n
		}
	}
	// 分页：page 从 1 开始，page_size 默认 50，最大 1000
	page := 1
	pageSize := 50
	if v := q.Get("page"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("page_size"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			pageSize = n
		}
	}
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize
	// activity_id 是模板 ID，日志按 act_name 关联；解析出 act_name 作为当前 activity 限定条件。
	actName := activityID
	if filter.ActName != "" {
		actName = filter.ActName
	} else if act, gerr := ws.svc.GetActivity(r.Context(), project, activityID); gerr == nil {
		actName = act.ActName
	}
	logs, total, err := ws.svc.ListActivityLogs(r.Context(), project, actName, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []*workflow.ActivityLogDef{}
	}
	writeJSON(w, http.StatusOK, workflow.ActivityLogPage{
		List:     logs,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// handleListActivityLogsGlobal 跨 Activity 查询执行日志（不限定单个 activity），
// 常用于按 trace_id 查看某次执行链路涉及的所有 activity 记录。
func (ws *WebServer) handleListActivityLogsGlobal(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	q := r.URL.Query()
	traceID := strings.TrimSpace(q.Get("trace_id"))
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace_id query parameter is required")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	filter := &workflow.ActivityLogFilter{
		Level:       q.Get("level"),
		ActName:     q.Get("act_name"),
		EventID:     q.Get("event_id"),
		Env:         q.Get("env"),
		Keyword:     q.Get("keyword"),
		RootChainID: q.Get("root_chain_id"),
		TraceID:     traceID,
		SpanID:      q.Get("span_id"),
		NodeSpanID:  q.Get("node_span_id"),
	}
	if v := q.Get("start"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			filter.Start = n
		}
	}
	if v := q.Get("end"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			filter.End = n
		}
	}
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize
	// actName 留空 → 查询该 project 下所有 activity 的日志（跨 activity）
	logs, total, err := ws.svc.ListActivityLogs(r.Context(), project, "", filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []*workflow.ActivityLogDef{}
	}
	writeJSON(w, http.StatusOK, workflow.ActivityLogPage{
		List:     logs,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (ws *WebServer) handleCreateActivity(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.ActivityDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	if def.Status == 0 {
		def.Status = 1
	}
	if err := ws.svc.CreateActivity(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("activity_id", def.ActivityID).Msg("activity created via web")
	writeJSON(w, http.StatusCreated, def)
}

func (ws *WebServer) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	activityID := r.PathValue("activity_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	def, err := ws.svc.GetActivity(r.Context(), project, activityID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleUpdateActivity(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	activityID := r.PathValue("activity_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var def workflow.ActivityDef
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	def.Project = project
	def.ActivityID = activityID
	if err := ws.svc.UpdateActivity(r.Context(), &def); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("activity_id", activityID).Msg("activity updated via web")
	writeJSON(w, http.StatusOK, def)
}

func (ws *WebServer) handleDeleteActivity(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	activityID := r.PathValue("activity_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteActivity(r.Context(), project, activityID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("project", project).Str("activity_id", activityID).Msg("activity deleted via web")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// ============================================================
// Activity 测试（MQ 分布式执行）
// ============================================================

func (ws *WebServer) handleTestActivity(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	activityID := r.PathValue("activity_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req service.TestActivityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	req.Project = project
	req.ActivityID = activityID
	if req.InputParams == nil {
		req.InputParams = map[string]any{}
	}
	if r.URL.Query().Get("save_record") == "false" {
		req.SaveRecord = false
	} else {
		req.SaveRecord = true
	}
	result, err := ws.svc.TestActivity(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (ws *WebServer) handleListActivityTestRecords(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	activityID := r.PathValue("activity_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	records, err := ws.svc.ListActivityTestRecords(r.Context(), project, activityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []*workflow.ActivityTestRecordDef{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (ws *WebServer) handleDeleteActivityTestRecord(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	recordID := r.PathValue("record_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if err := ws.svc.DeleteActivityTestRecord(r.Context(), project, recordID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// handleListNodeLogs 查询指定 node 的运行日志（收集器落库 wf_node_logs），按时间倒序分页返回。
func (ws *WebServer) handleListNodeLogs(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	nodeID := r.PathValue("node_id")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node_id path parameter is required")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	// 支持传入 trace_id 进行查询（按 node_id + trace_id 精确关联该 node 在某链路下的运行日志）
	filter := &workflow.NodeLogFilter{
		NodeID:  nodeID,
		TraceID: strings.TrimSpace(r.URL.Query().Get("trace_id")),
		Limit:   pageSize,
		Offset:  (page - 1) * pageSize,
	}
	logs, total, err := ws.svc.ListNodeLogsGlobal(r.Context(), project, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []*workflow.NodeLogDef{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// handleListNodeLogsGlobal 按条件全局查询 node 运行日志（支持 trace_id 回查），分页返回。
func (ws *WebServer) handleListNodeLogsGlobal(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	filter := &workflow.NodeLogFilter{
		Level:    q.Get("level"),
		NodeID:   q.Get("node_id"),
		NodeName: q.Get("node_name"),
		Env:      q.Get("env"),
		TraceID:  strings.TrimSpace(q.Get("trace_id")),
		Keyword:  q.Get("keyword"),
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	}
	logs, total, err := ws.svc.ListNodeLogsGlobal(r.Context(), project, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []*workflow.NodeLogDef{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ============================================================
// 公共响应工具
// ============================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("failed to encode json response")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// ListenAndServe 启动 HTTP 服务（便捷方法）。
func (ws *WebServer) ListenAndServe(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":8080"
	}
	log.Info().Str("addr", addr).Msg("workflow web server starting")
	return http.ListenAndServe(addr, ws.mux)
}
