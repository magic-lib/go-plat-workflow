package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

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

// currentUserSafe 取当前登录用户：优先取中间件注入到 context 的用户；
// 若为 nil（Go 1.22 ServeMux 在匹配带路径参数的路由时会重置 request context，
// 导致中间件注入 context 的用户丢失），则从会话 Cookie 回查兜底，
// 避免 admin 被误判为普通用户（isAdmin=false）。
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

// authMiddleware 全局鉴权中间件：
//   - 放行登录接口、健康检查、对外项目配置查询（secret_key）与静态首页。
//   - 其余请求必须携带有效且未过期的 wf_session Cookie。
func (ws *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// 放行白名单
		if p == "/api/login" || p == "/api/health" || p == "/api/version" || p == "/api/logout" {
			next.ServeHTTP(w, r)
			return
		}
		// 对外配置查询：POST /api/projects/{project}/config 使用 secret_key 校验，放行
		if r.Method == "POST" && len(p) > len("/api/projects/") &&
			hasSuffix(p, "/config") {
			next.ServeHTTP(w, r)
			return
		}
		// 对外 Redis 配置查询：POST /api/projects/{project}/redis-config 使用 secret_key 校验，放行
		if r.Method == "POST" && len(p) > len("/api/projects/") &&
			hasSuffix(p, "/redis-config") {
			next.ServeHTTP(w, r)
			return
		}
		// 对外工作流调用：POST /api/workflow/invoke 由外部系统按 project + chain_key 调用，放行
		if r.Method == "POST" && p == "/api/workflow/invoke" {
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
		// 用户管理接口（/api/users*）不走项目授权。
		if strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/api/users") {
			if project := r.URL.Query().Get("project"); project != "" {
				if !checkProjectAccessForRequest(ws, w, r.WithContext(ctx), r, project) {
					return
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

// requireProjectAccess 校验当前用户是否可访问指定 project。
// admin 用户拥有全部项目权限；viewer 需在其 wf_user_projects 授权列表中。
func (ws *WebServer) requireProjectAccess(w http.ResponseWriter, r *http.Request, project string) bool {
	u := ws.currentUserSafe(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "未登录：缺少有效会话，请重新登录")
		return false
	}
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return false
	}
	if u.Role == "admin" {
		return true
	}
	projects, err := ws.svc.UserRepo().ListProjectsByUser(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	for _, p := range projects {
		if p == project {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "no permission for project: "+project)
	return false
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
	u, err := ws.svc.UserRepo().GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if u.Status != 1 {
		writeError(w, http.StatusForbidden, "account disabled")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
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
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
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
