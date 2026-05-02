package flow_step

import "github.com/magic-lib/go-plat-workflow/task"

// Step 表示工作流中的一个步骤,包含一组条件触发的actions
type Step struct {
	Id               string           `yaml:"id" json:"id,omitempty"`                 // 该步骤Id
	Name             string           `yaml:"name" json:"name,omitempty"`             // 步骤名称
	DependsOn        []*task.Activity `yaml:"depends_on" json:"depends_on,omitempty"` // 手动填入依赖
	Condition        string           `yaml:"condition" json:"condition,omitempty"`   // 执行条件表达式
	task.FlowControl                  // 流程控制
	Strategy         []*task.Activity `yaml:"strategy" json:"strategy,omitempty"` // 策略列表
}
