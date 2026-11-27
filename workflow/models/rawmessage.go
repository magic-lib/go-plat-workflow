package models

import (
	"bytes"
	"encoding/json"
)

// safeRawMessage 将字符串安全转换为 json.RawMessage：空串或非法 JSON 一律回退为 null，
// 避免空列（如 DB 的 json/text 列存空串）导致 json.Marshal 报错
// （"unexpected end of JSON input"）致使接口返回空响应体、前端解析失败。
func safeRawMessage(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("null")
	}
	if !json.Valid([]byte(s)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

// jsonColumnValue 把要写入 MySQL json 类型列的内容规范成合法 JSON 文本：
// 空内容（如 flow_total 汇总日志不带 payload）或 null 统一写成空对象 {}，否则空串会让 MySQL 报
// Error 3140 "Invalid JSON text: The document is empty"；非法 JSON 按字符串编码后写入，保留原始内容。
func jsonColumnValue(raw []byte) string {
	const emptyJSONObject = "{}"
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return emptyJSONObject
	}
	if json.Valid(trimmed) {
		return string(raw)
	}
	b, err := json.Marshal(string(raw))
	if err != nil {
		return emptyJSONObject
	}
	return string(b)
}
