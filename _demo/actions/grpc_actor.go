package actions

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/plugins/action"
)

//"server": "127.0.0.1:9000",
//"service": "ble.DataService",
//"method": "StreamData",
//"checkInterval": 10000,
//"headers": {
//"aaa": "bbbb"
//},
//"request": "{\"name\":\"aaa\"}"

// GrpcToActor 转换为Actor (TODO: 待实现真正的 gRPC 调用)
func GrpcToActor(ac *action.ActMetaData) (action.Actor, error) {
	return nil, fmt.Errorf("GrpcToActor not yet implemented")
}
