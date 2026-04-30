package rulegox_test

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/action"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/rs/zerolog/log"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// 1. 加法：输入两个数字，返回和
type AddReq struct {
	A int `json:"a"`
	B int `json:"b"`
	C int `json:"c"`
}

func AddMethod(_ context.Context, req *AddReq) (int, error) {
	fmt.Println("[AddMethod] received:", conv.String(req))
	return req.A + req.B, nil
}

// 2. 拼接字符串：输入两个字符串，返回拼接结果
type ConcatReq struct {
	S1 string `json:"s1"`
	S2 string `json:"s2"`
}

func ConcatMethod(_ context.Context, req *ConcatReq) (string, error) {
	fmt.Println("[ConcatMethod] received:", conv.String(req))
	return req.S1 + req.S2, nil
}

// 3. 打印日志：输入任意 map，返回 echo
func LoggerMethod(_ context.Context, req map[string]any) (bool, error) {
	fmt.Println("[LoggerMethod] received:", conv.String(req))
	return true, nil
}

var testActionsRegistered bool

func registerTestActions() {
	if testActionsRegistered {
		return
	}
	testActionsRegistered = true
	// action 1: add
	err := action.RegisterActor[*AddReq, int](AddMethod, &action.ActMetaData{
		Namespace:    "test",
		Action:       "add",
		Desc:         "加法计算",
		RequiredArgs: []string{"a", "b"},
	})
	if err != nil {
		panic(err)
	}
	// action 2: concat
	err = action.RegisterActor[*ConcatReq, string](ConcatMethod, &action.ActMetaData{
		Namespace: "test",
		Action:    "concat",
		Desc:      "字符串拼接",
	})
	if err != nil {
		panic(err)
	}
	// action 3: logger
	err = action.RegisterActor[map[string]any, bool](LoggerMethod, &action.ActMetaData{
		Namespace: "test",
		Action:    "logger",
		Desc:      "日志打印",
	})
	if err != nil {
		panic(err)
	}

	// ---------- Activity 1: add（使用 Arguments 绑定参数）----------
	addAct := &activity.Activity{
		ActNamespace: "test",
		ActName:      "add",
		Arguments: []*param.BindConfig{
			{Key: "a", Value: 3, Policy: param.KeyPolicyDefaultOnly},
			{Key: "b", Value: 5, Policy: param.KeyPolicyBackendPriority},
			{Key: "c", Value: 8, Policy: param.KeyPolicyFrontendPriority},
		},
		Control: activity.ActivityControl{
			CtxCacheable: true, // 启用流程级缓存
		},
	}

	// ---------- Activity 2: concat（使用 ArgTemplate 动态取依赖结果）----------
	addAct1 := addAct.Clone()
	addAct1.Id = "testAdd4"
	concatAct := &activity.Activity{
		ActNamespace: "test",
		ActName:      "concat",
		ArgTemplate:  `{"s1":"{{testAdd4.responses}}","s2":"!"}`,
		DependsOn: []*activity.Activity{
			addAct1,
		},
	}

	// ---------- Activity 3: logger（使用 Hooks + When 条件）----------
	loggerAct := &activity.Activity{
		ActNamespace: "test",
		ActName:      "logger",
	}
	log.Print(concatAct, loggerAct)
}

func TestRuleGoActivityNode(t *testing.T) {
	registerTestActions()

	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	var chainConfig = `{
  "ruleChain": {
	"id": "activity_flow_01",
	"name": "多分支选择流程"
  },
  "metadata": {
	"nodes": [
	  { 
		"id": "testAdd1", 
		"type": "test/add", 
		"name": "数字相加",
		"configuration": {
			"arguments": {
				"a": 55
			},
			"responses": {
				"ctx_cacheable": true
			}
		}
	  },
{ 
		"id": "testConcat1", 
		"type": "test/concat", 
		"name": "字符串拼接"
	  },
{ 
		"id": "testLogger1", 
		"type": "test/logger", 
		"name": "日志"
	  },
{ 
		"id": "n_switch", 
		"type": "condition", 
		"name": "年龄条件分流器",
		"configuration": {
		  "condition": "Switch([variables.age], 10, 'Age10', 15, 'Age15', 'Default')"
		}
	  },
	  { "id": "n_age10", "type": "custom/handleAge10", "name": "处理10岁逻辑" },
	  { "id": "n_age15", "type": "custom/handleAge15", "name": "处理15岁逻辑" },
	  { "id": "n_default", "type": "custom/handleDefault", "name": "处理其他年龄"}
	],
	"connections": [
	  { "fromId": "testAdd1", "toId": "testConcat1", "type": "Success" },
	  { "fromId": "testConcat1", "toId": "testLogger1", "type": "Success" },
	  { "fromId": "testLogger1", "toId": "n_switch", "type": "Success" },
	  { "fromId": "n_switch", "toId": "n_age10", "type": "Age10" },
	  { "fromId": "n_switch", "toId": "n_age15", "type": "Age15" },
	  { "fromId": "n_switch", "toId": "n_default", "type": "Default" }
	]
  }
}`

	fmt.Println(chainConfig)
	//err := rulegox.StartActivityFlow(context.Background(), &rulegox.ActivityFlowConfig{
	//	RootChainDSL: map[string][]byte{
	//		"activity_flow_01": []byte(chainConfig),
	//	},
	//	UseCache: false,
	//	FlowCtx: &paramx.FlowContext{
	//		Arguments: map[string]any{
	//			"age": 20,
	//			"a":   4,
	//			"b":   7,
	//		},
	//		Responses: nil,
	//		Steps:     nil,
	//	},
	//	EndFunc: func(ctx context.Context, param *paramx.FlowContext, err error) {
	//		if err != nil {
	//			fmt.Printf("工作流执行失败: %v\n", err)
	//			return
	//		}
	//		fmt.Println("工作流执行成功:", conv.String(param))
	//	},
	//}, nil)
	//if err != nil {
	//	panic(err)
	//}
}

