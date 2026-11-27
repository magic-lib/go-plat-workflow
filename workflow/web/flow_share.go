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
//   - payload 绑定 project / chain_id / trace_id / 过期时间，只能查看这一条根链在这个 trace 下的执行路径；
//   - 签名密钥用项目已配置的 secret_key：平台签发时用最早创建的那个，校验时任一 secret_key 验过即可。
//     对接方后端持有 secret_key 时可按同样算法自行签发；删除对应 secret_key 即让其签发的 token 全部失效。
//   - 分享接口只返回画图所需的字段，不返回节点配置、执行入参与返回值等业务数据。
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
// 请求体：{ chain_id, trace_id, expire_hours }，expire_hours 默认与最大均为 2160（90 天）。
func (ws *WebServer) handleCreateFlowShare(w http.ResponseWriter, r *http.Request) {
	project := projectParam(r)
	if project == "" {
		writeError(w, http.StatusBadRequest, "project query parameter is required")
		return
	}
	var req struct {
		ChainID     string `json:"chain_id"`
		TraceID     string `json:"trace_id"`
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
	expiresAt := time.Now().Add(ttl)
	token, err := signFlowShareToken(flowSharePayload{
		Project:   project,
		ChainID:   req.ChainID,
		TraceID:   req.TraceID,
		ExpiresAt: expiresAt.Unix(),
	}, secrets[0])
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
		Str("user", username).Time("expires_at", expiresAt).Msg("flow-view share link created")
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

type flowShareLog struct {
	ID           uint      `json:"id"`
	NodeID       string    `json:"node_id"`
	Level        string    `json:"level"`
	RelationType string    `json:"relation_type"`
	Timestamp    int64     `json:"timestamp"`
	CreatedAt    time.Time `json:"created_at"`
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
	if p.TraceID != "" {
		const pageSize = 200
		for offset := 0; offset < flowShareMaxLogs; offset += pageSize {
			list, total, err := ws.svc.ListNodeLogsGlobal(r.Context(), p.Project, &workflow.NodeLogFilter{
				TraceID:     p.TraceID,
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
				logs = append(logs, flowShareLog{ID: l.ID, NodeID: l.NodeID, Level: level, RelationType: l.RelationType, Timestamp: l.Timestamp, CreatedAt: l.CreatedAt})
			}
			if len(list) < pageSize || int64(offset+len(list)) >= total {
				break
			}
		}
		sort.SliceStable(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"project":  p.Project,
		"trace_id": p.TraceID,
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
