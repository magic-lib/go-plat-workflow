// Package config 提供程序启动配置的加载能力。
//
// 基于 github.com/magic-lib/go-plat-startupcfg/startupcfg 读取配置文件。
// 启动时若未通过命令行 flag 或环境变量（DB_DSN / LISTEN_ADDR）提供配置，
// 则从本目录下的 app.yaml 配置文件中读取，作为兜底配置。
package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/magic-lib/go-plat-startupcfg/startupcfg"
	"github.com/magic-lib/go-plat-utils/conv"
)

// 配置文件中各配置项的 key 名（与 app.yaml 保持一致）。
const (
	// mysqlConnectName 配置文件中 mysql 连接项的 key 名。
	mysqlConnectName = "mysql_connect"
	// hostPortKey 配置文件中 custom.normal 下监听地址的 key 名。
	hostPortKey = "host_port"
	// feishuAlertWebhookKey 配置文件中 custom.normal 下飞书告警机器人地址的 key 名。
	feishuAlertWebhookKey = "feishu_alert_webhook"
	// normalKey 配置文件中 custom.normal 段的 key 名。
	normalKey = "normal"
)

type ReturnValue struct {
	Name  string `json:"name,omitempty"`
	Label string `json:"label,omitempty"`
	Key   string `json:"key,omitempty"`
	Type  string `json:"type,omitempty"`
}

// candidateConfigPaths 默认配置文件候选路径（按顺序尝试，存在即用）。
// 1) 可执行文件同级的 config/app.yaml
// 2) 可执行文件同级的 workflow/etc/app.yaml
// 3) 当前工作目录下的 config/app.yaml
// 4) 当前工作目录下的 workflow/etc/app.yaml
var candidateConfigPaths = []string{
	"config/app.yaml",
	"workflow/etc/app.yaml",
}

// AppConfig 应用启动配置，从 startupcfg 解析结果中提取。
type AppConfig struct {
	// DBDsn MySQL 连接串（由 startupcfg.MysqlConfig.DatasourceName() 生成）
	DBDsn string
	// ListenAddr HTTP 监听地址（由 custom.normal.host_port.host + port 拼接）
	ListenAddr string
	// FeishuAlertWebhook 飞书告警机器人 Webhook 地址（由 custom.normal.feishu_alert_webhook 读取）
	FeishuAlertWebhook string
}

// Load 通过 startupcfg 从配置文件加载应用配置。
// 未指定 path 时，依次尝试 CONFIG_PATH 环境变量、可执行文件同级的 config/app.yaml，
// 以及当前工作目录下的 config/app.yaml。文件不存在返回错误。
func Load(path ...string) (*AppConfig, error) {
	p, err := resolvePath(path...)
	if err != nil {
		return nil, err
	}

	// 设置敏感信息解密 key（AES-ECB）。全局仅允许设置一次。
	if err := initSecretHandler(); err != nil {
		return nil, err
	}

	startCfg, err := startupcfg.NewStartupForYamlFile(p)
	if err != nil {
		return nil, err
	}

	cfg := &AppConfig{}

	// 从 mysql 配置生成 DSN：优先取 mysql_connect，否则取第一个
	if mc, ok := startCfg.Mysql[mysqlConnectName]; ok && mc != nil {
		cfg.DBDsn = mc.DatasourceName()
	} else {
		for _, mc := range startCfg.Mysql {
			if mc != nil {
				cfg.DBDsn = mc.DatasourceName()
				break
			}
		}
	}

	// 从 custom.normal.host_port 解析监听地址：host + port
	if hp, ok := startCfg.Custom[hostPortKey]; ok {
		cfg.ListenAddr = hostPortToAddr(hp)
	}

	// 从 custom.normal.feishu_alert_webhook 读取飞书告警机器人地址
	cfg.FeishuAlertWebhook = customNormalString(startCfg.Custom, feishuAlertWebhookKey)

	return cfg, nil
}

// customNormalString 从 custom.normal 段读取字符串配置项。
// startCfg.Custom 形如 {"normal":{"host_port":{...},"feishu_alert_webhook":"..."}}，
// 找不到或类型不符时返回空串。
func customNormalString(custom map[string]interface{}, key string) string {
	normal, ok := custom[normalKey]
	if !ok {
		return ""
	}
	nm, ok := normal.(map[string]interface{})
	if !ok {
		return ""
	}
	if v, ok := nm[key].(string); ok {
		return v
	}
	return ""
}

// secretHandlerOnce 保证敏感信息解密 handler 全局仅初始化一次
// （startupcfg.SetDefaultEncryptedHandler 重复调用会返回 "decryptFunc has set" 错误）。
var secretHandlerOnce sync.Once

// initSecretHandler 初始化敏感信息解密 key。
// key 取自环境变量 CONFIG_SECRET_KEY，为空时使用空 key（密文无法解密时
// MysqlConfig.Password() 会回退返回原始串）。
func initSecretHandler() error {
	var initErr error
	secretHandlerOnce.Do(func() {
		key := os.Getenv("CONFIG_SECRET_KEY")
		if err := startupcfg.SetDefaultEncryptedHandler(key); err != nil {
			initErr = err
		}
	})
	return initErr
}

// hostPortToAddr 将 custom.normal.host_port（host + port）拼接为 host:port 地址。
// hp 为 yaml 反序列化后的 map[string]interface{}，形如 {"host":"0.0.0.0","port":8080}。
func hostPortToAddr(hp interface{}) string {
	m, ok := hp.(map[string]interface{})
	if !ok {
		return ""
	}
	host := conv.String(m["host"])
	port := conv.String(m["port"])
	if host == "" && port == "" {
		return ""
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return host + ":" + port
}

// resolvePath 解析配置文件的真实路径。
// 优先级：显式 path > CONFIG_PATH 环境变量 > 多个候选默认路径（存在即用）。
func resolvePath(path ...string) (string, error) {
	if len(path) > 0 && path[0] != "" {
		return path[0], nil
	}
	// 1. 优先使用 CONFIG_PATH 环境变量
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p, nil
	}
	// 2. 依次尝试候选路径：可执行文件同级 与 当前工作目录下各候选目录
	var bases []string
	if exe, err := os.Executable(); err == nil {
		bases = append(bases, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		bases = append(bases, wd)
	}
	for _, base := range bases {
		for _, rel := range candidateConfigPaths {
			p := filepath.Join(base, rel)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	// 3. 都不存在时返回首个候选（相对当前工作目录），由调用方报错
	return candidateConfigPaths[0], nil
}