type HandleAge10Node struct{}

func (n *HandleAge10Node) New() types.Node { return &HandleAge10Node{} }
func (n *HandleAge10Node) Type() string    { return "custom/handleAge10" }
func (n *HandleAge10Node) Init(ruleConfig types.Config, configuration types.Configuration) error {
	return nil
}
func (n *HandleAge10Node) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	fmt.Println("HandleAge10Node OnMsg:", msg.Type)

	var data map[string]any
	conv.Unmarshal(msg.GetData(), &data)

	fmt.Println("HandleAge10Node HandleRejectNode:", conv.String(msg))

	data["status"] = "rejected"
	msg.SetData(conv.String(data))

	ctx.TellNext(msg, types.Success)
}
func (n *HandleAge10Node) Destroy() {}

type HandleAge15Node struct{}

func (n *HandleAge15Node) New() types.Node { return &HandleAge15Node{} }
func (n *HandleAge15Node) Type() string    { return "custom/handleAge15" }
func (n *HandleAge15Node) Init(ruleConfig types.Config, configuration types.Configuration) error {
	return nil
}
func (n *HandleAge15Node) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	fmt.Println("HandleAge15Node OnMsg:", msg.Type)

	var data map[string]any
	conv.Unmarshal(msg.GetData(), &data)

	fmt.Println("HandleAge15Node HandleRejectNode:", conv.String(msg))

	data["status"] = "rejected"
	msg.SetData(conv.String(data))

	ctx.TellNext(msg, types.Success)
}
func (n *HandleAge15Node) Destroy() {}

type HandleAgeDefaultNode struct{}

func (n *HandleAgeDefaultNode) New() types.Node { return &HandleAgeDefaultNode{} }
func (n *HandleAgeDefaultNode) Type() string    { return "custom/handleDefault" }
func (n *HandleAgeDefaultNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	return nil
}
func (n *HandleAgeDefaultNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	fmt.Println("HandleAgeDefaultNode OnMsg:", msg.Type)

	var data map[string]any
	conv.Unmarshal(msg.GetData(), &data)

	fmt.Println("HandleAgeDefaultNode HandleRejectNode:", conv.String(msg))

	data["status"] = "rejected"
	msg.SetData(conv.String(data))

	ctx.TellNext(msg, types.Success)
}
func (n *HandleAgeDefaultNode) Destroy() {}

// TestRuleGoSubChain 测试子规则链：主链通过 flow 节点（targetId）调用子链。
// 关键点：子链与主链必须共用同一个 rulego.Config（即同一个引擎池），flow 节点才能按 targetId 解析到子链。
func TestRuleGoSubChain(t *testing.T) {
	registerTestActions()

	// 1) 子规则链：复用已注册的 test/add、test/logger 节点
	subChainDef := `{
  "ruleChain": { "id": "sub_test_01", "name": "子规则链-加法与日志" },
  "metadata": {
    "nodes": [
      { "id": "sub_add", "type": "test/add", "name": "子链加法" },
      { "id": "sub_logger", "type": "test/logger", "name": "子链日志" }
    ],
    "connections": [
      { "fromId": "sub_add", "toId": "sub_logger", "type": "Success" }
    ]
  }
}`

	// 2) 主规则链：通过 flow 节点（targetId）调用子链
	rootDef := `{
  "ruleChain": { "id": "root_sub_test", "name": "主链-调用子链" },
  "metadata": {
    "nodes": [
      { "id": "root_add", "type": "test/add", "name": "主链加法" },
      {
        "id": "root_flow",
        "type": "flow",
        "name": "调用子规则链",
        "configuration": { "targetId": "sub_test_01" }
      }
    ],
    "connections": [
      { "fromId": "root_add", "toId": "root_flow", "type": "Success" }
    ]
  }
}`

	config := rulego.NewConfig()
	// 关键：子链与主链共用同一个 config（同一个引擎池），flow 才能解析 targetId
	if _, err := rulego.New("sub_test_01", []byte(subChainDef), rulego.WithConfig(config)); err != nil {
		t.Fatalf("register sub chain failed: %v", err)
	}
	engine, err := rulego.New("root_sub_test", []byte(rootDef), rulego.WithConfig(config))
	if err != nil {
		t.Fatalf("create main engine failed: %v", err)
	}

	paramInput := paramx.NewFlowContext("", "", map[string]any{"age": 20})
	msg := types.NewMsg(0, "ACTIVITY_EVENT", types.JSON, types.NewMetadata(), conv.String(paramInput))

	done := make(chan error, 1)
	engine.OnMsgAndWait(msg, types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		if err != nil {
			fmt.Printf("子链流程执行失败: %v\n", err)
			done <- err
			return
		}
		fmt.Println("子链流程执行成功:", msg.GetData())
		done <- nil
	}))
	if err := <-done; err != nil {
		t.Fatalf("sub chain test failed: %v", err)
	}
}
