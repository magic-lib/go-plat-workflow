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
	// SubChainCallNodeTypeName 调用子链节点类型标识：根链（或子链）里添加的子链实例按普通节点处理，
	// 入参按本实例的参数配置解析后作为子链内的 arguments，子链返回值（子链里返回值节点写入的值）
	// 写入本节点的 steps.<实例ID>.responses，供下游节点引用。旧数据里的 flow 节点仍按 rulego 原生子链节点执行。
	SubChainCallNodeTypeName = "custom/SubChainCall"
)
