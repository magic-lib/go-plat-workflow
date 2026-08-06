// Package migration 提供数据库表结构初始化功能。
// 所有 DDL 语句通过 go:embed 嵌入，运行时支持配置表名前缀。
//
// 使用示例：
//
//	db, _ := sql.Open("mysql", dsn)
//	if err := migration.Run(db, "wf_"); err != nil {
//	    log.Fatal(err)
//	}
package migration

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed ddl.sql
var ddlTemplate string

var parsedTemplate = template.Must(template.New("ddl").Parse(ddlTemplate))

// Config 迁移配置
type Config struct {
	// TablePrefix 表名前缀，如 "wf_" → 表名变为 wf_activities、wf_workflows 等
	// 传空字符串表示不加前缀，使用原始表名
	TablePrefix string
}

// Generate 根据配置生成完整的 DDL SQL 字符串。
// 返回可直接打印或执行的建表语句。
func Generate(prefix string) (string, error) {
	var buf strings.Builder
	if err := parsedTemplate.Execute(&buf, Config{TablePrefix: prefix}); err != nil {
		return "", fmt.Errorf("migration: generate DDL failed: %w", err)
	}
	return buf.String(), nil
}

// DBInit 根据配置连接数据库并执行全部建表语句。
// 每条 DDL 语句独立执行，出错时返回具体失败的语句和行号。
func DBInit(db *sql.DB, prefix string) error {
	ddl, err := Generate(prefix)
	if err != nil {
		return err
	}
	return executeStatements(db, ddl)
}

// Tables 返回所有表名（含前缀），供业务层引用。
func Tables(prefix string) []string {
	return []string{
		prefix + "activities",
		prefix + "activity_instances",
		prefix + "workflows",
		prefix + "workflow_executions",
		prefix + "execution_logs",
	}
}

// executeStatements 按分号拆分 DDL 并逐条执行
func executeStatements(db *sql.DB, ddl string) error {
	stmts := splitDDL(ddl)
	for i, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			// 截取前 120 字符方便排查
			preview := stmt
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			return fmt.Errorf("migration: statement #%d failed: %w\n  SQL: %s", i+1, err, preview)
		}
	}
	return nil
}

// splitDDL 按分号拆分，跳过纯注释和空语句
func splitDDL(ddl string) []string {
	raw := strings.Split(ddl, ";")
	result := make([]string, 0, len(raw))
	for _, segment := range raw {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		// 跳过纯注释行（不含 SQL 关键字的块）
		if isCommentOnly(segment) {
			continue
		}
		result = append(result, segment)
	}
	return result
}

func isCommentOnly(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return false
	}
	return true
}
