package migration

import (
	"strings"
	"testing"
)

func TestGenerate_NoPrefix(t *testing.T) {
	ddl, err := Generate("")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	tables := []string{
		"CREATE TABLE IF NOT EXISTS activities",
		"CREATE TABLE IF NOT EXISTS activity_instances",
		"CREATE TABLE IF NOT EXISTS workflows",
		"CREATE TABLE IF NOT EXISTS workflow_executions",
		"CREATE TABLE IF NOT EXISTS execution_logs",
	}
	for _, want := range tables {
		if !strings.Contains(ddl, want) {
			t.Errorf("expected DDL to contain %q", want)
		}
	}
}

func TestGenerate_WithPrefix(t *testing.T) {
	const prefix = "wf_"
	ddl, err := Generate(prefix)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	tables := []string{
		"CREATE TABLE IF NOT EXISTS wf_activities",
		"CREATE TABLE IF NOT EXISTS wf_activity_instances",
		"CREATE TABLE IF NOT EXISTS wf_workflows",
		"CREATE TABLE IF NOT EXISTS wf_workflow_executions",
		"CREATE TABLE IF NOT EXISTS wf_execution_logs",
	}
	for _, want := range tables {
		if !strings.Contains(ddl, want) {
			t.Errorf("expected DDL to contain %q", want)
		}
	}

	// 确保原始表名不带前缀的不会出现
	if strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS activities\n") {
		t.Error("expected no unprefixed table name in DDL with prefix")
	}
}

func TestTables(t *testing.T) {
	result := Tables("wf_")
	expected := []string{
		"wf_activities",
		"wf_activity_instances",
		"wf_workflows",
		"wf_workflow_executions",
		"wf_execution_logs",
	}
	if len(result) != len(expected) {
		t.Fatalf("got %d tables, want %d", len(result), len(expected))
	}
	for i, name := range result {
		if name != expected[i] {
			t.Errorf("Tables()[%d] = %q, want %q", i, name, expected[i])
		}
	}
}
