package repo

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// TestCreateWithAutoBizIDConcurrentMySQL 在真实 MySQL 上并发创建，验证「先插入、再回写」的编号不重复。
// 默认跳过；设置 WF_IDSEQ_TEST_DSN（只能指向 test 库）后运行，测试数据用独立项目名，结束后按项目删除。
func TestCreateWithAutoBizIDConcurrentMySQL(t *testing.T) {
	dsn := os.Getenv("WF_IDSEQ_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 WF_IDSEQ_TEST_DSN，跳过 MySQL 并发测试")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// 限制连接数，避免把共用的 test 库连接打满；20 个连接同时取号足以覆盖并发场景
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(20)
	defer sqlDB.Close()
	const project = "__idseq_test__"
	tables := []string{"wf_nodes", "wf_sub_chains", "wf_test_cases", "wf_node_test_records", "wf_activity_test_records"}
	cleanup := func() {
		for _, tb := range tables {
			if err := db.Exec("DELETE FROM "+tb+" WHERE project = ?", project).Error; err != nil {
				t.Errorf("cleanup %s: %v", tb, err)
			}
		}
	}
	cleanup()
	defer cleanup()

	ctx := context.Background()
	const n = 50
	type result struct {
		kind, id string
		err      error
	}
	results := make(chan result, n*5)
	var wg sync.WaitGroup
	nodeRepo, subRepo := NewNodeRepo(db), NewSubChainRepo(db)
	caseRepo, ntrRepo, atrRepo := NewTestCaseRepo(db), NewNodeTestRecordRepo(db), NewActivityTestRecordRepo(db)
	for i := 0; i < n; i++ {
		wg.Add(5)
		go func(i int) {
			defer wg.Done()
			d := &workflow.NodeDef{Project: project, Name: fmt.Sprintf("n%d", i), Type: "custom/Activity", Status: 1}
			err := nodeRepo.Create(ctx, d)
			results <- result{"N", d.NodeID, err}
		}(i)
		go func(i int) {
			defer wg.Done()
			tmp := workflow.NewTempBizID()
			d := &workflow.SubChainDef{Project: project, ChainID: tmp, Name: fmt.Sprintf("s%d", i), Status: 1,
				DSLJSON: `{"ruleChain":{"id":"` + tmp + `","name":"s"},"metadata":{"nodes":[]}}`}
			err := subRepo.Create(ctx, d)
			if err == nil && (strings.Contains(d.DSLJSON, tmp) || !strings.Contains(d.DSLJSON, `"id":"`+d.ChainID+`"`)) {
				err = fmt.Errorf("dsl not replaced: %s", d.DSLJSON)
			}
			results <- result{"F", d.ChainID, err}
		}(i)
		go func(i int) {
			defer wg.Done()
			d := &workflow.TestCaseDef{Project: project, Name: fmt.Sprintf("c%d", i), ChainID: "R000001"}
			err := caseRepo.Create(ctx, d)
			results <- result{"TC", d.CaseID, err}
		}(i)
		go func(i int) {
			defer wg.Done()
			d := &workflow.NodeTestRecordDef{Project: project, NodeID: "N000001"}
			err := ntrRepo.Create(ctx, d)
			results <- result{"M", d.RecordID, err}
		}(i)
		go func(i int) {
			defer wg.Done()
			d := &workflow.ActivityTestRecordDef{Project: project, ActivityID: "A000001"}
			err := atrRepo.Create(ctx, d)
			results <- result{"AT", d.RecordID, err}
		}(i)
	}
	wg.Wait()
	close(results)

	prefix := map[string]string{"N": "N", "F": "F", "TC": "T", "M": "M", "AT": "T"}
	seen := map[string]map[string]bool{}
	for r := range results {
		if r.err != nil {
			t.Fatalf("%s create: %v", r.kind, r.err)
		}
		if !regexp.MustCompile(`^` + prefix[r.kind] + `\d{6,}$`).MatchString(r.id) {
			t.Fatalf("%s bad id %q", r.kind, r.id)
		}
		if seen[r.kind] == nil {
			seen[r.kind] = map[string]bool{}
		}
		if seen[r.kind][r.id] {
			t.Fatalf("%s duplicate id %q", r.kind, r.id)
		}
		seen[r.kind][r.id] = true
	}
	for kind, ids := range seen {
		if len(ids) != n {
			t.Fatalf("%s got %d ids, want %d", kind, len(ids), n)
		}
	}
	// 不残留临时编号
	var tmpLeft int64
	db.Raw("SELECT COUNT(*) FROM wf_nodes WHERE project = ? AND node_id LIKE '\\_\\_tmp\\_%'", project).Scan(&tmpLeft)
	if tmpLeft != 0 {
		t.Fatalf("temp node ids left: %d", tmpLeft)
	}
}
