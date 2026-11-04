package rulegox

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-workflow/workflow/engine"
	"github.com/rulego/rulego/api/types"
)

func jsTransformNode(id string) *types.RuleNode {
	return &types.RuleNode{Id: id, Type: "jsTransform", Name: id, Configuration: types.Configuration{
		"jsScript": "return {'msg':msg,'metadata':metadata,'msgType':msgType};",
	}}
}

// 一个节点扇出到两个结束分支：EndFunc 只能被回调一次（原实现每个分支各回调一次，调用方重复 close channel）。
func TestStartWorkFlowEndFuncCalledOnceOnFanOut(t *testing.T) {
	chainID := fmt.Sprint("fan_end_", time.Now().UnixNano())
	dsl := &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{ID: chainID, Name: chainID},
		Metadata: types.RuleMetadata{
			Nodes: []*types.RuleNode{jsTransformNode("n1"), jsTransformNode("n2"), jsTransformNode("n3")},
			Connections: []types.NodeConnection{
				{FromId: "n1", ToId: "n2", Type: types.Success},
				{FromId: "n1", ToId: "n3", Type: types.Success},
			},
		},
	}
	var calls int32
	done := make(chan struct{}, 4)
	err := StartWorkFlow(context.Background(), &ActivityFlowConfig{
		RootChainDSL: dsl,
		FlowContext: paramx.NewFlowContext(chainID, "trace", map[string]any{}, func(key string, val any) (any, bool) {
			return val, false
		}),
		UseCache: true,
		EndFunc: func(ctx context.Context, relationType string, param *paramx.FlowContext, err error) {
			atomic.AddInt32(&calls, 1)
			done <- struct{}{}
		},
	}, &ActivityMetaData{Env: "test", Project: "test"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("EndFunc not called")
	}
	time.Sleep(200 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("EndFunc called %d times, want 1", n)
	}
}

// Redis 配置（含密码）不进 msg.Metadata（会随调试/出错日志落库），改由进程内按 project+env 登记，节点从这里取回。
func TestStartWorkFlowRegistersRedisConfig(t *testing.T) {
	chainID := fmt.Sprint("meta_", time.Now().UnixNano())
	dsl := &types.RuleChain{
		RuleChain: types.RuleChainBaseInfo{ID: chainID, Name: chainID},
		Metadata:  types.RuleMetadata{Nodes: []*types.RuleNode{jsTransformNode("n1")}},
	}
	done := make(chan struct{})
	cfg := &ActivityFlowConfig{
		RootChainDSL: dsl,
		FlowContext: paramx.NewFlowContext(chainID, "trace", map[string]any{}, func(key string, val any) (any, bool) {
			return val, false
		}),
		UseCache: true,
		EndFunc:  func(ctx context.Context, relationType string, param *paramx.FlowContext, err error) { close(done) },
	}
	project := fmt.Sprint("p_", time.Now().UnixNano())
	meta := &ActivityMetaData{Env: "test", Project: project}
	raw, _ := json.Marshal(map[string]any{"addr": "127.0.0.1:6379", "password": "secret-pwd"})
	if err := json.Unmarshal(raw, &meta.RedisConfig); err != nil {
		t.Fatal(err)
	}
	if err := StartWorkFlow(context.Background(), cfg, meta); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := engine.EnvRedisConfig(project, "test"); got == nil || got != meta.RedisConfig {
		t.Fatalf("redis config not registered for project/env, got %v", got)
	}
}
