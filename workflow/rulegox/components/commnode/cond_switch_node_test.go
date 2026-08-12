package commnode_test

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox/components/commnode"
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

func TestRuleGoCondNode(t *testing.T) {
	rulego.Registry.Register(&HandleAge10Node{})
	rulego.Registry.Register(&HandleAge15Node{})
	rulego.Registry.Register(&HandleAgeDefaultNode{})

	var chainConfig = `{
  "ruleChain": {
	"id": "switch_flow_01",
	"name": "多分支选择流程"
  },
  "metadata": {
	"nodes": [
	  { 
		"id": "n_switch", 
		"type": "custom/CondSwitch", 
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
	  { "fromId": "n_switch", "toId": "n_age10", "type": "Age10" },
	  { "fromId": "n_switch", "toId": "n_age15", "type": "Age15" },
	  { "fromId": "n_switch", "toId": "n_default", "type": "Default" }
	]
  }
}`

	// 三、初始化引擎实例
	engine, err := rulego.New("switch_id_1", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	fmt.Println("初始化引擎")

	// 四、准备你的输入参数：map[string]any
	inputMap := map[string]any{
		"age": 15,
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

func TestRuleGoCondNode11(t *testing.T) {
	dataStr := `{"arguments":{},"responses":{},"steps":{"N000004":{"arguments":{"audit_order_id":"555","mobile":"0978219056"},"responses":null}}}`
	allParams := &paramx.FlowContext{}
	err := conv.Unmarshal(dataStr, allParams)
	if err != nil {
		panic(err)
	}
	response := []*param.BindConfig{
		{
			Key:    "group_code",
			Value:  nil,
			Type:   "string",
			Policy: "",
		},
	}
	fmt.Println(response)

	nodeOldParam := allParams.GetStepArguments("N000004")
	newParam1 := &paramx.FlowContext{Arguments: nodeOldParam}

	newParam, err := commnode.NodeParams(newParam1, "N000004", nil, []*param.BindConfig{
		{
			Key:    "audit_order_id",
			Value:  "",
			Type:   "",
			Policy: "frontend",
		},
		{
			Key:    "mobile",
			Value:  "",
			Type:   "",
			Policy: "frontend+",
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(conv.String(newParam))
}
