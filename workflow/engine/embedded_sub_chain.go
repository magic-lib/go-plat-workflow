package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rulego/rulego/api/types"
)

// 子链内容嵌入：与节点一样，保存根链（或子链）时把引用子链的 DSL 嵌进本链 DSL 的
// ruleChain.additionalInfo.embedded_sub_chains，发布快照只要带上 dsl_json 就带上了子链内容。
// 之后修改子链不影响已保存/已发布的链，需重新编辑保存（并发布）根链才会用上最新子链。
//
// 嵌入的子链以「子链ID#内容哈希」为 ID（如 F000001#3fa2b19c0d），flow 节点 targetId 指向 project:该 ID，
// ruleChainId 仍为 project:子链ID（发布检查、前端解析按子链 ID 使用）。
// 内容不同的同一子链 ID 不同，在引擎池里互不覆盖；内容相同的则共用一个实例。

// EmbeddedSubChainsKey 嵌入子链在 ruleChain.additionalInfo 中的键。
const EmbeddedSubChainsKey = "embedded_sub_chains"

// embeddedIDSep 嵌入子链 ID 中子链 ID 与内容哈希的分隔符。
const embeddedIDSep = "#"

// EmbedSubChain 把子链 DSL 嵌入 chain，返回嵌入后的子链 ID（子链ID#内容哈希）。
// 嵌入内容的 ruleChain.id 改为该 ID、root=false，其余（含它自己嵌入的下级子链）原样保留。
func EmbedSubChain(chain *types.RuleChain, subChainID, subDSL string) (string, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(subDSL), &doc); err != nil {
		return "", fmt.Errorf("parse sub chain %s dsl: %w", subChainID, err)
	}
	rc, _ := doc["ruleChain"].(map[string]interface{})
	if rc == nil {
		rc = map[string]interface{}{}
		doc["ruleChain"] = rc
	}
	rc["id"] = subChainID
	rc["root"] = false
	// map 序列化按键排序，内容相同则哈希相同
	canonical, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal sub chain %s dsl: %w", subChainID, err)
	}
	sum := sha256.Sum256(canonical)
	embeddedID := subChainID + embeddedIDSep + hex.EncodeToString(sum[:])[:10]
	rc["id"] = embeddedID

	if chain.RuleChain.AdditionalInfo == nil {
		chain.RuleChain.AdditionalInfo = make(map[string]interface{})
	}
	store, _ := chain.RuleChain.AdditionalInfo[EmbeddedSubChainsKey].(map[string]interface{})
	if store == nil {
		store = make(map[string]interface{})
		chain.RuleChain.AdditionalInfo[EmbeddedSubChainsKey] = store
	}
	store[embeddedID] = doc
	return embeddedID, nil
}

// EmbeddedSubChainBaseID 嵌入子链 ID（F000001#hash）还原为子链 ID；非嵌入 ID 原样返回。
func EmbeddedSubChainBaseID(id string) string {
	if i := strings.Index(id, embeddedIDSep); i >= 0 {
		return id[:i]
	}
	return id
}

// ExtractEmbeddedSubChains 取出 DSL 里嵌入的全部子链（含嵌入子链里再嵌入的下级子链），key 为嵌入子链 ID，value 为子链 DSL。
// DSL 无法解析时返回 ok=false。
func ExtractEmbeddedSubChains(dsl []byte) (map[string][]byte, bool) {
	out := make(map[string][]byte)
	if !collectEmbedded(dsl, out, 0) {
		return nil, false
	}
	return out, true
}

func collectEmbedded(dsl []byte, out map[string][]byte, depth int) bool {
	const maxDepth = 10
	var doc struct {
		RuleChain struct {
			AdditionalInfo map[string]json.RawMessage `json:"additionalInfo"`
		} `json:"ruleChain"`
	}
	if err := json.Unmarshal(dsl, &doc); err != nil {
		return false
	}
	raw, ok := doc.RuleChain.AdditionalInfo[EmbeddedSubChainsKey]
	if !ok || depth >= maxDepth {
		return true
	}
	var store map[string]json.RawMessage
	if err := json.Unmarshal(raw, &store); err != nil {
		return true
	}
	for id, sub := range store {
		if _, seen := out[id]; seen {
			continue
		}
		out[id] = sub
		collectEmbedded(sub, out, depth+1)
	}
	return true
}

// FlowTarget flow 节点引用的子链：SubChainID 为子链 ID，TargetID 为 targetId 去掉 project: 前缀后的值
// （嵌入子链 ID，或旧数据的子链 ID）。
type FlowTarget struct {
	SubChainID string
	TargetID   string
}

// FlowTargets 返回节点列表里 flow 节点引用的子链（按出现顺序，按 TargetID 去重）。
func FlowTargets(nodes []*types.RuleNode) []FlowTarget {
	var out []FlowTarget
	seen := make(map[string]bool)
	for _, n := range nodes {
		if n == nil || n.Type != flowNodeType {
			continue
		}
		ref, _ := n.Configuration["ruleChainId"].(string)
		target, _ := n.Configuration["targetId"].(string)
		if target == "" {
			target = ref
		}
		ft := FlowTarget{SubChainID: stripProject(ref), TargetID: stripProject(target)}
		if ft.SubChainID == "" {
			ft.SubChainID = EmbeddedSubChainBaseID(ft.TargetID)
		}
		if ft.TargetID == "" || seen[ft.TargetID] {
			continue
		}
		seen[ft.TargetID] = true
		out = append(out, ft)
	}
	return out
}

func stripProject(ref string) string {
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
