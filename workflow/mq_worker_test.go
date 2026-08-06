package workflow_test

import (
	"context"
	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-workflow/workflow"
	"testing"
)

func TestMQWorkerRequest(t *testing.T) {
	projectName := "zamloan2_bot_credit"
	env := "test"
	w, err := workflow.NewMQWorker(projectName, env, &conn.Connect{
		Host:     "202.60.228.31",
		Port:     "6379",
		Password: "mjhttyryt565-jyjh5824t-p55w",
		Database: "0",
	})
	if err != nil {
		panic(err)
	}
	defer w.Stop()
	data, err := w.RequestActivity(context.Background(), "credit-server", "credit_order_status", map[string]any{
		"audit_order_id": 555,
	})
	if err != nil {
		panic(err)
	}
	t.Log("response:", conv.String(data))
}
