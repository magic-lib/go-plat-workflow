package action

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-curl/curl"
	"github.com/magic-lib/go-plat-utils/utils"
	"reflect"
)

// GrpcToActor 转换为Actor
func GrpcToActor(originReqFun func() *curl.Request, ac *ActMeta) (Actor, error) {
	if ac == nil {
		return nil, fmt.Errorf("data or curlReq is nil")
	}
	if ac.Activity == "" {
		return nil, fmt.Errorf("action name is empty")
	}

	var curlFunction = func(ctx context.Context, curlReq *curl.Request) (*curl.Response, error) {
		curlClient := curl.NewClient()
		var originReq *curl.Request
		if originReqFun != nil {
			originReq = originReqFun()
		}
		result := mergeCurlRequest(originReq, curlReq)
		resp := curlClient.NewRequest(result).Submit(ctx)
		if resp.Error != nil {
			return resp, resp.Error
		}
		return resp, nil
	}

	newMethod, err := utils.ContextMethodToAnyHandler[*curl.Request, *curl.Response](curlFunction)
	if err != nil {
		return nil, err
	}
	if ac.ArgumentType == nil {
		var zero *curl.Request
		ac.ArgumentType = reflect.TypeOf(zero)
	}
	ac.actionMethod = newMethod
	return ac, nil
}
