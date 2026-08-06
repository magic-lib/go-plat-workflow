package rulegox_test

import (
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"testing"

	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

// ==========================================
// 定义条件判断节点：检查年龄 (CheckAge)
// ==========================================
type CheckAgeNode struct{}

func (n *CheckAgeNode) New() types.Node { return &CheckAgeNode{} }
func (n *CheckAgeNode) Type() string    { return "custom/checkAge" }
func (n *CheckAgeNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	return nil
}

func (n *CheckAgeNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	var data map[string]any
	if err := conv.Unmarshal(msg.GetData(), &data); err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	// 获取年龄进行条件判断
	age, ok := data["age"].(float64) // JSON 中的数字反序列化默认为 float64
	if !ok {
		ctx.TellFailure(msg, fmt.Errorf("age 字段类型不正确或不存在"))
		return
	}

	// ------ 条件判断核心逻辑 ------
	if age >= 18 {
		// 满足条件：走 Success 分支
		data["age_check_passed"] = true
		newData, _ := json.Marshal(data)
		msg.Data.SetBytes(newData)

		ctx.TellNext(msg, types.Success) // 相当于流向 "Success" 连线的节点
	} else {
		// 不满足条件：走自定义的 Reject 分支
		data["age_check_passed"] = false
		data["reason"] = "未满18岁，拒绝注册"
		newData, _ := json.Marshal(data)
		msg.Data.SetBytes(newData)

		ctx.TellNext(msg, "Reject") // 相当于流向 "Reject" 连线的节点
	}
}

func (n *CheckAgeNode) Destroy() {}

// ==========================================
// 定义被拒绝后的处理节点 (HandleReject)
// ==========================================
type HandleRejectNode struct{}

func (n *HandleRejectNode) New() types.Node { return &HandleRejectNode{} }
func (n *HandleRejectNode) Type() string    { return "custom/handleReject" }
func (n *HandleRejectNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	return nil
}
func (n *HandleRejectNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	fmt.Println("OnMsg:", msg.Type)

	var data map[string]any
	conv.Unmarshal(msg.GetData(), &data)

	fmt.Println("HandleRejectNode:", conv.String(msg))

	data["status"] = "rejected"
	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)

	ctx.TellNext(msg, types.Success)
}
func (n *HandleRejectNode) Destroy() {}

func TestRuleGo2(t *testing.T) {
	// 注册所有节点
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&CheckAgeNode{})
	rulego.Registry.Register(&SaveToDBNode{})
	rulego.Registry.Register(&HandleRejectNode{})

	// 新的带有条件分支的 JSON 配置
	var chainConfig = `{
	  "ruleChain": {
		"id": "conditional_flow_01",
		"name": "带条件判断的注册流程"
	  },
	  "metadata": {
		"nodes": [
		  { "id": "node_validate", "type": "custom/validateUser", "name": "1.验证数据" },
		  { "id": "node_check_age", "type": "custom/checkAge", "name": "2.年龄判断" },
		  { "id": "node_save", "type": "custom/saveToDB", "name": "3A.允许注册-存库" },
		  { "id": "node_reject", "type": "custom/handleReject", "name": "3B.拒绝注册-处理" }
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
			"type": "Success"
		  },
		  {
			"fromId": "node_check_age",
			"toId": "node_reject",
			"type": "Reject"
		  }
		]
	  }
	}`

	engine, err := rulego.New("chain_id_2", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	// 测试数据：未成年人
	inputMap := map[string]any{
		"username": "Jerry",
		"age":      16,
	}
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
