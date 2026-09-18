package workflow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

func TestInvokeWorkerFlowAPI_Success(t *testing.T) {
	const (
		project = "zamloan2"
		env     = "test"
		domain  = "http://127.0.0.1:8686"
		apiTok  = "8e421035-705e-be79-f9e8-81f46426a5ef"
	)

	req := &workflow.InvokeRequest{
		ChainKey: "R000003-9q0pq",
		Payload: map[string]any{
			"group_code": "M3",
			"user_id":    10013,
			"mobile":     "0972921661",
		},
	}

	data, err := workflow.InvokeWorkerFlowAPI(context.Background(), project, env, domain, apiTok, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data type unexpected: %T", data)
	}
	fmt.Print(m)
}
