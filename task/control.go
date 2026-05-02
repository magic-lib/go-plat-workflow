package task

import (
	"context"
)

type (
	SortType    string
	OnExitType  string
	OnErrorType string

	LifecycleEvent string
	LifecycleHooks map[LifecycleEvent]*Activity
)

const (
	LifecycleEventOnStart    LifecycleEvent = "start"
	LifecycleEventOnComplete LifecycleEvent = "complete"
	LifecycleEventOnSuccess  LifecycleEvent = "success"
	LifecycleEventOnError    LifecycleEvent = "error"
	//LifecycleEventOnTimeout  LifecycleEvent = "timeout"
)

const (
	SortTypeActivity SortType = "activity"
	SortTypeSequence SortType = "sequence"
	SortTypeParallel SortType = "parallel"
)

const (
	OnErrorIgnore  OnErrorType = "ignore"   //遇到错误时，忽略错误，继续执行，比如打日志，或者是发消息，错误就错误了
	OnExitExit     OnExitType  = "exit"     // 直接退出，后续流程不再执行
	OnExitReturn   OnExitType  = "return"   // 直接退出，后续全部异步执行
	OnExitContinue OnExitType  = "continue" // 继续执行
)

type (
	RetryPolicy struct {
		MaxAttempts int `yaml:"max_attempts" json:"max_attempts"` // 最大尝试次数
		Interval    int `yaml:"interval" json:"interval"`         // 重试间隔时间，秒
	}

	Executable interface {
		Execute(ctx context.Context, vars map[string]any) (map[string]any, error)
	}
)

type (
	ActivityControl struct {
		When          string `yaml:"when" json:"when,omitempty"`                     // 执行该action的前提条件
		Timeout       int    `yaml:"timeout" json:"timeout,omitempty"`               // 秒，设置超时时间
		CacheTime     int    `yaml:"cache_time" json:"cache_time,omitempty"`         // 缓存时间，默认为秒
		DelayDuration int    `yaml:"delay_duration" json:"delay_duration,omitempty"` // 延时多长时间执行 ==0 立即执行，> 0 延时执行，<0 异步执行
		//RetryPolicy   RetryPolicy `yaml:"retry_policy" json:"retry_policy,omitempty"`   // 重试策略
	}

	FlowControl struct {
		Timeout       int `yaml:"timeout" json:"timeout,omitempty"`               // 秒，设置超时时间
		DelayDuration int `yaml:"delay_duration" json:"delay_duration,omitempty"` // 延时多长时间执行 ==0 立即执行，> 0 延时执行，<0 异步执行
		//RetryPolicy   RetryPolicy `yaml:"retry_policy" json:"retry_policy,omitempty"`   // 重试策略
		ExecutionOrder []SortType `yaml:"execution_order" json:"execution_order,omitempty"`

		OnError OnErrorType `yaml:"on_error" json:"on_error,omitempty"` // 如果出现执行错误了，是直接跳出，还是继续执行
		OnExit  OnExitType  `yaml:"on_exit" json:"on_exit,omitempty"`   // 是否执行完当前的Activity后，就直接返回，后续的则子流程
	}
)

// 判断是否在执行后立即退出
func (c *FlowControl) shouldExitOnExecute() bool {
	if c == nil {
		return false // 默认不退出
	}
	return c.OnExit == OnExitExit
}

// 判断是否在执行后继续执行后续流程
func (c *FlowControl) shouldContinueOnExecute() bool {
	if c == nil {
		return false // 默认不退出
	}
	return c.OnExit == OnExitContinue
}

// 直接返回，后面流程异步
func (c *FlowControl) shouldReturnOnExecute() bool {
	if c == nil {
		return false // 默认不退出
	}
	return c.OnExit == OnExitReturn
}
func (c *FlowControl) shouldIgnoreOnError() bool {
	if c == nil {
		return false
	}
	return c.OnError == OnErrorIgnore
}

var (
	_ Executable = (*Activity)(nil)
	//_ Executable = (Sequence)(nil)
	//_ Executable = (Parallel)(nil)
)
