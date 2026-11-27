package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// 流程执行路径查看页（/flow-view）只读分享 token。
//
// 用途：把 /flow-view 嵌入到其它系统（跨域名 iframe 拿不到 wf_session Cookie）时免登录查看。
// 格式：base64url(payload JSON) + "." + base64url(HMAC-SHA256(项目 secret_key, payload 段))。
//   - payload 绑定 project / chain_id / 过期时间，只能查看这一条根链；
//     trace_id 可选：不绑定时为「按根链分享」，打开时用地址里的 trace_id，可查看该根链下任意一次执行；
//     绑定时为「按单次执行分享」，只能看这一条，地址里的 trace_id 不生效（兼容早期生成的链接）。
//   - 签名密钥用项目已配置的 secret_key：平台签发时用最早创建的那个，校验时任一 secret_key 验过即可。
//     对接方后端持有 secret_key 时可按同样算法自行签发；删除对应 secret_key 即让其签发的 token 全部失效。
//   - 分享接口不返回节点配置（地址、参数模板等）；该 trace 的节点执行记录会带上入参、返回值、错误与耗时，
//     供页面悬停查看，持有链接即可看到这些执行数据。
const (
	flowShareDefaultTTL = 90 * 24 * time.Hour
	flowShareMaxTTL     = 90 * 24 * time.Hour
	// flowShareMaxLogs 分享接口最多返回的节点日志条数，与页面登录态下的查询上限一致。
	flowShareMaxLogs = 1000
)

var errFlowShareInvalid = errors.New("分享链接无效")

// flowSharePayload 分享 token 的载荷。字段名缩写以缩短 URL。
type flowSharePayload struct {
	Project string `json:"p"`
	ChainID string `json:"c"`
	TraceID string `json:"t,omitempty"`
	// ExpiresAt 过期时间（Unix 秒）
	ExpiresAt int64 `json:"e"`
}

func flowShareSign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// signFlowShareToken 用 secret 签发分享 token。
func signFlowShareToken(p flowSharePayload, secret string) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal share payload: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + flowShareSign(secret, body), nil
}

// parseFlowShareToken 解析 token 载荷（不校验签名），用于先取出 project 再查该项目的密钥。
func parseFlowShareToken(token string) (flowSharePayload, string, string, error) {
	var p flowSharePayload
	body, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || body == "" || sig == "" {
		return p, "", "", errFlowShareInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return p, "", "", errFlowShareInvalid
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Project == "" || p.ChainID == "" || p.ExpiresAt <= 0 {
		return p, "", "", errFlowShareInvalid
	}
	return p, body, sig, nil
}

// verifyFlowShareToken 校验 token：签名需与 secrets 中任一密钥匹配，且未过期。
func verifyFlowShareToken(token string, secrets []string, now time.Time) (flowSharePayload, error) {
	p, body, sig, err := parseFlowShareToken(token)
	if err != nil {
		return p, err
	}
	matched := false
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(flowShareSign(s, body)), []byte(sig)) == 1 {
			matched = true
			break
		}
	}
	if !matched {
		return p, errFlowShareInvalid
	}
	if now.Unix() > p.ExpiresAt {
		return p, errors.New("分享链接已过期")
	}
	return p, nil
}

// projectSecretKeys 取项目全部 secret_key（按创建顺序）。
func (ws *WebServer) projectSecretKeys(ctx context.Context, project string) ([]string, error) {
	items, err := ws.svc.ListProjectSecrets(ctx, project)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		if it != nil && it.Key != "" {
			keys = append(keys, it.Key)
		}
	}
	return keys, nil
}

