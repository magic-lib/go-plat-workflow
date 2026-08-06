package task_test

import (
	"testing"
)

import "github.com/Bunny3th/easy-workflow/workflow/engine"

func DBConnConfig() {
	engine.DBConnConfigurator.DBConnectString = "数据库账号:密码@tcp(地址:端口)/数据库名称?charset=utf8mb4&parseTime=True&loc=Local"
}

// 示例事件
type MyEvent struct{}

// 节点结束事件
//func (e *MyEvent) MyEvent_End(ProcessInstanceID int, CurrentNode *Node, PrevNode Node) error {
//	//示例:在节点结束时打印信息
//	processName, err := GetProcessNameByInstanceID(ProcessInstanceID)
//	if err != nil {
//		return err
//	}
//	log.Printf("--------流程[%s]节点[%s]结束-------", processName, CurrentNode.NodeName)
//	return nil
//}

func TestDependsOnIds(t *testing.T) {
	engine.StartWorkFlow(DBConnConfig, false, &MyEvent{})
}
