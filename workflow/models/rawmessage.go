package models

import "encoding/json"

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