// handleCreateFlowShare 生成 /flow-view 只读分享链接（需登录，且对该项目有编辑权限，由中间件按 ?project= 校验）。
// 请求体：{ chain_id, trace_id, bind_trace, expire_hours }，expire_hours 默认与最大均为 2160（90 天）。
// 默认按根链分享：trace_id 只拼进返回的链接地址方便直接打开，不写进 token，换成该根链下其它 trace_id 同样可看；
// bind_trace=true 时把 trace_id 绑进 token，只能看这一次执行。
func (ws *WebServer) handleCreateFlowShare(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req struct {
		ChainID     string `json:"chain_id"`
		TraceID     string `json:"trace_id"`
		BindTrace   bool   `json:"bind_trace"`
		ExpireHours int    `json:"expire_hours"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	req.ChainID = strings.TrimSpace(req.ChainID)
	req.TraceID = strings.TrimSpace(req.TraceID)
	if req.ChainID == "" {
		writeError(w, http.StatusBadRequest, "chain_id is required")
		return
	}
	ttl := flowShareDefaultTTL
	if req.ExpireHours > 0 {
		ttl = time.Duration(req.ExpireHours) * time.Hour
	}
	if ttl > flowShareMaxTTL {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("有效期最长 %d 小时", int(flowShareMaxTTL.Hours())))
		return
	}
	if _, err := ws.svc.GetRootChain(r.Context(), project, req.ChainID); err != nil {
		if errors.Is(err, workflow.ErrRootChainNotFound) {
			writeError(w, http.StatusNotFound, "root chain not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secrets, err := ws.projectSecretKeys(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(secrets) == 0 {
		writeError(w, http.StatusBadRequest, "该项目还没有配置 secret_key，请先在项目管理里添加密钥后再生成分享链接")
		return
	}
	if req.BindTrace && req.TraceID == "" {
		writeError(w, http.StatusBadRequest, "bind_trace 需要同时提供 trace_id")
		return
	}
	expiresAt := time.Now().Add(ttl)
	payload := flowSharePayload{Project: project, ChainID: req.ChainID, ExpiresAt: expiresAt.Unix()}
	if req.BindTrace {
		payload.TraceID = req.TraceID
	}
	token, err := signFlowShareToken(payload, secrets[0])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := url.Values{}
	q.Set("project", project)
	q.Set("chain_id", req.ChainID)
	if req.TraceID != "" {
		q.Set("trace_id", req.TraceID)
	}
	q.Set("share", token)
	username := ""
	if u := ws.currentUserSafe(r); u != nil {
		username = u.Username
	}
	log.Info().Str("project", project).Str("chain_id", req.ChainID).Str("trace_id", req.TraceID).
		Bool("bind_trace", req.BindTrace).Str("user", username).Time("expires_at", expiresAt).Msg("flow-view share link created")
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"url":        "/flow-view?" + q.Encode(),
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// flowShareChain 分享接口返回的根链信息：只保留画图字段，dsl_json 只保留节点 id/name/type 与路由条件。
type flowShareChain struct {
	ChainID             string `json:"chain_id"`
	Name                string `json:"name"`
	NodeIDs             string `json:"node_ids"`
	SubChainIDs         string `json:"sub_chain_ids"`
	ConnectionsData     string `json:"connections_data"`
	NodeNameOverrides   string `json:"node_name_overrides"`
	NodeSwitchOverrides string `json:"node_switch_overrides"`
	DSLJSON             string `json:"dsl_json"`
}

type flowShareNode struct {
	NodeID             string `json:"node_id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	Kind               string `json:"kind,omitempty"`
	HasSwitchCondition bool   `json:"has_switch_condition,omitempty"`
}

// flowShareLog 分享接口返回的节点执行记录：用于标出执行路径，并在节点上显示耗时、悬停查看入参与返回值。
type flowShareLog struct {
	ID           uint            `json:"id"`
	NodeID       string          `json:"node_id"`
	NodeName     string          `json:"node_name,omitempty"`
	Level        string          `json:"level"`
	RelationType string          `json:"relation_type"`
	DurationMs   int64           `json:"duration_ms"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Result       string          `json:"result,omitempty"`
	ErrorMsg     string          `json:"error_msg,omitempty"`
	Timestamp    int64           `json:"timestamp"`
	CreatedAt    time.Time       `json:"created_at"`
}

// slimFlowDSL 从完整 DSL 中只抽出画图要用的节点字段，避免把节点配置（地址、参数等）暴露给分享链接。
func slimFlowDSL(dslJSON string) string {
	var dsl struct {
		Metadata struct {
			Nodes []map[string]any `json:"nodes"`
		} `json:"metadata"`
		RuleChain struct {
			Nodes []map[string]any `json:"nodes"`
		} `json:"ruleChain"`
	}
	if strings.TrimSpace(dslJSON) == "" || json.Unmarshal([]byte(dslJSON), &dsl) != nil {
		return ""
	}
	nodes := dsl.Metadata.Nodes
	if len(nodes) == 0 {
		nodes = dsl.RuleChain.Nodes
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		item := map[string]any{"id": n["id"], "name": n["name"], "type": n["type"]}
		if cfg, ok := n["configuration"].(map[string]any); ok {
			slimCfg := map[string]any{}
			if nc, ok := cfg["node_config"].(map[string]any); ok {
				if sc, ok := nc["switch_condition"]; ok {
					slimCfg["node_config"] = map[string]any{"switch_condition": sc}
				}
			}
			if t, ok := cfg["target_chain_id"]; ok {
				slimCfg["target_chain_id"] = t
			}
			if len(slimCfg) > 0 {
				item["configuration"] = slimCfg
			}
		}
		out = append(out, item)
	}
	b, err := json.Marshal(map[string]any{"metadata": map[string]any{"nodes": out}})
	if err != nil {
		return ""
	}
	return string(b)
}

// flowShareMaxTraceIDLen 地址里 trace_id 的最大长度（与 wf_node_logs.trace_id 列宽一致）。
const flowShareMaxTraceIDLen = 128

// resolveFlowShareTraceID 确定本次查看的 trace：token 绑定了 trace 时只认 token 里的；
// 未绑定（按根链分享）时取地址参数 trace_id，可为空（只看流程图）。
func resolveFlowShareTraceID(p flowSharePayload, queryTraceID string) (string, error) {
	if p.TraceID != "" {
		return p.TraceID, nil
	}
	t := strings.TrimSpace(queryTraceID)
	if len(t) > flowShareMaxTraceIDLen {
		return "", fmt.Errorf("trace_id 过长（最多 %d 个字符）", flowShareMaxTraceIDLen)
	}
	return t, nil
}

// handleGetFlowShare 免登录接口：凭分享 token 一次返回画图所需的根链结构、节点定义与该 trace 的节点执行记录。
func (ws *WebServer) handleGetFlowShare(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("share"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "缺少分享 token")
		return
	}
	p, _, _, err := parseFlowShareToken(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	secrets, err := ws.projectSecretKeys(r.Context(), p.Project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p, err = verifyFlowShareToken(token, secrets, time.Now()); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	traceID, err := resolveFlowShareTraceID(p, r.URL.Query().Get("trace_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	chain, err := ws.svc.GetRootChain(r.Context(), p.Project, p.ChainID)
	if err != nil {
		if errors.Is(err, workflow.ErrRootChainNotFound) {
			writeError(w, http.StatusNotFound, "root chain not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 只返回本链引用到的节点定义
	used := map[string]bool{}
	for _, id := range strings.Split(chain.NodeIDs, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if base, _, ok := strings.Cut(id, "__"); ok {
			id = base
		}
		used[id] = true
	}
	nodes := []flowShareNode{}
	if len(used) > 0 {
		defs, err := ws.svc.ListNodes(r.Context(), p.Project, "", "", false, false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, n := range defs {
			if n != nil && used[n.NodeID] {
				nodes = append(nodes, flowShareNode{NodeID: n.NodeID, Name: n.Name, Type: n.Type, Kind: n.Kind, HasSwitchCondition: n.HasSwitchCondition})
			}
		}
	}

	logs := []flowShareLog{}
	if traceID != "" {
		const pageSize = 200
		for offset := 0; offset < flowShareMaxLogs; offset += pageSize {
			list, total, err := ws.svc.ListNodeLogsGlobal(r.Context(), p.Project, &workflow.NodeLogFilter{
				TraceID:     traceID,
				RootChainID: p.ChainID,
				Limit:       pageSize,
				Offset:      offset,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, l := range list {
				if l == nil {
					continue
				}
				level := l.Level
				if level != "flow_total" && (l.ErrorMsg != "" || l.Error != "") {
					level = "error"
				}
				errMsg := l.ErrorMsg
				if errMsg == "" {
					errMsg = l.Error
				}
				logs = append(logs, flowShareLog{
					ID: l.ID, NodeID: l.NodeID, NodeName: l.NodeName, Level: level, RelationType: l.RelationType,
					DurationMs: l.DurationMs, Arguments: l.Arguments, Result: l.Result, ErrorMsg: errMsg,
					Timestamp: l.Timestamp, CreatedAt: l.CreatedAt,
				})
			}
			if len(list) < pageSize || int64(offset+len(list)) >= total {
				break
			}
		}
		sort.SliceStable(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"project":  p.Project,
		"trace_id": traceID,
		// trace_bound 为 true 表示链接只能看绑定的这一次执行，地址里改 trace_id 不生效
		"trace_bound": p.TraceID != "",
		"chain": flowShareChain{
			ChainID:             chain.ChainID,
			Name:                chain.Name,
			NodeIDs:             chain.NodeIDs,
			SubChainIDs:         chain.SubChainIDs,
			ConnectionsData:     chain.ConnectionsData,
			NodeNameOverrides:   chain.NodeNameOverrides,
			NodeSwitchOverrides: chain.NodeSwitchOverrides,
			DSLJSON:             slimFlowDSL(chain.DSLJSON),
		},
		"nodes":      nodes,
		"logs":       logs,
		"expires_at": time.Unix(p.ExpiresAt, 0).Format(time.RFC3339),
	})
}
