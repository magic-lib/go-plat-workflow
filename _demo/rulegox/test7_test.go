package rulegox_test

import (
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	_ "github.com/magic-lib/go-plat-utils/plugins/rulegox/components/commnode"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

func TestRuleGo7(t *testing.T) {
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})
	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
  "ruleChain": { "id": "expr_switch_flow" },
  "metadata": {
	"nodes": [
	  { "id": "n_validate", "type": "custom/validateUser" },
	  {
		"id": "node_my_router",
		"type": "condRouter",
		"name": "极简年龄路由器",
		"configuration": {
			"condition": "Switch(age, 10, 'Age10', 15, 'Age15', 'Default')"
		}
      },
	  { "id": "n_age10", "type": "custom/handleAge10" },
	  { "id": "n_age15", "type": "custom/handleAge15" }
	],
	"connections": [
	  { "fromId": "n_validate", "toId": "node_my_router", "type": "Success" },
	  
	  { "fromId": "node_my_router", "toId": "n_age10", "type": "Age10" },
	  { "fromId": "node_my_router", "toId": "n_age15", "type": "Age15" }
	]
  }
}`

	engine, err := rulego.New("chain_id_3", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	// 3. 测试输入：16岁
	inputMap := map[string]any{"username": "Jerry", "age": 15}
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
