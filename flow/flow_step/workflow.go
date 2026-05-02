package flow_step

import "github.com/magic-lib/go-plat-workflow/task"

type (
	Workflow struct {
		Name       string           `yaml:"name" json:"name,omitempty"`             // 工作流名称
		Variables  map[string]any   `yaml:"variables" json:"variables,omitempty"`   // 全局变量
		Responses  map[string]any   `yaml:"responses" json:"responses,omitempty"`   // 请求最终返回的结构
		Activities []*task.Activity `yaml:"activities" json:"activities,omitempty"` //公共的activity资源，用于公共执行的部分,比如公共打日志，可以提高使用率
		Steps      []*Step          `yaml:"steps" json:"steps,omitempty"`
	}
)
