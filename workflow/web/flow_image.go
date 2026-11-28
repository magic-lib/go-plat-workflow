package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// 流程执行路径图片（/api/flow-view/image）：凭 /flow-view 的只读分享 token 生成 PNG/SVG 图片，
// 供业务后台调用后保存，日后查看。参数与分享链接一致：share、trace_id（chain_id/project 以 token 为准）。
//
// 画法与 /flow-view 页面保持一致：节点按种类配色，路由节点显示路由条件，执行过的节点与连线标红并显示耗时；
// 同一 trace_id 下多次执行时与页面一样合并画在一张图里。图由 Graphviz 的 dot 命令生成，服务器需安装
// graphviz 与中文字体（Dockerfile 运行镜像已安装 graphviz、font-wqy-zenhei）。
const (
	flowImageDotBin = "dot"
	// flowImageFont 图片字体（运行镜像安装的文泉驿正黑）：找不到时 Pango 会自动回退到系统里可用的中文字体
	flowImageFont = "WenQuanYi Zen Hei"
	// flowImageTimeout 单次 dot 生成的最长耗时
	flowImageTimeout = 30 * time.Second
	// flowImageMaxConcurrent 同时生成图片的最大数量，避免突发调用把机器 CPU 打满
	flowImageMaxConcurrent = 4
)

var (
	errFlowImageDotMissing = errors.New("服务器未安装 graphviz（dot 命令），无法生成图片")
	flowImageSem           = make(chan struct{}, flowImageMaxConcurrent)
	flowImageFileNameRe    = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
)

// flowImageFormats 支持的图片格式及其 Content-Type
var flowImageFormats = map[string]string{
	"png": "image/png",
	"svg": "image/svg+xml",
}

type flowImagePalette struct{ fill, stroke, color string }

// 节点配色与 flow_view.js 的 KIND_PALETTE / ROUTE_FILL 一致
var (
	flowImageKindPalette = map[string]flowImagePalette{
		"condition": {"#dcfce7", "#16a34a", "#14532d"}, // 查询获取=浅绿
		"action":    {"#fee2e2", "#dc2626", "#7f1d1d"}, // 策略执行=淡红
		"return":    {"#fef9c3", "#ca8a04", "#713f12"}, // 返回值节点=浅黄
		"call":      {"#e0f2fe", "#0891b2", "#164e63"}, // 调用根链=浅青
		"input":     {"#f3e8ff", "#9333ea", "#581c87"}, // 参数输入=浅紫
	}
	flowImageKindFallback = flowImagePalette{"#f3f4f6", "#9ca3af", "#374151"}
	flowImageRoutePalette = flowImagePalette{"#dbeafe", "#2563eb", "#1e3a8a"}
	flowImageSubPalette   = flowImagePalette{"#ede9fe", "#7c3aed", "#4c1d95"}
)

const (
	flowImageTraceColor = "#ef4444"
	flowImageErrorColor = "#7f1d1d"
)

type flowImageConn struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type"`
}

// flowImageRun 节点的一次执行：order 为该次执行在整条 trace 中的步序（从 1 开始）
type flowImageRun struct {
	order int
	log   flowShareLog
}

type flowImageTraceNode struct {
	runs  []flowImageRun
	error bool
}

// flowImageTrace 执行路径：执行过的节点与走过的连线（key 为 from|to）
type flowImageTrace struct {
	steps int
	nodes map[string]*flowImageTraceNode
	edges map[string]bool
}

