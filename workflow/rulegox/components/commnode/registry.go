/*
 * Copyright 2023 The RuleGo Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package commnode

import (
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/plugins/paramx"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil/param"
	"github.com/rulego/rulego/api/types"
	"strings"
)

type CommConfiguration struct {
	NodeConfig  map[string]any      `json:"node_config"`
	Arguments   []*param.BindConfig `json:"arguments"`    //需要参数的定义
	Responses   []*param.BindConfig `json:"responses"`    //返回值的定义，节点执行完后对外输出的值
	ArgTemplate map[string]any      `json:"arg_template"` //参数映射，动态配置的，是在配置主或子链的时候添加的
	RetTemplate map[string]any      `json:"ret_template"` //返回值映射，动态配置的，是在配置主或子链的时候添加的
}

// NodeParams 根据传入的参数和节点配置，生成节点执行所需的参数映射
func NodeParams(allParamCtx *paramx.FlowContext, nodeId paramx.StepId, argTemplate map[string]any, arguments []*param.BindConfig) (map[string]any, error) {
	if len(arguments) > 0 {
		// 需要对value进行替换，如果含有变量的话
		ruleExpr := templates.NewRuleExprEngine()
		allMaps, _ := allParamCtx.ToMaps()
		for _, arg := range arguments {
			if arg == nil || arg.Value == "" {
				continue
			}
			val := conv.String(arg.Value)
			if strings.Contains(val, templates.DefaultPrefix) {
				valTemp, err := ruleExpr.RunString(val, allMaps)
				if err == nil {
					arg.Value = valTemp
				}
			}
		}
	}

	ret := allParamCtx.GetStepArguments(nodeId)
	if len(argTemplate) > 0 {
		for k, v := range argTemplate {
			ret[k] = v
		}
		retAny, err := allParamCtx.TemplateArguments(argTemplate)
		if err != nil {
			return ret, err
		}
		ret = retAny
	}
	retMap := param.MergeArgumentsByBinding(ret, arguments)
	return retMap, nil
}

// NodeResponses 根据节点的返回值定义（Configuration.responses）生成该节点对外输出的返回值映射。
// 取值规则按每一项的 Source 区分：
//   - value：直接使用配置的字面量 Value；
//   - ref_act / ref_node：Ref 为 {{...}} 引用路径，统一交给 TemplateArguments 按当前上下文求值。
//
// 未配置 responses 时返回 nil，调用方应保持原有返回值不变。
func NodeResponses(allParamCtx *paramx.FlowContext, nodeConfig *CommConfiguration) (map[string]any, error) {
	if nodeConfig == nil || len(nodeConfig.Responses) == 0 {
		return nil, nil
	}
	ret := make(map[string]any, len(nodeConfig.Responses))
	refTpl := make(map[string]any)
	for _, resp := range nodeConfig.Responses {
		if resp == nil || resp.Key == "" {
			continue
		}
		ret[resp.Key] = resp.Value // 默认按手工配置处理
	}
	if len(refTpl) == 0 {
		return ret, nil
	}
	if allParamCtx == nil {
		// 无上下文可解析引用，保留原始引用串，避免丢 key
		for k, v := range refTpl {
			ret[k] = v
		}
		return ret, nil
	}
	resolved, err := allParamCtx.TemplateArguments(refTpl)
	if err != nil {
		return ret, err
	}
	for k, v := range resolved {
		ret[k] = v
	}
	return ret, nil
}

var Registry = &types.SafeComponentSlice{}
