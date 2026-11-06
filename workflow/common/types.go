package common

const (
	ActivityNodeTypeName   = "custom/Activity"
	CondSwitchNodeTypeName = "custom/CondSwitch"
	// ReturnNodeTypeName 根链返回值节点类型标识：参数面板动态取自根链「返回值定义」，
	// 运行期把各分支赋值写入整条根链 FlowContext.Responses，实现多分支动态返回。
	ReturnValueNodeTypeName = "custom/ReturnValue"
	// RootChainCallNodeTypeName 调用根链节点类型标识：在链中同步调用另一条根链的当前发布版本，
	// 参数取目标根链的入参，返回值取目标根链的返回值，写入本节点的 steps.<实例ID>.responses。
	RootChainCallNodeTypeName = "custom/RootChainCall"
	// RootChainCallTargetKey 编排时选中的目标根链 ID 在 node_param_overrides 中的保留 key，
	// 组装 DSL 时移到节点 configuration 的 RootChainCallConfigKey，不作为参数传给目标根链。
	RootChainCallTargetKey = "__target_chain_id"
	// RootChainCallConfigKey 调用根链节点 DSL configuration 中目标根链 ID 的字段名。
	RootChainCallConfigKey = "target_chain_id"
)
