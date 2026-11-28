package models

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// indexDefs 解析模型索引：索引名 -> "列(前缀长度),..."，唯一索引带 UNIQUE: 前缀。
func indexDefs(t *testing.T, model any) map[string]string {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	out := map[string]string{}
	for _, idx := range s.ParseIndexes() {
		cols := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			c := f.DBName
			if f.Length > 0 {
				c += fmt.Sprintf("(%d)", f.Length)
			}
			cols = append(cols, c)
		}
		def := strings.Join(cols, ",")
		if idx.Class == "UNIQUE" {
			def = "UNIQUE:" + def
		}
		out[idx.Name] = def
	}
	return out
}

// 启动迁移已精简为只跑 AutoMigrate + 少量老库处理，表结构最终态必须由 model 标签表达。
func TestSchemaFinalStateInModels(t *testing.T) {
	nl := indexDefs(t, &NodeLogModel{})
	// event_id / relation_type 为 varchar(500)，utf8mb4 全列索引超长，只能建前缀索引；索引名沿用老库的列名
	if nl["event_id"] != "event_id(191)" || nl["relation_type"] != "relation_type(191)" {
		t.Errorf("wf_node_logs prefix indexes: event_id=%q relation_type=%q", nl["event_id"], nl["relation_type"])
	}

	rc := indexDefs(t, &RootChainModel{})
	if rc["uk_project_chain_id"] != "UNIQUE:project,chain_id" {
		t.Errorf("uk_project_chain_id = %q", rc["uk_project_chain_id"])
	}
	// chain_key 全局唯一索引由 service.ensureChainKeyUniqueIndex 管理，标签里不能再声明（否则老库有重名时 AutoMigrate 直接失败）
	for _, name := range []string{"uk_project_chain_key", "uk_chain_key"} {
		if def, ok := rc[name]; ok {
			t.Errorf("wf_root_chains must not declare %s in tags, got %q", name, def)
		}
	}

	tr := indexDefs(t, &NodeTestRecordModel{})
	if tr["idx_wf_node_test_records_trace_id"] != "trace_id" {
		t.Errorf("wf_node_test_records trace_id index = %q", tr["idx_wf_node_test_records_trace_id"])
	}
	if _, ok := tr["idx_trace_id"]; ok {
		t.Error("idx_trace_id duplicates the trace_id index and should not be declared")
	}
}
