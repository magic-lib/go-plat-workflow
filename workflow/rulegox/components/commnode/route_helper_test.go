package commnode

import (
	"fmt"
	"testing"

	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/rulego/rulego/api/types"
)

func TestRouteByCondition(t *testing.T) {
	ruleObj := templates.NewRuleExprEngine()

	cases := []struct {
		name    string
		expr    string
		params  map[string]any
		wantRT  string
		wantErr bool
	}{
		{
			name:   "bool true -> True",
			expr:   "[responses.in_blacklist] == true",
			params: map[string]any{"responses": map[string]any{"in_blacklist": true}},
			wantRT: types.True,
		},
		{
			name:   "bool false -> False",
			expr:   "[responses.in_blacklist] == true",
			params: map[string]any{"responses": map[string]any{"in_blacklist": false}},
			wantRT: types.False,
		},
		{
			name:   "string -> 自定义 relationType",
			expr:   "[responses.code]",
			params: map[string]any{"responses": map[string]any{"code": "blacklist_hit"}},
			wantRT: "blacklist_hit",
		},
		{
			name:   "数字 1 -> 自定义 relationType \"1\"",
			expr:   "[responses.count]",
			params: map[string]any{"responses": map[string]any{"count": 1}},
			wantRT: "1",
		},
		{
			name:   "数字 0 -> 自定义 relationType \"0\"",
			expr:   "[responses.count]",
			params: map[string]any{"responses": map[string]any{"count": 0}},
			wantRT: "0",
		},
		{
			name:   "算术结果 -> 自定义 relationType",
			expr:   "[responses.count] + 1",
			params: map[string]any{"responses": map[string]any{"count": 199}},
			wantRT: "200",
		},
		{
			name:   "小数 -> 自定义 relationType",
			expr:   "[responses.score]",
			params: map[string]any{"responses": map[string]any{"score": 2.5}},
			wantRT: "2.5",
		},
		{
			name:   "模板替换的数字 -> 自定义 relationType",
			expr:   "{{responses.count}}",
			params: map[string]any{"responses": map[string]any{"count": 3}},
			wantRT: "3",
		},
		{
			name:   "浮点误差 -> 四舍五入后匹配",
			expr:   "0.1 + 0.2",
			params: map[string]any{},
			wantRT: "0.3",
		},
		{
			name:   "除不尽 -> 保留 6 位小数",
			expr:   "10 / 3",
			params: map[string]any{},
			wantRT: "3.333333",
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
func TestNumberToRelationType(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{3.0000000001, "3"},
		{4.0, "4"},
		{3.3333333333, "3.333333"},
		{2.9999999999, "3"},
		{85.5, "85.5"},
		{0.1 + 0.2, "0.3"},
		{-1.25, "-1.25"},
		{-0.0000001, "0"},
		{1e21, "1000000000000000000000"},
		{float32(2.5), "2.5"},
		{200, "200"},
		{int64(-7), "-7"},
	}
	for _, c := range cases {
		if got := numberToRelationType(c.in); got != c.want {
			t.Errorf("numberToRelationType(%v) 期望 %q，实际 %q", c.in, c.want, got)
		}
	}
}

func TestRouteByCondition11(t *testing.T) {
	ruleObj := templates.NewRuleExprEngine()
	expr := "SwitchExpr((In('K1', [responses.tag_list]) || In('K2', [responses.tag_list])  || In('K3', [responses.tag_list])), 'SET_T_MOBILE_DEFAULT', In('P1', [responses.tag_list]), 'SET_T_BANK_DEFAULT', 'SKIP')"

	params := map[string]any{
		"responses": map[string]any{
			"tag_list": []string{"M301", "M3", "N", "P1"},
		},
		//"responses.tag_list": []string{"M301", "M3", "N", "P1"},
	}
	//paramsStr := `{"arguments":{"account_user_id":2748,"mobile":"0571876472","user_id":"2748"},"end_time_ms":1789557314131,"error":null,"responses":{"tag_list":["M301","M3","N","P1"]},"start_time_ms":1789557313053,"status":"success"}`
	//_ = conv.Unmarshal([]byte(paramsStr), &params)

	aa, err := ruleObj.RunString(expr, params)
	fmt.Print(aa, err)
}
