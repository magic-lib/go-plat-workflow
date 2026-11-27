package repo

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// IGNORE INDEX 里写了不存在的索引 MySQL 会直接报错，这里保证统计查询忽略的每个索引都在 NodeLogModel 中声明，
// 且 NodeLogModel 中所有以 project 开头的联合索引都已被忽略（否则优化器可能改走它们，统计又变慢）。
func TestStatsIgnoreIndexesExist(t *testing.T) {
	m := regexp.MustCompile(`IGNORE INDEX \(([^)]*)\)`).FindStringSubmatch(statsNodeLogTable)
	if m == nil {
		t.Fatalf("statsNodeLogTable has no IGNORE INDEX: %q", statsNodeLogTable)
	}
	ignored := map[string]bool{}
	for _, n := range strings.Split(m[1], ",") {
		ignored[strings.TrimSpace(n)] = true
	}

	s, err := schema.Parse(&models.NodeLogModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	declared := map[string]bool{}
	for _, idx := range s.ParseIndexes() {
		declared[idx.Name] = true
		if len(idx.Fields) > 1 && idx.Fields[0].DBName == "project" && !ignored[idx.Name] {
			t.Errorf("index %s starts with project but is not ignored by stats queries", idx.Name)
		}
	}
	for name := range ignored {
		if !declared[name] {
			t.Errorf("stats queries ignore index %s, but NodeLogModel does not declare it", name)
		}
	}
}
