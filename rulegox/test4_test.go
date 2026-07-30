package rulegox_test

import (
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

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
	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)

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
	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)

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
	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)

	ctx.TellNext(msg, types.Success)
}
func (n *HandleAgeDefaultNode) Destroy() {}

func TestRuleGo4(t *testing.T) {
	// 1. 注册纯业务的 Go 节点（不需要注册条件判断节点了，RuleGo 内置了 jsFilter）
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})
	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
  "ruleChain": {
	"id": "switch_flow_01",
	"name": "多分支选择流程"
  },
  "metadata": {
	"nodes": [
	  { "id": "n_validate", "type": "custom/validateUser" },
	  { 
		"id": "n_switch", 
		"type": "jsSwitch", 
		"name": "年龄条件分流器",
		"configuration": {
		  "jsScript": "if (msg.age == 10) return ['Age10']; else if (msg.age == 15) return ['Age15']; else return ['Default'];"
		}
	  },
	  { "id": "n_age10", "type": "custom/handleAge10", "name": "处理10岁逻辑" },
	  { "id": "n_age15", "type": "custom/handleAge15", "name": "处理15岁逻辑" },
	  { "id": "n_default", "type": "custom/handleDefault", "name": "处理其他年龄"}
	],
	"connections": [
	  { "fromId": "n_validate", "toId": "n_switch", "type": "Success" },
	  
	  { "fromId": "n_switch", "toId": "n_age10", "type": "Age10" },
	  { "fromId": "n_switch", "toId": "n_age15", "type": "Age15" },
	  { "fromId": "n_switch", "toId": "n_default", "type": "Default" }
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

func TestRuleGo5(t *testing.T) {
	// 1. 注册纯业务的 Go 节点（不需要注册条件判断节点了，RuleGo 内置了 jsFilter）
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})
	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
  "ruleChain": { "id": "parallel_filter_flow" },
  "metadata": {
	"nodes": [
	  { "id": "n_validate", "type": "custom/validateUser" },
	  
	  { "id": "f_10", "type": "jsFilter", "configuration": { "jsScript": "return msg.age == 10;" } },
	  { "id": "f_15", "type": "jsFilter", "configuration": { "jsScript": "return msg.age == 15;" } },
	  
	  { "id": "n_age10", "type": "custom/handleAge10" },
	  { "id": "n_age15", "type": "custom/handleAge15" }
	],
	"connections": [
	  { "fromId": "n_validate", "toId": "f_10", "type": "Success" },
	  { "fromId": "n_validate", "toId": "f_15", "type": "Success" },

	  { "fromId": "f_10", "toId": "n_age10", "type": "True" },
	  { "fromId": "f_15", "toId": "n_age15", "type": "True" }
	]
  }
}`

	engine, _ := rulego.New("chain_id_3", []byte(chainConfig))

	// 3. 测试输入：16岁
	inputMap := map[string]any{"username": "Jerry", "age": 18}
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
