package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// =======================
// 数据库配置
// =======================
// 使用 SQLite 作为本地开发数据库（无需安装 MySQL）
const (
	DBDriver   = "sqlite3"
	DBFilePath = "./workflow.db"
	ServerPort = ":8080"
)

// ========================
// 数据模型
// ========================

// Workflow 数据库完整记录
type Workflow struct {
	ID          int64     `json:"id"`           // 自增主键
	WorkflowID  string    `json:"workflow_id"`  // 业务 ID（如 test_workflow）
	Name        string    `json:"name"`         // 工作流名称
	YAMLContent string    `json:"yaml_content"` // 生成的 YAML
	JSONConfig  string    `json:"json_config"`  // 前端配置 JSON（用于回显编辑）
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkflowListItem 列表查询时只返回摘要，不含大字段
type WorkflowListItem struct {
	ID         int64     `json:"id"`
	WorkflowID string    `json:"workflow_id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Resp struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func ok(data interface{}) Resp   { return Resp{Code: 0, Message: "success", Data: data} }
func fail(msg string) Resp       { return Resp{Code: 1, Message: msg} }

// ========================
// 数据库初始化
// ========================

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open(DBDriver, DBFilePath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err = db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	return createTable()
}

func createTable() error {
	ddl := `CREATE TABLE IF NOT EXISTS workflows (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		workflow_id  TEXT    NOT NULL DEFAULT '',
		name         TEXT    NOT NULL,
		yaml_content TEXT    NOT NULL,
		json_config  TEXT    NOT NULL,
		created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(ddl); err != nil {
		return err
	}

	ddl2 := `CREATE TABLE IF NOT EXISTS activities_config (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		namespace   TEXT NOT NULL,
		activity    TEXT NOT NULL,
		description TEXT DEFAULT '',
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (namespace, activity)
	);`
	_, err := db.Exec(ddl2)
	return err
}

// ========================
// HTTP 工具
// ========================

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// ========================
// API Handlers
// ========================

// POST /api/workflows — 新建工作流
func handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowID  string `json:"workflow_id"`
		Name        string `json:"name"`
		YAMLContent string `json:"yaml_content"`
		JSONConfig  string `json:"json_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, fail("请求参数错误: "+err.Error()))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeJSON(w, 400, fail("工作流名称不能为空"))
		return
	}

	result, err := db.Exec(
		`INSERT INTO workflows (workflow_id, name, yaml_content, json_config) VALUES (?, ?, ?, ?)`,
		req.WorkflowID, req.Name, req.YAMLContent, req.JSONConfig,
	)
	if err != nil {
		writeJSON(w, 500, fail("保存失败: "+err.Error()))
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 200, ok(map[string]interface{}{"id": id}))
}

// GET /api/workflows — 获取列表（不含大字段）
func handleList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(
		`SELECT id, workflow_id, name, created_at, updated_at
		 FROM workflows ORDER BY updated_at DESC LIMIT 200`,
	)
	if err != nil {
		writeJSON(w, 500, fail("查询失败: "+err.Error()))
		return
	}
	defer rows.Close()

	list := make([]WorkflowListItem, 0)
	for rows.Next() {
		var item WorkflowListItem
		if err := rows.Scan(&item.ID, &item.WorkflowID, &item.Name, &item.CreatedAt, &item.UpdatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	writeJSON(w, 200, ok(list))
}

// GET /api/workflows/:id — 获取详情（含 yaml_content 和 json_config）
func handleGet(w http.ResponseWriter, r *http.Request, id int64) {
	var wf Workflow
	err := db.QueryRow(
		`SELECT id, workflow_id, name, yaml_content, json_config, created_at, updated_at
		 FROM workflows WHERE id = ?`, id,
	).Scan(&wf.ID, &wf.WorkflowID, &wf.Name, &wf.YAMLContent, &wf.JSONConfig, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		writeJSON(w, 404, fail("工作流不存在"))
		return
	}
	if err != nil {
		writeJSON(w, 500, fail("查询失败: "+err.Error()))
		return
	}
	writeJSON(w, 200, ok(wf))
}

// PUT /api/workflows/:id — 更新工作流
func handleUpdate(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		WorkflowID  string `json:"workflow_id"`
		Name        string `json:"name"`
		YAMLContent string `json:"yaml_content"`
		JSONConfig  string `json:"json_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, fail("请求参数错误: "+err.Error()))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeJSON(w, 400, fail("工作流名称不能为空"))
		return
	}

	result, err := db.Exec(
		`UPDATE workflows SET workflow_id=?, name=?, yaml_content=?, json_config=? WHERE id=?`,
		req.WorkflowID, req.Name, req.YAMLContent, req.JSONConfig, id,
	)
	if err != nil {
		writeJSON(w, 500, fail("更新失败: "+err.Error()))
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, 404, fail("工作流不存在"))
		return
	}
	writeJSON(w, 200, ok(nil))
}

// DELETE /api/workflows/:id — 删除工作流
func handleDelete(w http.ResponseWriter, r *http.Request, id int64) {
	result, err := db.Exec(`DELETE FROM workflows WHERE id = ?`, id)
	if err != nil {
		writeJSON(w, 500, fail("删除失败: "+err.Error()))
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, 404, fail("工作流不存在"))
		return
	}
	writeJSON(w, 200, ok(nil))
}

