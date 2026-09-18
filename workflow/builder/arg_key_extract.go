package builder

import (
	"regexp"

	"github.com/rulego/rulego/api/types"
)

// argKeyRe 匹配 {{arguments.<name>}} 形式的占位符，捕获中间的参数名 name。
// 适用于任意出现在普通文本中的 {{arguments.xxx}} 片段（非严格 JSON 场景）。
var argKeyRe = regexp.MustCompile(`\{\{arguments\.([^}]+)}}`)

// ExtractArgumentKeys 从字符串中提取所有 {{arguments.xxx}} 占位符中间的参数名（xxx），
// 返回去重后的参数名数组，保持首次出现顺序。
// 例如输入 "fdfdsfds{{arguments.N000036__70lic.mobile}}fdsfsdfsdf\n{{arguments.mobile}}"
// 返回 ["N000036__70lic.mobile", "mobile"]。
func ExtractArgumentKeys(s string) []string {
	matches := argKeyRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		// m[1] 为括号捕获的参数名
		name := m[1]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		keys = append(keys, name)
	}
	return keys
}

// CollectAllInputArguments 遍历节点集合，从每个节点 configuration 的 arguments / responses 数组的
// 元素 value 字段中，提取所有 {{arguments.xxx}} 引用的入参名（去重、按首次出现顺序返回）。
//
// 结构约定（节点配置第一层）：
//   - arguments：param.BindConfig 数组，元素形如 {"key":..., "value":..., "policy":...}
//   - responses：返回值定义数组（outputs 转换而来），元素形如 {"key":..., "value":..., ...}
//   两者的 value 均为字符串，可能包含 {{arguments.xxx}} 形式的入参引用。
//
// 仅提取 value 为字符串且含 {{arguments.}} 的项；非字符串值（固定字面量/引用路径）不会误判为入参。
func CollectAllInputArguments(nodes []*types.RuleNode) []string {
	seen := make(map[string]bool)
	keys := make([]string, 0)

	addFromValue := func(v any) {
		s, ok := v.(string)
		if !ok {
			return
		}
		for _, k := range ExtractArgumentKeys(s) {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	scanArray := func(arr any) {
		list, ok := arr.([]any)
		if !ok {
			return
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			addFromValue(m["value"])
		}
	}

	for _, node := range nodes {
		if node == nil {
			continue
		}
		scanArray(node.Configuration["arguments"])
		scanArray(node.Configuration["responses"])
	}
	return keys
}

