package tools

import (
	"fmt"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-workflow/common"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/samber/lo"
	"regexp"
)

var (
	activityIdRegExpMap = cmap.New[*regexp.Regexp]() //缓存，提高效率
)

func createMap(data any) map[string]any {
	m := make(map[string]any)
	_ = conv.Unmarshal(conv.String(data), &m)
	return m
}

func CloneMap(data map[string]any) map[string]any {
	m := make(map[string]any)
	err := conv.Unmarshal(conv.String(data), &m)
	if err == nil {
		return m
	}
	return lo.Assign(data)
}

func MergeNewArguments(oldArgs map[string]any, args map[string]any) map[string]any {
	if len(args) == 0 {
		return oldArgs
	}
	jsonTemplate := templates.NewJsonMapTemplate()
	newArgs, err := jsonTemplate.Replace(oldArgs, args)
	if err == nil {
		_ = conv.Unmarshal(newArgs, &oldArgs)
	}
	for key, val := range args {
		if _, ok := oldArgs[key]; !ok {
			oldArgs[key] = val
		}
	}
	return oldArgs
}

func getResponseRegExp(keyPrefix string) *regexp.Regexp {
	re, ok := activityIdRegExpMap.Get(keyPrefix)
	if ok && re != nil {
		return re
	}
	retStr := fmt.Sprintf(`\{\{%s([a-zA-Z0-9_]+)\.(%s|%s)\.`, keyPrefix, common.Arguments, common.Responses)
	re = regexp.MustCompile(retStr)
	activityIdRegExpMap.Set(keyPrefix, re)
	return re
}

// ExtractDependsActivityIds 解析出待依赖的所有ActivityId
func ExtractDependsActivityIds(condition string, keyPrefix string) []string {
	actIdList := make([]string, 0)
	re := getResponseRegExp(keyPrefix)
	matches := re.FindAllStringSubmatch(condition, -1)
	for _, match := range matches {
		if len(match) > 1 {
			activityID := match[1]
			actIdList = append(actIdList, activityID)
		}
	}
	return actIdList
}
