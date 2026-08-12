package commnode_test

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/action"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// 1. 加法：输入两个数字，返回和
type AddReq struct {
	A int `json:"a"`
	B int `json:"b"`
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

func registerTestActions() {
	// action 1: add
	addActor, err := action.MethodToActor[*AddReq, int](AddMethod, &action.ActMetaData{
		Namespace:    "test",
		Action:       "add",
		Desc:         "加法计算",
		RequiredArgs: []string{"a", "b"},
	})
	if err != nil {
		panic(err)
	}
	// action 2: concat
	concatActor, err := action.MethodToActor[*ConcatReq, string](ConcatMethod, &action.ActMetaData{
		Namespace:    "test",
		Action:       "concat",
		Desc:         "字符串拼接",
		RequiredArgs: []string{"s1", "s2"},
	})
	if err != nil {
		panic(err)
	}
	// action 3: logger
	loggerActor, err := action.MethodToActor[map[string]any, bool](LoggerMethod, &action.ActMetaData{
		Namespace: "test",
		Action:    "logger",
		Desc:      "日志打印",
	})
	if err != nil {
		panic(err)
	}

	err = action.Register(addActor, concatActor, loggerActor)
	if err != nil {
		panic(err)
	}

	activityList := `[
  {
	"activity_type": "test/add",
    "act_namespace": "test",
    "act_name": "add",
    "arguments": [
      {"key": "a", "value": 3, "policy": "default"},
      {"key": "b", "value": 5}
    ],
    "control": {"ctx_cacheable": true}
  },
  {
    "activity_type": "test/concat",
    "act_namespace": "test",
    "act_name": "concat",
    "arg_template": "{\"s1\":\"{{add1.responses}}\",\"s2\":\"!\"}",
    "depends_on": [
      {"id": "add1", "act_namespace": "test", "act_name": "add"}
    ]
  },
  {
    "activity_type": "test/logger",
    "act_namespace": "test",
    "act_name": "logger",
    "control": {"when": "{{add1.responses}} > 5"},
    "depends_on": [
      {
		"id": "add1",
		"act_namespace": "test",
		"act_name": "add"
		}
    ]
  }
]`
	activityDBList := make([]*activity.Activity, 0)
	err = conv.Unmarshal(activityList, &activityDBList)
	if err != nil {
		panic(err)
	}

	fmt.Println(activityDBList)
	//
	//// ---------- Activity 1: add（使用 Arguments 绑定参数）----------
	//addAct := &activity.Activity{
	//	ActNamespace: "test",
	//	ActName:      "add",
	//	Arguments: []*param.BindConfig{
	//		{Key: "a", Value: 3, Policy: param.KeyPolicyDefaultOnly},
	//		{Key: "b", Value: 5},
	//	},
	//	Control: activity.ActivityControl{
	//		CtxCacheable: true, // 启用流程级缓存
	//	},
	//}
	//
	//// ---------- Activity 2: concat（使用 ArgTemplate 动态取依赖结果）----------
	//concatAct := &activity.Activity{
	//	ActNamespace: "test",
	//	ActName:      "concat",
	//	ArgTemplate:  `{"s1":"{{add1.responses}}","s2":"!"}`,
	//	DependsOn: []*activity.Activity{
	//		addAct,
	//	},
	//}
	//
	//// ---------- Activity 3: logger（使用 Hooks + When 条件）----------
	//loggerAct := &activity.Activity{
	//	ActNamespace: "test",
	//	ActName:      "logger",
	//	// 仅当 add1 的 sum > 5 时才执行
	//	Control: activity.ActivityControl{
	//		When: `{{add1.responses}} > 5`,
	//	},
	//	DependsOn: []*activity.Activity{addAct},
	//}
	//err = commnode.RegisterNodes(nil, commnode.ActivityGroup{
	//	{activityDBList[0]},
	//	{activityDBList[1]},
	//	{activityDBList[2]},
	//})
	//if err != nil {
	//	panic(err)
	//}
}

func TestRuleGoActivityNode(t *testing.T) {
	registerTestActions()

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
		}
	  },
{ 
		"id": "testConcat1", 
		"type": "test/concat", 
		"name": "字符串拼接",
		"configuration": {
		}
	  },
{ 
		"id": "testLogger1", 
		"type": "test/logger", 
		"name": "日志",
		"configuration": {
		}
	  },
{ 
		"id": "n_switch", 
		"type": "condition", 
		"name": "年龄条件分流器",
		"configuration": {
		  "condition": "Switch(age, 10, 'Age10', 15, 'Age15', 'Default')"
		}
	  },
	  { "id": "n_age10", "type": "custom/handleAge10", "name": "处理10岁逻辑" },
	  { "id": "n_age15", "type": "custom/handleAge15", "name": "处理15岁逻辑" },
	  { "id": "n_default", "type": "custom/handleDefault", "name": "处理其他年龄"}
	],
	"connections": [
	  { "fromId": "testAdd1", "toId": "testConcat1", "type": "Success" },
	  { "fromId": "testConcat1", "toId": "testLogger1", "type": "Success" },
	  { "fromId": "n_switch", "toId": "n_age10", "type": "Age10" },
	  { "fromId": "n_switch", "toId": "n_age15", "type": "Age15" },
	  { "fromId": "n_switch", "toId": "n_default", "type": "Default" }
	]
  }
}`

	// 三、初始化引擎实例
	engine, err := rulego.New("activity_flow_01", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	fmt.Println("初始化引擎")

	// 四、准备你的输入参数：map[string]any
	inputMap := map[string]any{
		"age": 15,
		"a":   4,
		"b":   7,
	}

	msg := types.NewMsg(0, "USER_REGISTER", types.JSON, types.NewMetadata(), conv.String(inputMap))

	fmt.Println("--- 开始执行工作流 ---")

	// 五、直接同步执行并直接获取最终结果（使用 OnMsgWithEnd）
	engine.OnMsgAndWait(msg, types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		if err != nil {
			fmt.Printf("工作流执行失败: %v\n", err)
			return
		}

		// 六、最后得到结果：将最终数据再解回 map[string]any
		var resultMap map[string]any
		conv.Unmarshal(msg.GetData(), &resultMap)

		fmt.Println("工作流执行成功！最终输出结果为：")
		for k, v := range resultMap {
			fmt.Printf("键: %s, 值: %v\n", k, v)
		}
	}))
}

func TestRuleOneActivityNode(t *testing.T) {
	// action 1: add
	addActor, err := action.MethodToActor[*AddReq, int](AddMethod, &action.ActMetaData{
		Namespace:    "test",
		Action:       "add",
		Desc:         "加法计算",
		RequiredArgs: []string{"a", "b"},
	})
	if err != nil {
		panic(err)
	}
	// action 2: concat
	concatActor, err := action.MethodToActor[*ConcatReq, string](ConcatMethod, &action.ActMetaData{
		Namespace:    "test",
		Action:       "concat",
		Desc:         "字符串拼接",
		RequiredArgs: []string{"s1", "s2"},
	})
	if err != nil {
		panic(err)
	}
	// action 3: logger
	loggerActor, err := action.MethodToActor[map[string]any, bool](LoggerMethod, &action.ActMetaData{
		Namespace: "test",
		Action:    "logger",
		Desc:      "日志打印",
	})
	if err != nil {
		panic(err)
	}

	err = action.Register(addActor, concatActor, loggerActor)
	if err != nil {
		panic(err)
	}

	activityList := `[
  {
	"activity_type": "test/add",
    "act_namespace": "test",
    "act_name": "add",
    "arguments": [
      {"key": "a", "value": 3, "policy": "default"},
      {"key": "b", "value": 5}
    ]
  },
  {
    "activity_type": "test/concat",
    "act_namespace": "test",
    "act_name": "concat"
  },
  {
    "activity_type": "test/logger",
    "act_namespace": "test",
    "act_name": "logger"
  }
]`
	activityDBList := make([]*activity.Activity, 0)
	err = conv.Unmarshal(activityList, &activityDBList)
	if err != nil {
		panic(err)
	}

	var chainConfig = `{
  "ruleChain": {
	"id": "activity_flow_01",
	"name": "多分支选择流程"
  },
  "metadata": {
	"nodes": [
	  { 
		"id": "testAdd1", 
		"type": "test/add|test/concat|test/logger", 
		"name": "数字相加",
		"configuration": {
		}
	  }
	],
	"connections": [
	  { "fromId": "testAdd1", "toId": "testConcat1", "type": "Success" }
	]
  }
}`

	engine, err := rulego.New("activity_flow_01", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	fmt.Println("初始化引擎")

	// 四、准备你的输入参数：map[string]any
	inputMap := &paramx.FlowContext{}
	inputMap.SetVariables(map[string]any{
		"a": 4,
		"b": 7,
	})

	msg := types.NewMsg(0, "USER_REGISTER", types.JSON, types.NewMetadata(), conv.String(inputMap))

	fmt.Println("--- 开始执行工作流 ---")

	// 五、直接同步执行并直接获取最终结果（使用 OnMsgWithEnd）
	engine.OnMsgAndWait(msg, types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		if err != nil {
			fmt.Printf("工作流执行失败: %v\n", err)
			return
		}

		// 六、最后得到结果：将最终数据再解回 map[string]any
		var resultMap map[string]any
		conv.Unmarshal(msg.GetData(), &resultMap)

		fmt.Println("工作流执行成功！最终输出结果为：")
		for k, v := range resultMap {
			fmt.Printf("键: %s, 值: %v\n", k, v)
		}
	}))

}
