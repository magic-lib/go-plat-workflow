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
	// ParamInputNodeTypeName 参数输入节点类型标识：编排时为每个实例动态定义参数列表（参数名/类型/说明），
	// 运行时按各参数配置的来源解析出值，写入本节点的 steps.<实例ID>.arguments 与 responses，供下游节点引用。
	ParamInputNodeTypeName = "custom/ParamInput"
	// ParamInputDefsKey 编排时参数输入节点的参数定义（JSON 数组）在 node_param_overrides 中的保留 key，
	// 组装 DSL 时移到节点 configuration 的 ParamInputConfigKey，不作为参数解析。
	ParamInputDefsKey = "__input_params"
	// ParamInputConfigKey 参数输入节点 DSL configuration 中参数定义的字段名。
	ParamInputConfigKey = "input_params"
	// LoopNodeTypeName 循环节点类型标识：按遍历数组 / 固定次数 / 条件三种模式重复执行循环体，
	// 循环体可以是一条子链（共享执行上下文）或一条已发布的根链（按入参映射调用）。
	LoopNodeTypeName = "custom/Loop"
	// LoopConfigKey 编排时循环配置（JSON 对象）在 node_param_overrides 中的保留 key，
	// 组装 DSL 时移到节点 configuration 的 LoopConfigField，不作为入参。
	LoopConfigKey = "__loop_config"
	// LoopConfigField 循环节点 DSL configuration 中循环配置的字段名。
	LoopConfigField = "loop"
	// LoopVarKey 每轮执行时写入 arguments 的循环变量名，循环体内用 {{arguments._loop.item}} 等引用；
	// 它不是调用方入参，提取必传入参时跳过。
	LoopVarKey = "_loop"
	// LoopBodySubChain / LoopBodyRootChain 循环体类型：子链 / 已发布的根链。
	LoopBodySubChain  = "subchain"
	LoopBodyRootChain = "rootchain"
)
