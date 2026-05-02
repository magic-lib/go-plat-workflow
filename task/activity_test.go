package task_test

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/magic-lib/go-plat-workflow/task"
	"testing"
)

func TestDependsOnIds(t *testing.T) {
	oneAct := task.Activity{
		Id:        "",
		Namespace: "",
		Activity:  "",
		Arguments: []*param.BindConfig{
			{
				Key:   "name",
				Value: "{{ffff.responses.id}}",
			},
		},
		ArgTemplate: "{{aaaa.responses.id}}+{{return_bbbb.responses.mm}}+{{cccc.aa}}",
		Responses:   nil,
		Hooks:       nil,
		Control:     task.ActivityControl{},
	}

	testCases := []*utils.TestStruct{
		{"err", []any{0}, []any{"err:no id"}, func(n int) string {
			//task.WithReturnKeyPrefix("return_")

			list := oneAct.GetDependsOnIds()
			fmt.Println(list)
			return ""
		}},
	}
	utils.TestFunction(t, testCases, nil)
}