// ========================
// Activities Config 数据模型
// ========================

type ActivitiesConfig struct {
	ID          int64     `json:"id"`
	Namespace   string    `json:"namespace"`
	Activity    string    `json:"activity"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// ========================
// Activities Config API Handlers
// ========================

func routeActivitiesConfig(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/activities-config")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			handleListActivitiesConfig(w, r)
		case http.MethodPost:
			handleCreateActivitiesConfig(w, r)
		default:
			writeJSON(w, 405, fail("方法不允许"))
		}
		return
	}

	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeJSON(w, 400, fail("无效的 ID"))
		return
	}
	if r.Method == http.MethodDelete {
		handleDeleteActivitiesConfig(w, r, id)
		return
	}
	writeJSON(w, 405, fail("方法不允许"))
}

// GET /api/activities-config — 获取全部配置
func handleListActivitiesConfig(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, namespace, activity, description, created_at FROM activities_config ORDER BY id ASC`)
	if err != nil {
		writeJSON(w, 500, fail("查询失败: "+err.Error()))
		return
	}
	defer rows.Close()

	list := make([]ActivitiesConfig, 0)
	for rows.Next() {
		var item ActivitiesConfig
		if err := rows.Scan(&item.ID, &item.Namespace, &item.Activity, &item.Description, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	writeJSON(w, 200, ok(list))
}

// POST /api/activities-config — 新增配置
func handleCreateActivitiesConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Namespace   string `json:"namespace"`
		Activity    string `json:"activity"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, fail("请求参数错误: "+err.Error()))
		return
	}
	if strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.Activity) == "" {
		writeJSON(w, 400, fail("namespace 和 activity 不能为空"))
		return
	}
	_, err := db.Exec(
		`INSERT INTO activities_config (namespace, activity, description) VALUES (?, ?, ?)`,
		req.Namespace, req.Activity, req.Description,
	)
	if err != nil {
		writeJSON(w, 500, fail("保存失败: "+err.Error()))
		return
	}
	writeJSON(w, 200, ok(nil))
}

// DELETE /api/activities-config/:id — 删除配置
func handleDeleteActivitiesConfig(w http.ResponseWriter, r *http.Request, id int64) {
	result, err := db.Exec(`DELETE FROM activities_config WHERE id = ?`, id)
	if err != nil {
		writeJSON(w, 500, fail("删除失败: "+err.Error()))
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, 404, fail("配置不存在"))
		return
	}
	writeJSON(w, 200, ok(nil))
}

// ========================
// 测试流程 Mock API
// ========================

// POST /api/test-workflow — Mock 测试工作流
func handleTestWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, fail("方法不允许"))
		return
	}
	var req struct {
		WorkflowID string                 `json:"workflow_id"`
		Variables  map[string]string      `json:"variables"`
		JSONConfig map[string]interface{} `json:"json_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, fail("请求参数错误: "+err.Error()))
		return
	}

	// 构造 steps 摘要日志
	now := time.Now().Format("2006-01-02 15:04:05")
	logs := []string{
		"[" + now + "] 开始执行工作流: " + req.WorkflowID,
	}
	if cfg := req.JSONConfig; cfg != nil {
		if steps, ok := cfg["steps"].([]interface{}); ok {
			for _, s := range steps {
				if step, ok := s.(map[string]interface{}); ok {
					sid, _ := step["id"].(string)
					sname, _ := step["name"].(string)
					label := sid
					if sname != "" {
						label = sname + "(" + sid + ")"
					}
					logs = append(logs, "["+label+"] 执行成功")
				}
			}
		}
	}
	logs = append(logs, "["+now+"] 工作流执行完成，状态: success")

	// Mock 返回，延迟 1 秒模拟执行
	time.Sleep(1 * time.Second)
	writeJSON(w, 200, ok(map[string]interface{}{
		"status": "success",
		"result": map[string]interface{}{
			"workflow_id": req.WorkflowID,
			"output":      "工作流执行成功（mock）",
			"variables":   req.Variables,
		},
		"logs": logs,
	}))
}

// ========================
// 路由分发
// ========================

func routeWorkflows(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/workflows")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			handleList(w, r)
		case http.MethodPost:
			handleCreate(w, r)
		default:
			writeJSON(w, 405, fail("方法不允许"))
		}
		return
	}

	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeJSON(w, 400, fail("无效的 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet(w, r, id)
	case http.MethodPut:
		handleUpdate(w, r, id)
	case http.MethodDelete:
		handleDelete(w, r, id)
	default:
		writeJSON(w, 405, fail("方法不允许"))
	}
}

// ========================
// 主入口
// ========================

func main() {
	if err := initDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Println("数据库连接成功，表结构已就绪")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/workflows", enableCORS(routeWorkflows))
	mux.HandleFunc("/api/workflows/", enableCORS(routeWorkflows))
	mux.HandleFunc("/api/activities-config", enableCORS(routeActivitiesConfig))
	mux.HandleFunc("/api/activities-config/", enableCORS(routeActivitiesConfig))
	mux.HandleFunc("/api/test-workflow", enableCORS(handleTestWorkflow))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, ok("ok"))
	})

	log.Printf("服务启动，监听 %s\n", ServerPort)
	if err := http.ListenAndServe(ServerPort, mux); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
