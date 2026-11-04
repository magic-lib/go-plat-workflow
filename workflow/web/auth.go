package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// sessionCookieName 登录会话 Cookie 名。
const sessionCookieName = "wf_session"

// sessionDuration 会话有效期。
const sessionDuration = 24 * time.Hour

// ctxUserKey 用于把当前登录用户存入 request context 的 key。
type ctxUserKey struct{}

// 登录失败限流：同一「用户名 + 客户端 IP」在 loginFailWindow 内失败 loginFailLimit 次后，锁定到窗口结束。
// 按组合计数而不是单按用户名：单按用户名的话，任何人连续输错就能把 admin 锁住。
// 计数在进程内，多副本时每个副本各自计数；条目数设上限，防止用随机用户名/IP 把内存撑大。
const (
	loginFailLimit      = 5
	loginFailWindow     = 15 * time.Minute
	loginFailMaxEntries = 10000
)

type loginFailEntry struct {
	count int
	first time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*loginFailEntry
}

var loginFails = &loginLimiter{entries: map[string]*loginFailEntry{}}

// blocked 返回 keys 中是否有任一个已达失败上限（顺带清理过期计数）。
func (l *loginLimiter) blocked(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, e := range l.entries {
		if now.Sub(e.first) > loginFailWindow {
			delete(l.entries, k)
		}
	}
	for _, k := range keys {
		if e, ok := l.entries[k]; ok && e.count >= loginFailLimit {
			return true
		}
	}
	return false
}

func (l *loginLimiter) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if e, ok := l.entries[k]; ok {
			e.count++
		} else if len(l.entries) < loginFailMaxEntries {
			l.entries[k] = &loginFailEntry{count: 1, first: time.Now()}
		}
	}
}

func (l *loginLimiter) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.entries, k)
	}
}

// clientIP 取客户端 IP：优先 X-Forwarded-For 第一个地址（服务部署在网关之后），否则取 RemoteAddr。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// isHTTPS 判断请求是否经 HTTPS 到达（直连 TLS 或网关透传 X-Forwarded-Proto），用于决定 Cookie 是否加 Secure。
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// currentUserSafe 取当前登录用户：优先取中间件注入到 context 的用户；
// 取不到时（如未经 authMiddleware 直接调用 mux 的测试代码）从会话 Cookie 回查兜底。
func (ws *WebServer) currentUserSafe(r *http.Request) *models.UserModel {
	if u, ok := r.Context().Value(ctxUserKey{}).(*models.UserModel); ok {
		if u != nil {
			return u
		}
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if _, su, serr := ws.svc.UserRepo().GetSession(r.Context(), c.Value); serr == nil && su != nil {
			return su
		}
	}
	return nil
}

func (ws *WebServer) currentUserIsAdmin(r *http.Request) bool {
	return ws.currentUserSafe(r) != nil && ws.currentUserSafe(r).Role == "admin"
}

// genToken 生成随机会话 token。
func genToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// publicAPIMux 免登录的 API 路由（只用于匹配，不处理请求）：
// 登录/健康检查，以及由 handler 自行按项目密钥签名校验的对外接口。
// 用 ServeMux 的路由模式做精确匹配，避免按后缀放行把 /api/xxx/config 一类路径也放过去。
var publicAPIMux = func() *http.ServeMux {
	m := http.NewServeMux()
	noop := func(http.ResponseWriter, *http.Request) {}
	for _, pattern := range []string{
		"POST /api/login",
		"POST /api/logout",
		"GET /api/health",
		"GET /api/version",
		// 对外项目配置查询（secret_key 签名校验）
		"POST /api/projects/{project}/config",
		// 对外 Redis 配置查询与工作流调用（secret_key 签名校验）
		"POST " + workflow.ProjectRedisCfgPath,
		"POST " + workflow.ProjectWorkflowInvoke,
	} {
		m.HandleFunc(pattern, noop)
	}
	return m
}()

