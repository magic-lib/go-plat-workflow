package models

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// indexColumns 解析模型的 gorm 索引定义，返回 索引名 -> 按顺序的列名（逗号连接）。
func indexColumns(t *testing.T, model any) map[string]string {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	out := map[string]string{}
	for _, idx := range s.ParseIndexes() {
		cols := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			cols = append(cols, f.DBName)
		}
		out[idx.Name] = strings.Join(cols, ",")
	}
	return out
}

func TestLogModelIndexes(t *testing.T) {
	cases := []struct {
		model any
		want  map[string]string
	}{
		{&NodeLogModel{}, map[string]string{
			"idx_node_proj":       "project,node_id",
			"idx_nl_created":      "created_at",
			"idx_nl_proj_created": "project,created_at",
			"idx_nl_proj_id":      "project,id",
		}},
		{&ActivityLogModel{}, map[string]string{
			"idx_proj_act":   "project,act_namespace,act_name",
			"idx_al_created": "created_at",
			"idx_al_proj_id": "project,id",
		}},
	}
	for _, c := range cases {
		got := indexColumns(t, c.model)
		for name, cols := range c.want {
			if got[name] != cols {
				t.Errorf("%T index %s: got %q, want %q", c.model, name, got[name], cols)
			}
		}
	}
}
