package action

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-curl/curl"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/samber/lo"
	"net/http"
	"reflect"
)

// CurlToActor 转换为Actor
func CurlToActor(originReqFun func() *curl.Request, respCreateFun func(resp *curl.Response) (any, error), ac *ActMeta) (Actor, error) {
	if ac == nil {
		return nil, fmt.Errorf("data or curlReq is nil")
	}
	if ac.Activity == "" {
		return nil, fmt.Errorf("action name is empty")
	}

	var curlFunction = func(ctx context.Context, curlReq *curl.Request) (any, error) {
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
		if respCreateFun != nil {
			retData, err := respCreateFun(resp)
			if err != nil {
				return resp, err
			}
			return retData, nil
		}
		return resp, nil
	}

	newMethod, err := utils.ContextMethodToAnyHandler[*curl.Request, any](curlFunction)
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

// mergeCurlRequest 如果curlReq某字段为空，则继承originReq里的值
func mergeCurlRequest(originReq, curlReq *curl.Request) *curl.Request {
	if originReq == nil {
		return curlReq
	}
	if curlReq == nil {
		return originReq
	}

	result := &curl.Request{}

	if curlReq.Url == "" && originReq.Url != "" {
		result.Url = originReq.Url
	} else {
		result.Url = curlReq.Url
	}

	if curlReq.Method == "" && originReq.Method != "" {
		result.Method = originReq.Method
	} else {
		result.Method = curlReq.Method
	}
	headers := make(http.Header)
	if len(originReq.Header) > 0 {
		for k, _ := range originReq.Header {
			headers.Add(k, originReq.Header.Get(k))
		}
	}
	if len(curlReq.Header) > 0 {
		for k, _ := range curlReq.Header {
			headers.Add(k, curlReq.Header.Get(k))
		}
	}
	result.Header = headers

	if curlReq.Data == nil {
		result.Data = originReq.Data
	} else if originReq.Data == nil {
		result.Data = curlReq.Data
	} else if curlReq.Data != nil && originReq.Data != nil {
		originMap := make(map[string]any)
		_ = conv.Unmarshal(originReq.Data, &originMap)
		nowMap := make(map[string]any)
		_ = conv.Unmarshal(curlReq.Data, &nowMap)
		result.Data = lo.Assign(originMap, nowMap)
	}

	return result
}
