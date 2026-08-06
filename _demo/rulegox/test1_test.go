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
// 1. 定义节点一：验证用户输入 (ValidateUser)
// ==========================================
type ValidateUserNode struct{}

// New 创建节点实例
func (n *ValidateUserNode) New() types.Node {
	return &ValidateUserNode{}
}

// Type 定义节点在 JSON 配置文件里的唯一标识/类型名
func (n *ValidateUserNode) Type() string {
	return "custom/validateUser"
}

func (n *ValidateUserNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	fmt.Println("ValidateUserNode init ruleConfig:", conv.String(ruleConfig))
	fmt.Println("ValidateUserNode init configuration:", conv.String(configuration))

	return nil
}

// OnMsg 核心执行方法（对应你的 Go 方法逻辑）
func (n *ValidateUserNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	// a. 将传入的 Payload 解析为 map[string]any
	var data map[string]any
	if err := conv.Unmarshal(msg.GetData(), &data); err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	// b. 你的业务逻辑：验证用户名是否存在
	username, ok := data["username"].(string)
	if !ok || username == "" {
		ctx.TellFailure(msg, fmt.Errorf("username 不能为空"))
		return
	}

	// c. 修改或丰富数据（例如打上标签）
	data["is_validated"] = true

	// d. 将数据打包回去并传给下一个节点
	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)

	// 告知引擎：当前节点成功，沿着 "Success" 关系线走向下一个节点
	ctx.TellNext(msg, types.Success)
}

func (n *ValidateUserNode) Destroy() {}

// ==========================================
// 2. 定义节点二：模拟保存数据库 (SaveToDB)
// ==========================================
type SaveToDBNode struct{}

func (n *SaveToDBNode) New() types.Node { return &SaveToDBNode{} }
func (n *SaveToDBNode) Type() string    { return "custom/saveToDB" }
func (n *SaveToDBNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	fmt.Println("SaveToDBNode init ruleConfig:", conv.String(ruleConfig))
	fmt.Println("SaveToDBNode init configuration:", conv.String(configuration))
	return nil
}

func (n *SaveToDBNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	var data map[string]any
	conv.Unmarshal(msg.GetData(), &data)

	// 你的业务逻辑：模拟生成用户 ID 并写入数据
	data["user_id"] = 9527
	data["status"] = "saved_success"

	newData, _ := json.Marshal(data)
	msg.Data.SetBytes(newData)
	ctx.TellNext(msg, types.Success)
}

func (n *SaveToDBNode) Destroy() {}

// ==========================================
// 3. 主程序：注册、加载、运行
// ==========================================
func TestRuleGo1(t *testing.T) {
	// 一、注册你自定义的 Go 节点方法
	// 注册后，RuleGo 官方配套的拖拽前端就能识别并在配置中使用它们
	rulego.Registry.Register(&ValidateUserNode{})
	rulego.Registry.Register(&SaveToDBNode{})

	fmt.Println("注册成功")

	// 二、定义你的工作流 JSON（可由前端拖拽生成）
	// 这里通过配置把 custom/validateUser 和 custom/saveToDB 串联起来
	var chainConfig = `{
	  "ruleChain": {
		"id": "user_flow_01",
		"name": "用户注册流程"
	  },
	  "metadata": {
		"nodes": [
		  {
			"id": "node_validate",
			"type": "custom/validateUser",
			"name": "验证用户数据"
		  },
		  {
			"id": "node_save",
			"type": "custom/saveToDB",
			"name": "保存至数据库"
		  }
		],
		"connections": [
		  {
			"fromId": "node_validate",
			"toId": "node_save",
			"type": "Success"
		  }
		]
	  }
	}`

	// 三、初始化引擎实例
	engine, err := rulego.New("chain_id_1", []byte(chainConfig))
	if err != nil {
		panic(err)
	}

	fmt.Println("初始化引擎")

	// 四、准备你的输入参数：map[string]any
	inputMap := map[string]any{
		"username": "Tom",
		"age":      18,
	}

	// 转为 RuleGo 需要的序列化字符串
	inputJson, _ := json.Marshal(inputMap)

	// 创建规则消息体
	msg := types.NewMsg(0, "USER_REGISTER", types.JSON, types.NewMetadata(), string(inputJson))

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