func splitFlowIDs(s string) []string {
	out := []string{}
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func flowConnType(t string) string {
	if t == "" {
		return "Success"
	}
	return t
}

// buildFlowImageTrace 与 flow_view.js 的 sortTraceLogs + buildTracePath 口径一致：
// 去掉汇总行按执行时间正序；某步的结果类型对应的出边，若目标节点在之后执行过则算走过；
// 某步没有命中任何出边时，若与下一步之间有连线也算走过（兼容无关系类型的日志）。
func buildFlowImageTrace(logs []flowShareLog, graphIDs map[string]bool, conns []flowImageConn) *flowImageTrace {
	list := make([]flowShareLog, 0, len(logs))
	for _, l := range logs {
		if l.NodeID != "" && l.Level != "flow_total" {
			list = append(list, l)
		}
	}
	at := func(l flowShareLog) int64 {
		if !l.CreatedAt.IsZero() {
			return l.CreatedAt.UnixMilli()
		}
		return l.Timestamp * 1000
	}
	sort.SliceStable(list, func(i, j int) bool {
		if a, b := at(list[i]), at(list[j]); a != b {
			return a < b
		}
		return list[i].ID < list[j].ID
	})

	tr := &flowImageTrace{nodes: map[string]*flowImageTraceNode{}, edges: map[string]bool{}}
	stepIDs := []string{}
	stepRel := []string{}
	for _, l := range list {
		if !graphIDs[l.NodeID] {
			continue
		}
		stepIDs = append(stepIDs, l.NodeID)
		stepRel = append(stepRel, flowConnType(l.RelationType))
		order := len(stepIDs)
		n := tr.nodes[l.NodeID]
		if n == nil {
			n = &flowImageTraceNode{}
			tr.nodes[l.NodeID] = n
		}
		n.runs = append(n.runs, flowImageRun{order: order, log: l})
		if l.Level == "error" || l.ErrorMsg != "" {
			n.error = true
		}
	}
	tr.steps = len(stepIDs)
	ranAfter := func(id string, order int) bool {
		n := tr.nodes[id]
		if n == nil {
			return false
		}
		for _, r := range n.runs {
			if r.order > order {
				return true
			}
		}
		return false
	}
	for i, id := range stepIDs {
		for _, c := range conns {
			if c.FromID == id && flowConnType(c.Type) == stepRel[i] && ranAfter(c.ToID, i+1) {
				tr.edges[c.FromID+"|"+c.ToID] = true
			}
		}
	}
	for i := 0; i+1 < len(stepIDs); i++ {
		a, b := stepIDs[i], stepIDs[i+1]
		hasOut := false
		for k := range tr.edges {
			if strings.HasPrefix(k, a+"|") {
				hasOut = true
				break
			}
		}
		if hasOut {
			continue
		}
		for _, c := range conns {
			if c.FromID == a && c.ToID == b {
				tr.edges[a+"|"+b] = true
				break
			}
		}
	}
	return tr
}

// fmtFlowMs 与 flow_view.js 的 fmtMs 一致
func fmtFlowMs(ms int64) string {
	if ms < 0 {
		return ""
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		if ms < 10000 {
			return fmt.Sprintf("%.2fs", float64(ms)/1000)
		}
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dm%ds", ms/60000, int64(math.Round(float64(ms%60000)/1000)))
}

// flowImageDurText 与 flow_view.js 的 traceDurText 一致：执行一次直接显示；多次显示前 3 次与合计
func flowImageDurText(n *flowImageTraceNode) string {
	if n == nil || len(n.runs) == 0 {
		return ""
	}
	if len(n.runs) == 1 {
		return fmtFlowMs(n.runs[0].log.DurationMs)
	}
	var total int64
	shown := []string{}
	for i, r := range n.runs {
		total += r.log.DurationMs
		if i < 3 {
			shown = append(shown, fmtFlowMs(r.log.DurationMs))
		}
	}
	s := strings.Join(shown, " / ")
	if len(n.runs) > 3 {
		s += " …"
	}
	return fmt.Sprintf("%s（%d 次，共 %s）", s, len(n.runs), fmtFlowMs(total))
}

// dotHTMLEscape 转义 Graphviz HTML 标签里的文本
func dotHTMLEscape(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// dotQuote 生成 dot 的双引号字符串
func dotQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
}

func flowAnyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// buildFlowDot 把分享数据转成 dot 源码。now 为图片上显示的生成时间。
func buildFlowDot(d *flowShareData, now time.Time) string {
	chain := d.Chain
	nodeIDs := splitFlowIDs(chain.NodeIDs)
	subIDs := splitFlowIDs(chain.SubChainIDs)
	var conns []flowImageConn
	_ = json.Unmarshal([]byte(chain.ConnectionsData), &conns)
	nameOv := map[string]any{}
	_ = json.Unmarshal([]byte(chain.NodeNameOverrides), &nameOv)
	switchOv := map[string]any{}
	_ = json.Unmarshal([]byte(chain.NodeSwitchOverrides), &switchOv)
	var dsl struct {
		Metadata struct {
			Nodes []map[string]any `json:"nodes"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal([]byte(chain.DSLJSON), &dsl)
	dslByID := map[string]map[string]any{}
	for _, n := range dsl.Metadata.Nodes {
		if id := flowAnyString(n["id"]); id != "" {
			dslByID[id] = n
		}
	}
	defByID := map[string]flowShareNode{}
	for _, n := range d.Nodes {
		defByID[n.NodeID] = n
	}

	nodeSet := map[string]bool{}
	subSet := map[string]bool{}
	graphIDs := map[string]bool{}
	allIDs := []string{}
	seen := map[string]bool{}
	addID := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			allIDs = append(allIDs, id)
		}
	}
	for _, id := range nodeIDs {
		nodeSet[id], graphIDs[id] = true, true
		addID(id)
	}
	for _, id := range subIDs {
		subSet[id], graphIDs[id] = true, true
		addID(id)
	}
	for _, c := range conns {
		addID(c.FromID)
		addID(c.ToID)
	}

	var tr *flowImageTrace
	if d.TraceID != "" {
		tr = buildFlowImageTrace(d.Logs, graphIDs, conns)
	}

	var b strings.Builder
	b.WriteString("digraph flow {\n")
	title := "<B>" + dotHTMLEscape(chain.Name) + "（" + dotHTMLEscape(chain.ChainID) + "）</B>"
	sub := []string{}
	if d.TraceID != "" {
		sub = append(sub, "trace_id: "+dotHTMLEscape(d.TraceID))
		if tr != nil {
			sub = append(sub, fmt.Sprintf("执行步数: %d", tr.steps))
		}
		if len(d.Logs) >= flowShareMaxLogs {
			sub = append(sub, fmt.Sprintf("日志达到上限 %d 条，可能不完整", flowShareMaxLogs))
		}
	}
	sub = append(sub, "生成时间: "+now.Format("2006-01-02 15:04:05 -07:00"))
	title += `<BR/><FONT POINT-SIZE="11" COLOR="#6b7280">` + strings.Join(sub, " · ") + "</FONT>"
	fmt.Fprintf(&b, "  graph [rankdir=TB, bgcolor=\"white\", pad=0.3, nodesep=0.4, ranksep=0.5, labelloc=t, fontname=%s, fontsize=16, label=<%s>];\n",
		dotQuote(flowImageFont), title)
	fmt.Fprintf(&b, "  node [shape=box, style=\"rounded,filled\", penwidth=2, margin=\"0.15,0.08\", fontname=%s, fontsize=12];\n", dotQuote(flowImageFont))
	fmt.Fprintf(&b, "  edge [fontname=%s, fontsize=11, color=\"#9ca3af\", fontcolor=\"#6b7280\", arrowsize=0.7];\n", dotQuote(flowImageFont))

	small := func(s string) string {
		return `<BR ALIGN="LEFT"/><FONT POINT-SIZE="10" COLOR="#4b5563">` + s + "</FONT>"
	}
	for _, id := range allIDs {
		var label string
		pal := flowImageKindFallback
		switch {
		case nodeSet[id]:
			baseID := id
			if base, _, ok := strings.Cut(id, "__"); ok {
				baseID = base
			}
			def, hasDef := defByID[baseID]
			dn := dslByID[id]
			typ := def.Type
			if typ == "" {
				typ = flowAnyString(dn["type"])
			}
			cfg, _ := dn["configuration"].(map[string]any)
			name := strings.TrimSpace(flowAnyString(nameOv[id]))
			if name == "" {
				name = flowAnyString(dn["name"])
			}
			if name == "" {
				name = def.Name
			}
			if name == "" {
				name = baseID
			}
			isReturn := typ == "custom/ReturnValue"
			kind := "action"
			if isReturn {
				kind = "return"
			} else if hasDef && def.Kind != "" {
				kind = def.Kind
			}
			expr := ""
			if typ == "custom/Activity" || typ == "custom/CondSwitch" {
				if v, ok := switchOv[id]; ok {
					expr = flowAnyString(v)
				} else if nc, ok := cfg["node_config"].(map[string]any); ok {
					expr = flowAnyString(nc["switch_condition"])
				}
				expr = strings.Join(strings.Fields(expr), " ")
			}
			routing := !isReturn && (def.HasSwitchCondition || expr != "")
			if routing {
				pal = flowImageRoutePalette
			} else if p, ok := flowImageKindPalette[kind]; ok {
				pal = p
			}
			label = "<B>"
			if routing {
				label += "[路由] "
			}
			label += dotHTMLEscape(name) + "</B>" + small("NodeId: "+dotHTMLEscape(baseID))
			if id != baseID {
				label += small("Id: " + dotHTMLEscape(id))
			}
			if typ == "custom/RootChainCall" {
				if t := flowAnyString(cfg["target_chain_id"]); t != "" {
					label += small("→ 调用根链: " + dotHTMLEscape(t))
				}
			}
			if expr != "" {
				label += `<BR ALIGN="LEFT"/><FONT POINT-SIZE="10" COLOR="#1d4ed8">条件: ` + dotHTMLEscape(expr) + "</FONT>"
			}
		case subSet[id]:
			pal = flowImageSubPalette
			name := flowAnyString(dslByID[id]["name"])
			if name == "" {
				label = "<B>[子链] " + dotHTMLEscape(id) + "</B>"
			} else {
				label = "<B>[子链] " + dotHTMLEscape(name) + "</B>" + small(dotHTMLEscape(id))
			}
		default:
			label = "<B>? " + dotHTMLEscape(id) + "</B>"
		}
		attrs := fmt.Sprintf("fillcolor=%s, color=%s, fontcolor=%s", dotQuote(pal.fill), dotQuote(pal.stroke), dotQuote(pal.color))
		if tr != nil {
			if n := tr.nodes[id]; n != nil {
				if dur := flowImageDurText(n); dur != "" {
					label += `<BR ALIGN="LEFT"/><FONT POINT-SIZE="11" COLOR="#dc2626"><B>耗时 ` + dotHTMLEscape(dur) + "</B></FONT>"
				}
				if n.error {
					attrs = fmt.Sprintf("fillcolor=%s, color=%s, fontcolor=%s, penwidth=4, style=\"rounded,filled,dashed\"",
						dotQuote(pal.fill), dotQuote(flowImageErrorColor), dotQuote(pal.color))
				} else {
					attrs = fmt.Sprintf("fillcolor=%s, color=%s, fontcolor=%s, penwidth=4",
						dotQuote(pal.fill), dotQuote(flowImageTraceColor), dotQuote(pal.color))
				}
			}
		}
		fmt.Fprintf(&b, "  %s [label=<%s<BR ALIGN=\"LEFT\"/>>, %s];\n", dotQuote(id), label, attrs)
	}

	// 相同 from->to 的连线合并为一条，关系类型逗号分隔（与页面一致）
	type mergedEdge struct {
		from, to string
		types    []string
	}
	merged := []*mergedEdge{}
	mergedMap := map[string]*mergedEdge{}
	for _, c := range conns {
		if c.FromID == "" || c.ToID == "" {
			continue
		}
		key := c.FromID + "|" + c.ToID
		t := flowConnType(c.Type)
		if m := mergedMap[key]; m != nil {
			dup := false
			for _, x := range m.types {
				dup = dup || x == t
			}
			if !dup {
				m.types = append(m.types, t)
			}
			continue
		}
		m := &mergedEdge{from: c.FromID, to: c.ToID, types: []string{t}}
		mergedMap[key] = m
		merged = append(merged, m)
	}
	for _, m := range merged {
		attrs := "label=" + dotQuote(" "+strings.Join(m.types, ",")+" ")
		if tr != nil && tr.edges[m.from+"|"+m.to] {
			attrs += fmt.Sprintf(", color=%s, fontcolor=%s, penwidth=3", dotQuote(flowImageTraceColor), dotQuote(flowImageTraceColor))
		}
		fmt.Fprintf(&b, "  %s -> %s [%s];\n", dotQuote(m.from), dotQuote(m.to), attrs)
	}
	b.WriteString("}\n")
	return b.String()
}

// renderFlowDot 调用 dot 命令把 dot 源码生成指定格式的图片。
func renderFlowDot(ctx context.Context, src, format string) ([]byte, error) {
	bin, err := exec.LookPath(flowImageDotBin)
	if err != nil {
		return nil, errFlowImageDotMissing
	}
	ctx, cancel := context.WithTimeout(ctx, flowImageTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-T"+format)
	cmd.Stdin = strings.NewReader(src)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dot -T%s: %w: %s", format, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// handleFlowShareImage 免登录接口：凭分享 token 生成流程执行路径图片。
// GET /api/flow-view/image?share=<token>&trace_id=<trace_id>&format=png|svg（format 默认 png）
func (ws *WebServer) handleFlowShareImage(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "png"
	}
	contentType, ok := flowImageFormats[format]
	if !ok {
		writeError(w, http.StatusBadRequest, "format 只支持 png、svg")
		return
	}
	data, status, err := ws.loadFlowShare(r)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	src := buildFlowDot(data, time.Now())

	select {
	case flowImageSem <- struct{}{}:
		defer func() { <-flowImageSem }()
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "请求已取消")
		return
	}
	start := time.Now()
	img, err := renderFlowDot(r.Context(), src, format)
	if err != nil {
		log.Error().Err(err).Str("project", data.Project).Str("chain_id", data.Chain.ChainID).Str("trace_id", data.TraceID).Msg("flow-view image render failed")
		if errors.Is(err, errFlowImageDotMissing) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "生成图片失败")
		return
	}
	log.Info().Str("project", data.Project).Str("chain_id", data.Chain.ChainID).Str("trace_id", data.TraceID).
		Str("format", format).Int("bytes", len(img)).Dur("cost", time.Since(start)).Msg("flow-view image rendered")

	name := "flow_" + data.Chain.ChainID
	if data.TraceID != "" {
		name += "_" + data.TraceID
	}
	name = flowImageFileNameRe.ReplaceAllString(name, "_") + "." + format
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	// 日志会随执行增加，图片每次按最新数据生成，不缓存
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img)
}
