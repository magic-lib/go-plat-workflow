package common

const (
	ActivityNodeTypeName   = "custom/Activity"
	CondSwitchNodeTypeName = "custom/CondSwitch"
	// ReturnNodeTypeName 根链返回值节点类型标识：参数面板动态取自根链「返回值定义」，
	// 运行期把各分支赋值写入整条根链 FlowContext.Responses，实现多分支动态返回。
	ReturnValueNodeTypeName = "custom/ReturnValue"
)
