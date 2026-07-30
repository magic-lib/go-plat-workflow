package rulegox_test

import (
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

func TestRuleGo3(t *testing.T) {
	// 1. 注册纯业务的 Go 节点（不需要注册条件判断节点了，RuleGo 内置了 jsFilter）
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
  "ruleChain": {
	"id": "conditional_flow_json_config",
	"name": "动态条件控制流程"
  },
  "metadata": {
	"nodes": [
	  { 
		"id": "node_validate", 
		"type": "custom/validateUser", 
		"name": "1.验证数据" 
	  },
	  { 
		"id": "node_check_age", 
		"type": "jsFilter", 
		"name": "2.动态条件判断",
		"configuration": {
		  "jsScript": "return msg.age >= 18;"
		}
	  },
	  { 
		"id": "node_save", 
		"type": "custom/saveToDB", 
		"name": "3A.条件成立-存库" 
	  },
	  { 
		"id": "node_reject", 
		"type": "custom/handleReject", 
		"name": "3B.条件不成立-拒绝" 
	  }
	],
	"connections": [
	  {
		"fromId": "node_validate",
		"toId": "node_check_age",
		"type": "Success"
	  },
	  {
		"fromId": "node_check_age",
		"toId": "node_save",
		"type": "True"
	  },
	  {
		"fromId": "node_check_age",
		"toId": "node_reject",
		"type": "False"
	  }
	]
  }
}`

	engine, _ := rulego.New("chain_id_3", []byte(chainConfig))

	// 3. 测试输入：16岁
	inputMap := map[string]any{"username": "Jerry", "age": 16}
	inputJson, _ := json.Marshal(inputMap)
	msg := types.NewMsg(0, "USER_REGISTER", types.JSON, types.NewMetadata(), string(inputJson))

	engine.OnMsgAndWait(msg, types.WithOnEnd(func(ctx types.RuleContext, msg types.RuleMsg, err error, relationType string) {
		fmt.Println("OnMsgAndWait:", relationType)

		var resultMap map[string]any
		conv.Unmarshal(msg.GetData(), &resultMap)
		fmt.Println("工作流最终输出结果：")
		for k, v := range resultMap {
			fmt.Printf("%s: %v\n", k, v)
		}
	}))
}
