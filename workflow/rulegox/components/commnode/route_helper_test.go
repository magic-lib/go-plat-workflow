package commnode

import (
	"testing"

	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/rulego/rulego/api/types"
)

func TestRouteByCondition(t *testing.T) {
	ruleObj := templates.NewRuleExprEngine()

	cases := []struct {
		name     string
		expr     string
		params   map[string]any
		wantRT   string
		wantErr  bool
	}{
		{
			name:   "bool true -> True",
			expr:   "responses.in_blacklist == true",
			params: map[string]any{"responses": map[string]any{"in_blacklist": true}},
			wantRT: types.True,
		},
		{
			name:   "bool false -> False",
			expr:   "responses.in_blacklist == true",
			params: map[string]any{"responses": map[string]any{"in_blacklist": false}},
			wantRT: types.False,
		},
		{
			name:   "string -> 自定义 relationType",
			expr:   "responses.code",
			params: map[string]any{"responses": map[string]any{"code": "blacklist_hit"}},
			wantRT: "blacklist_hit",
		},
		{
			name:   "数字 1 -> 可转 bool -> Success",
			expr:   "responses.count",
			params: map[string]any{"responses": map[string]any{"count": 1}},
			wantRT: types.Success,
		},
		{
			name:   "数字 0 -> 可转 bool -> Failure",
			expr:   "responses.count",
			params: map[string]any{"responses": map[string]any{"count": 0}},
			wantRT: types.Failure,
		},
		{
			name:    "语法错误 -> 报错",
			expr:    "responses.x == ",
			params:  map[string]any{"responses": map[string]any{}},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt, _, err := routeByCondition(ruleObj, c.expr, c.params)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错，但无错误，rt=%s", rt)
				}
				return
			}
			if err != nil {
				t.Fatalf("未期望报错: %v", err)
			}
			if rt != c.wantRT {
				t.Fatalf("relationType 期望 %q，实际 %q", c.wantRT, rt)
			}
		})
	}
}
