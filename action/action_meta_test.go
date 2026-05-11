package action_test

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-curl/curl"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/crypto"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/magic-lib/go-plat-workflow/action"
	"github.com/samber/lo"
	"log"
	"net/http"
	"testing"
	"time"
)

type Order struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

type ApiAccountInfoReq struct {
	Time  int64  `json:"time"`
	Key   string `json:"key"`
	Accno string `json:"accno"`
	Mid   int64  `json:"mid"`
	Nid   string `json:"nid"`
	Name  string `json:"name"`
	From  string `json:"from"`
	Bcode string `json:"bcode"`
	Cache int    `json:"cache"`
}

func (o *Order) GetOrderName(ctx context.Context, id int) (string, error) {
	if id == 0 {
		return "", fmt.Errorf("err:no id")
	}
	return fmt.Sprintf("%d", id+1), nil
}

func (o *Order) GetMemberGroup(ctx context.Context, id int) (string, error) {
	if id == 0 {
		return "", fmt.Errorf("err:no id")
	}
	return "M8", nil
}
func (o *Order) GetOrderInfo(ctx context.Context, or *Order) (*Order, error) {
	return or, nil
}

func (o *Order) SetOrderInfo(ctx context.Context, orderId string) (bool, error) {
	fmt.Println(orderId)
	return true, nil
}
func (o *Order) Logger(ctx context.Context, or map[string]any) (bool, error) {
	log.Print("Logger aaaaaaa: ", conv.String(or))
	return true, nil
}

var ns = "order"

func registerAction() {
	orderModel := &Order{
		Name: "tianlin999777",
	}
	getOrderNameInterface, err := action.MethodToActor[int, string](orderModel.GetOrderName, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetOrderName",
		Desc:         "获取订单名称",
		RequiredArgs: []string{"name", "age"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getMemberGroupInterface, err := action.MethodToActor[int, string](orderModel.GetMemberGroup, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetMemberGroup",
		Desc:         "获取用户客群",
		RequiredArgs: []string{"name", "age"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	setOrderInfoInterface, err := action.MethodToActor[string, bool](orderModel.SetOrderInfo, &action.ActMeta{
		Namespace:    ns,
		Activity:     "SetOrderInfo",
		Desc:         "设置订单信息",
		RequiredArgs: nil,
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getOrderInfoInterface, err := action.MethodToActor[*Order, *Order](orderModel.GetOrderInfo, &action.ActMeta{
		Namespace:    ns,
		Activity:     "GetOrderInfo",
		Desc:         "获取订单信息",
		RequiredArgs: []string{"name", "group"},
		RequiredResp: nil,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	getLoggerInterface, err := action.MethodToActor[*Order, bool](orderModel.Logger, &action.ActMeta{
		Activity: "Log",
		Desc:     "日志",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	curlInterface, err := action.CurlToActor(func() *curl.Request {
		token := "rkbKlwl9OL0ew2cZ6m"

		req := new(ApiAccountInfoReq)

		req.Time = time.Now().Unix()
		req.Key = crypto.Md5(fmt.Sprintf("%d%s", req.Time, token))

		return &curl.Request{
			Url:    "https://public.biucn.xyz/api/public/account_query",
			Method: http.MethodPost,
			Data:   req,
		}
	}, &action.ActMeta{
		Namespace: ns,
		Activity:  "query",
		Desc:      "日志",
	})

	//rkbKlwl9OL0ew2cZ6m
	if err != nil {
		fmt.Println(err)
		return
	}

	actionList := []action.Actor{
		getOrderNameInterface,
		setOrderInfoInterface,
		getMemberGroupInterface,
		getOrderInfoInterface,
		getLoggerInterface,
		curlInterface,
	}

	lo.ForEach(actionList, func(item action.Actor, _ int) {
		err = action.RegisterAction(item)
		if err != nil {
			fmt.Println(err)
			return
		}
	})
}

func TestActionRegister(t *testing.T) {
	registerAction()

	curlQuery, err := action.GetAction(ns, "query")
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := curlQuery.Execute(context.Background(), &curl.Request{
		Data: map[string]any{
			"accno": "0771340782",
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conv.String(resp))

	actionFun, err := action.GetAction(ns, "GetOrderName")
	if err != nil {
		fmt.Println(err)
		return
	}
	//functionName := actionFun.Name()
	//fmt.Println(functionName)

	testCases := []*utils.TestStruct{
		{"err", []any{0}, []any{"err:no id"}, func(n int) string {
			_, err := actionFun.Execute(context.Background(), n)
			if err != nil {
				return err.Error()
			}
			return ""
		}},
		{"int", []any{5}, []any{"6"}, func(n int) string {
			intStr, err := actionFun.Execute(context.Background(), n)
			if err != nil {
				return err.Error()
			}
			return conv.String(intStr)
		}},
		{"int2", []any{"7"}, []any{"8"}, func(n int) string {
			fmt.Println("\nexecute:")
			mm, err := actionFun.Execute(context.Background(), "7")
			if err != nil {
				return err.Error()
			}
			return conv.String(mm)
		}},
	}
	utils.TestFunction(t, testCases, nil)
}
