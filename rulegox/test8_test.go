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

func TestRuleGo8(t *testing.T) {
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})
	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
	"ruleChain": {
		"id": "xN_T0a9ASgmQ",
		"name": "新的名称",
		"root": true,
		"debugMode": true,
		"additionalInfo": {
			"description": "",
			"noDefaultInput": true,
			"layoutX": "403",
			"layoutY": "262"
		},
		"configuration": {}
	},
	"metadata": {
		"endpoints": [
			{
				"id": "node_2",
				"type": "endpoint/schedule",
				"name": "定时调度",
				"configuration": {},
				"debugMode": false,
				"additionalInfo": {
					"layoutX": 403,
					"layoutY": 322
				},
				"routers": [
					{
						"id": "9qpUpjVETmvIdA_A9Crof",
						"params": [
							"{\"id\":1}",
							"JSON"
						],
						"from": {
							"path": "*/10 * * * * *",
							"processors": []
						},
						"to": {
							"path": "xN_T0a9ASgmQ:node_4",
							"processors": [],
							"wait": false
						}
					}
				]
			}
		],
		"nodes": [
			{
				"id": "node_4",
				"type": "log",
				"name": "日志",
				"configuration": {
					"jsScript": "return 'Incoming message:\\n' + JSON.stringify(msg) + '\\nIncoming metadata:\\n' + JSON.stringify(metadata);"
				},
				"debugMode": false,
				"additionalInfo": {
					"layoutX": 884,
					"layoutY": 323,
					"description": "日志"
				}
			}
		],
		"connections": []
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