// isPublicRequest 判断请求是否免登录：非 /api/ 的静态页面与资源（登录页本身也在其中），以及 publicAPIMux 中的接口。
func isPublicRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	_, pattern := publicAPIMux.Handler(r)
	return pattern != ""
}

// authMiddleware 全局鉴权中间件：
//   - 放行静态页面、登录接口、健康检查，以及按项目密钥签名校验的对外接口。
//   - 其余请求必须携带有效且未过期的 wf_session Cookie。
func (ws *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if isPublicRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "未登录：缺少会话 Cookie，请重新登录")
			return
		}
		_, u, err := ws.svc.UserRepo().GetSession(r.Context(), c.Value)
		if err != nil || u == nil || u.Status != 1 {
			// 无效/过期/被禁用会话：清除 Cookie
			http.SetCookie(w, &http.Cookie{
				Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
			})
			reason := "会话无效或已过期"
			if err != nil {
				reason = "会话校验失败: " + err.Error()
			} else if u == nil {
				reason = "会话对应的用户不存在"
			} else if u.Status != 1 {
				reason = "账号已被禁用"
			}
			log.Warn().Str("path", p).Str("reason", reason).Msg("auth rejected")
			writeError(w, http.StatusUnauthorized, reason)
			return
		}
		// 将当前用户放入 context，供后续 handler 做项目级权限判断
		ctx := context.WithValue(r.Context(), ctxUserKey{}, u)

		// 项目级权限：凡带 ?project= 的 API 请求，校验当前用户是否有权访问该项目。
		// admin 拥有全部项目；普通用户按 wf_user_projects 中的角色授权：
		//   - viewer（只读）：可查日志、执行单元测试等，不可编辑；
		//   - editor（管理）：可编辑该项目所有功能。
		// 用户管理接口（/api/users*）、与项目无关的接口（项目列表、当前用户）不走项目授权：
		// 前端会给所有请求自动拼上地址栏里的 ?project=，该项目无权限（被取消授权、已删除、别人分享的链接）时，
		// 不能连项目列表都加载不出来。
		if strings.HasPrefix(p, "/api/") && !isProjectAgnosticPath(p) {
			if project := r.URL.Query().Get("project"); project != "" {
				if !checkProjectAccessForRequest(ws, w, r.WithContext(ctx), r, project) {
					return
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isProjectAgnosticPath 与具体项目无关、不需要按 ?project= 校验项目权限的接口（handler 内各自做 admin 等校验）。
func isProjectAgnosticPath(p string) bool {
	return strings.HasPrefix(p, "/api/users") ||
		p == "/api/projects" ||
		p == "/api/me" || strings.HasPrefix(p, "/api/me/")
}

// isWriteRequest 判断请求是否为「编辑类」写操作（需 editor/admin）。
// 执行/测试类 POST（如 /test、/execute、workflow/execute）对 viewer 放行（单元测试）。
func isWriteRequest(r *http.Request) bool {
	m := r.Method
	if m == "PUT" || m == "DELETE" {
		return true
	}
	if m != "POST" {
		return false
	}
	// 执行/测试类 POST 放行 viewer（单元测试只读权限可执行）
	p := r.URL.Path
	if p == "/api/workflow/execute" ||
		strings.HasSuffix(p, "/test") ||
		strings.HasSuffix(p, "/execute") {
		return false
	}
	// 其余 POST 视为写操作
	return true
}

// projectRoleOf 返回当前用户对 project 的角色：admin -> "admin"；普通用户查授权表，未授权返回空。
func (ws *WebServer) projectRoleOf(r *http.Request, project string) (string, error) {
	u := ws.currentUserSafe(r)
	if u == nil {
		return "", nil
	}
	if u.Role == "admin" {
		return "admin", nil
	}
	return ws.svc.UserRepo().GetProjectRole(r.Context(), u.ID, project)
}

// checkProjectAccessForRequest 按请求类型校验项目权限：读操作 viewer 即可，写操作需 editor/admin。
func checkProjectAccessForRequest(ws *WebServer, w http.ResponseWriter, r *http.Request, req *http.Request, project string) bool {
	role, err := ws.projectRoleOf(req, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if role == "" {
		writeError(w, http.StatusForbidden, "no permission for project: "+project)
		return false
	}
	if isWriteRequest(req) && role != "admin" && role != "editor" {
		writeError(w, http.StatusForbidden, "viewer 只读权限，不可编辑该项目: "+project)
		return false
	}
	return true
}

// requireAdmin 校验当前用户是否为 admin，否则返回 403。
// 优先取中间件注入到 context 的当前用户；若 context 为空（Go ServeMux
// 在匹配带路径参数的路由时会重置 request context），则回查会话 Cookie，
// 避免 admin 接口误报 "未登录：请求未携带有效会话"。
func (ws *WebServer) requireAdmin(w http.ResponseWriter, r *http.Request) *models.UserModel {
	u := ws.currentUserSafe(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return nil
	}
	isAdmin := ws.currentUserIsAdmin(r)
	if !isAdmin {
		writeError(w, http.StatusForbidden, "admin only")
		return nil
	}
	return u
}

// requireProjectAccess 校验当前用户是否可访问指定 project：读操作 viewer 即可，写操作需 editor/admin。
// 用于 project 在 path 中（不走中间件 ?project= 校验）的接口，如修改、删除项目。
func (ws *WebServer) requireProjectAccess(w http.ResponseWriter, r *http.Request, project string) bool {
	if ws.currentUserSafe(r) == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return false
	}
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return false
	}
	return checkProjectAccessForRequest(ws, w, r, r, project)
}

// resolveProject 统一确定请求操作的 project，并保证该 project 做过权限校验：
//   - 以 URL query 的 ?project= 为准（中间件已按它校验过权限）；
//   - body 里也带了 project 且与 query 不一致时拒绝，避免「用 A 的权限改 B」；
//   - query 未带时退回 body 里的 project，并在这里补做权限校验。
//
// 返回 ok=false 时已写出错误响应。project 为空时返回 ("", true)，由调用方决定是否必填。
func (ws *WebServer) resolveProject(w http.ResponseWriter, r *http.Request, bodyProject string) (string, bool) {
	q := projectParam(r)
	bodyProject = strings.TrimSpace(bodyProject)
	if q != "" {
		if bodyProject != "" && bodyProject != q {
			writeError(w, http.StatusBadRequest, "project in body does not match project query parameter")
			return "", false
		}
		return q, true
	}
	if bodyProject == "" {
		return "", true
	}
	if !checkProjectAccessForRequest(ws, w, r, r, bodyProject) {
		return "", false
	}
	return bodyProject, true
}

// ============================================================
// 登录 / 登出 / 当前用户
// ============================================================

func (ws *WebServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	limitKeys := []string{req.Username + "|" + clientIP(r)}
	if loginFails.blocked(limitKeys...) {
		writeError(w, http.StatusTooManyRequests, "登录失败次数过多，请 15 分钟后再试")
		return
	}
	// 先校验密码再看账号状态：密码不对时不论账号是否存在、是否禁用都返回同一个错误，避免被用来探测账号
	u, err := ws.svc.UserRepo().GetUserByUsername(r.Context(), req.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		loginFails.fail(limitKeys...)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	loginFails.reset(limitKeys...)
	if u.Status != 1 {
		writeError(w, http.StatusForbidden, "account disabled")
		return
	}
	token, err := genToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ws.svc.UserRepo().CreateSession(r.Context(), &models.UserSessionModel{
		UserID:    u.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(sessionDuration),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 顺带清理过期会话，避免 wf_user_sessions 只增不减（登录频率低，放这里足够）
	if err := ws.svc.UserRepo().DeleteExpiredSessions(r.Context()); err != nil {
		log.Warn().Err(err).Msg("delete expired sessions failed")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionDuration),
		MaxAge:   int(sessionDuration.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"username": u.Username,
		"nickname": u.Nickname,
		"role":     u.Role,
	})
}

func (ws *WebServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = ws.svc.UserRepo().DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (ws *WebServer) handleMe(w http.ResponseWriter, r *http.Request) {
	u := ws.currentUserSafe(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return
	}
	resp := map[string]any{
		"username": u.Username,
		"nickname": u.Nickname,
		"role":     u.Role,
	}
	if u.Role == "admin" {
		resp["projects"] = "all"
	} else {
		projects, err := ws.svc.UserRepo().ListProjectsByUser(r.Context(), u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// 项目 -> 项目级角色（viewer=只读 / editor=管理），前端据此控制编辑入口
		roles, err := ws.svc.UserRepo().ListProjectRolesByUser(r.Context(), u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp["projects"] = projects
		resp["project_roles"] = roles
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleChangeMyPassword 当前登录用户修改自己的密码（需提供旧密码）。
func (ws *WebServer) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	u := ws.currentUserSafe(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.OldPassword == "" {
		writeError(w, http.StatusBadRequest, "旧密码必填")
		return
	}
	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码至少 6 位")
		return
	}
	// 重新从库取最新哈希，避免 context 中的 u 是会话创建时的快照
	full, err := ws.svc.UserRepo().GetUserByID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(full.PasswordHash), []byte(req.OldPassword)) != nil {
		writeError(w, http.StatusBadRequest, "旧密码错误")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 保留其它字段，仅更新 password_hash
	full.PasswordHash = string(newHash)
	if err := ws.svc.UserRepo().UpdateUser(r.Context(), full); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 改密码后让该用户的其它会话失效，只保留当前这个
	var currentToken string
	if c, err := r.Cookie(sessionCookieName); err == nil {
		currentToken = c.Value
	}
	if err := ws.svc.UserRepo().DeleteUserSessions(r.Context(), u.ID, currentToken); err != nil {
		writeError(w, http.StatusInternalServerError, "密码已修改，但清理其它会话失败: "+err.Error())
		return
	}
	log.Info().Uint("user_id", u.ID).Msg("user changed own password")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ============================================================
// 用户管理（仅 admin）
// ============================================================

func (ws *WebServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if ws.requireAdmin(w, r) == nil {
		return
	}
	users, err := ws.svc.UserRepo().ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []*models.UserModel{}
	}
	// 附带每个用户授权的项目列表（admin 不展示）
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		item := map[string]any{
			"id":       u.ID,
			"username": u.Username,
			"nickname": u.Nickname,
			"role":     u.Role,
			"status":   u.Status,
		}
		if u.Role != "admin" {
			projects, _ := ws.svc.UserRepo().ListProjectsByUser(r.Context(), u.ID)
			roles, _ := ws.svc.UserRepo().ListProjectRolesByUser(r.Context(), u.ID)
			item["projects"] = projects
			item["project_roles"] = roles
		} else {
			item["projects"] = "all"
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// userUpsertReq 创建/更新用户请求。
// Projects 为项目名 -> 项目级角色（viewer=只读 / editor=管理）的映射。
type userUpsertReq struct {
	Username string            `json:"username"`
	Password string            `json:"password"`
	Nickname string            `json:"nickname"`
	Role     string            `json:"role"`
	Status   int8              `json:"status"`
	Projects map[string]string `json:"projects"`
}

func (ws *WebServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	admin := ws.requireAdmin(w, r)
	if admin == nil {
		return
	}
	var req userUpsertReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}
	role := req.Role
	if role != "admin" && role != "viewer" {
		role = "viewer"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := req.Status
	if status == 0 {
		status = 1
	}
	id, err := ws.svc.UserRepo().CreateUser(r.Context(), &models.UserModel{
		Username:     req.Username,
		PasswordHash: string(hash),
		Nickname:     req.Nickname,
		Role:         role,
		Status:       status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 授权项目绑定（project -> role）
	for project, role := range req.Projects {
		_ = ws.svc.UserRepo().BindProject(r.Context(), id, project, role)
	}
	log.Info().Str("admin", admin.Username).Str("username", req.Username).Msg("user created")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "username": req.Username})
}

func (ws *WebServer) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	admin := ws.requireAdmin(w, r)
	if admin == nil {
		return
	}
	uid := atou(r.PathValue("user_id"))
	if uid == 0 {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	existing, err := ws.svc.UserRepo().GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var req userUpsertReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Password != "" {
		hash, e := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if e != nil {
			writeError(w, http.StatusInternalServerError, e.Error())
			return
		}
		existing.PasswordHash = string(hash)
	}
	if req.Nickname != "" {
		existing.Nickname = req.Nickname
	}
	if req.Role == "admin" || req.Role == "viewer" {
		existing.Role = req.Role
	}
	if req.Status != 0 {
		existing.Status = req.Status
	}
	if err := ws.svc.UserRepo().UpdateUser(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 管理员重置了密码：该用户已有的会话全部失效（重置的是自己时保留当前会话，避免把自己踢下线）
	if req.Password != "" {
		var keepToken string
		if uid == admin.ID {
			if c, err := r.Cookie(sessionCookieName); err == nil {
				keepToken = c.Value
			}
		}
		if err := ws.svc.UserRepo().DeleteUserSessions(r.Context(), uid, keepToken); err != nil {
			writeError(w, http.StatusInternalServerError, "用户已更新，但清理会话失败: "+err.Error())
			return
		}
	}
	// 当传入 projects 字段时，重建授权关系（先解绑全部再按角色绑定）
	if req.Projects != nil {
		old, _ := ws.svc.UserRepo().ListProjectsByUser(r.Context(), uid)
		for _, p := range old {
			_ = ws.svc.UserRepo().UnbindProject(r.Context(), uid, p)
		}
		for project, role := range req.Projects {
			_ = ws.svc.UserRepo().BindProject(r.Context(), uid, project, role)
		}
	}
	log.Info().Str("admin", admin.Username).Uint("user_id", uid).Msg("user updated")
	writeJSON(w, http.StatusOK, map[string]any{"id": uid})
}

func (ws *WebServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin := ws.requireAdmin(w, r)
	if admin == nil {
		return
	}
	uid := atou(r.PathValue("user_id"))
	if uid == 0 {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if uid == admin.ID {
		writeError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	if err := ws.svc.UserRepo().DeleteUser(r.Context(), uid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("admin", admin.Username).Uint("user_id", uid).Msg("user deleted")
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// handleBindUserProject 为指定用户绑定/解绑项目（仅 admin）。
func (ws *WebServer) handleBindUserProject(w http.ResponseWriter, r *http.Request) {
	admin := ws.requireAdmin(w, r)
	if admin == nil {
		return
	}
	uid := atou(r.PathValue("user_id"))
	if uid == 0 {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	var req struct {
		Project string `json:"project"`
		Role    string `json:"role"` // viewer=只读 editor=管理（绑定时生效）
		Bind    bool   `json:"bind"` // true=绑定 false=解绑
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if req.Bind {
		if err := ws.svc.UserRepo().BindProject(r.Context(), uid, req.Project, req.Role); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := ws.svc.UserRepo().UnbindProject(r.Context(), uid, req.Project); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	log.Info().Str("admin", admin.Username).Uint("user_id", uid).
		Str("project", req.Project).Bool("bind", req.Bind).Msg("user-project binding updated")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// ============================================================
// 小工具
// ============================================================

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func atou(s string) uint {
	var n uint
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint(c-'0')
	}
	return n
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
