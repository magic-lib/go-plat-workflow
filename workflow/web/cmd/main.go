// Package main 工作流管理平台入口。
//
// 启动 Web 管理界面，提供 Node/SubChain/RootChain 的在线管理
// 和工作流的可视化组装与执行。
//
// 用法:
//
//	go run ./workflow/web/cmd/
//
// 命令行参数:
//
//	-dsn   MySQL 连接串（默认空，回退到环境变量/配置文件/内置默认值）
//	-addr  HTTP 监听地址（默认空，回退到环境变量/配置文件/内置默认值）
//	-f     配置文件路径（最高优先级，覆盖 CONFIG_PATH 与默认候选路径）
//
// 环境变量:
//
//	DB_DSN            MySQL 连接串，默认: root:root@tcp(127.0.0.1:3306)/workflow?charset=utf8mb4&parseTime=True&loc=Local
//	LISTEN_ADDR       监听地址，默认: :8080
//	CONFIG_PATH       配置文件路径（可选），默认候选: config/app.yaml 或 workflow/etc/app.yaml
//	CONFIG_SECRET_KEY 配置文件敏感信息解密 key（可选），为空时使用空 key
//
// 配置优先级（从高到低）:
//
//	命令行 flag (-f 指定文件 > -dsn / -addr) > 环境变量 (DB_DSN / LISTEN_ADDR / CONFIG_PATH) > 默认候选路径 > 内置默认值
//
// 配置文件通过 github.com/magic-lib/go-plat-startupcfg/startupcfg 加载，
// 格式见 config/app.yaml（mysql 段生成 DSN，custom.normal.host_port 生成监听地址）。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"github.com/magic-lib/go-plat-utils/logs"
	"github.com/magic-lib/go-plat-utils/logs/mysqllog"
	"github.com/magic-lib/go-plat-workflow/workflow/config"
	"github.com/magic-lib/go-plat-workflow/workflow/service"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"github.com/magic-lib/go-plat-workflow/workflow/web"
)

func main() {
	// 命令行参数（默认值为空，避免 flag 默认值抢占环境变量/配置文件）
	var (
		dbDSN      string
		listenAddr string
		configPath string
	)
	flag.StringVar(&dbDSN, "dsn", "", "MySQL DSN（优先级：命令行 > 环境变量 DB_DSN > 配置文件 > 默认值）")
	flag.StringVar(&listenAddr, "addr", "", "HTTP 监听地址（优先级：命令行 > 环境变量 LISTEN_ADDR > 配置文件 > 默认值）")
	flag.StringVar(&configPath, "f", "", "配置文件路径（最高优先级，覆盖 CONFIG_PATH 与默认候选路径）")
	flag.Parse()

	// 记录 -f 指定的配置文件路径，供 loadAppConfig 使用
	if configPath != "" {
		userConfigPath = configPath
		log.Info().Msg("config file:" + userConfigPath)
	}

	// 启动时打印构建时写入的 git commit id（由 Dockerfile 生成 /app/git_commit_id）
	printGitCommitID()

	// 按优先级兜底解析 dbDSN 与 listenAddr：
	// 命令行 flag 已显式传入则不再覆盖；否则依次回退到环境变量、配置文件、内置默认值。
	dbDSN = resolveDBDSN(dbDSN)
	if dbDSN == "" {
		log.Error().Msg("dbDSN is empty,failed to connect to MySQL")
		return
	}
	listenAddr = resolveListenAddr(listenAddr)

	// 连接 MySQL
	log.Info().Str("dsn", maskDSN(dbDSN)).Msg("connecting to MySQL...")
	db, err := gorm.Open(mysql.Open(dbDSN), &gorm.Config{
		Logger: gormlog.Default.LogMode(gormlog.Warn),
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to connect to MySQL")
		return
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	service.MysqlLogger, err = initMysqlLogger(sqlDB)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init mysql logger")
	}
	log.Info().Msg("MySQL connected, initializing workflow service...")

	// 创建 Web 服务（内部会自动建表；并启动按环境配置自动发现 Redis 的日志/心跳收集器）
	ws, err := web.NewWebServer(db)
	if err != nil {
		log.Error().Err(err).Msg("failed to create web server")
		return
	}

	// 优雅退出
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Info().Msg("shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 停止 workflow 引擎
		if err := ws.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("failed to shutdown workflow engine")
		}
		sqlDB.Close()
		os.Exit(0)
	}()

	log.Info().Str("addr", listenAddr).Msg("workflow web server starting")
	listenAddrList := strings.Split(listenAddr, ":")
	if len(listenAddrList) == 2 {
		log.Info().Msgf("=> Open http://localhost:%s in your browser", listenAddrList[1])
	} else {
		log.Info().Msgf("=> Open http://%s in your browser", listenAddr)
	}

	if err := ws.ListenAndServe(listenAddr); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

// defaultListenAddr 内置默认监听地址。
const defaultListenAddr = ":8080"

// appConfigOnce 缓存一次配置文件加载结果，避免多次重复读文件。
var (
	appConfigOnce   sync.Once
	appConfigCached *config.AppConfig
	// userConfigPath 由 -f 命令行参数指定的配置文件路径（最高优先级）。
	userConfigPath string
)

// loadAppConfig 加载配置文件（config/app.yaml），失败时返回 nil（不阻断启动，走默认值兜底）。
// 若指定了 -f 参数，则优先加载该路径。
func loadAppConfig() *config.AppConfig {
	appConfigOnce.Do(func() {
		var (
			cfg *config.AppConfig
			err error
		)
		if userConfigPath != "" {
			cfg, err = config.Load(userConfigPath)
		} else {
			cfg, err = config.Load()
		}
		if err == nil {
			appConfigCached = cfg
		}
	})
	return appConfigCached
}

// resolveDBDSN 按优先级解析 MySQL DSN：命令行 > 环境变量 DB_DSN > 配置文件 > 默认值。
func resolveDBDSN(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("DB_DSN"); v != "" {
		return v
	}
	if cfg := loadAppConfig(); cfg != nil && cfg.DBDsn != "" {
		return cfg.DBDsn
	}
	return ""
}

// resolveListenAddr 按优先级解析监听地址：命令行 > 环境变量 LISTEN_ADDR > 配置文件 > 默认值。
func resolveListenAddr(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		return v
	}
	if cfg := loadAppConfig(); cfg != nil && cfg.ListenAddr != "" {
		return cfg.ListenAddr
	}
	return defaultListenAddr
}

// maskDSN 隐藏密码部分用于日志输出。
func maskDSN(dsn string) string {
	if idx := strings.IndexByte(dsn, '@'); idx > 0 {
		return "***@***" + dsn[idx+4:]
	}
	return dsn
}

// gitCommitIDFile 构建时由 Dockerfile 生成的 commit id 文件路径。
const gitCommitIDFile = "/app/git_commit_id"

// printGitCommitID 读取构建时写入的 git commit id 并打印到控制台。
// 文件不存在（本地开发）时静默跳过。
func printGitCommitID() {
	data, err := os.ReadFile(gitCommitIDFile)
	if err != nil {
		return
	}
	commitID := strings.TrimSpace(string(data))
	if commitID == "" {
		return
	}
	log.Info().Str("git_commit_id", commitID).Msg("build info")
}

func initMysqlLogger(db *sql.DB) (logs.ILogger, error) {
	if db == nil {
		return nil, fmt.Errorf("mysql config is nil")
	}
	cfg := &mysqllog.Config{
		DB:          db,
		TablePrefix: "wf_chain_rule_log",
		BatchSize:   5,
		ExtendFields: []mysqllog.ExtendField{
			{Name: "code", DBType: "INT", Comment: "返回码，非0表示错误"},
			{Name: "project", DBType: "VARCHAR(100)", Comment: "项目名称"},
			{Name: "chain_id", DBType: "VARCHAR(100)", Comment: "规则链Id"},
			{Name: "chain_key", DBType: "VARCHAR(100)", Comment: "规则链key"},
			{Name: "trace_id", DBType: "VARCHAR(100)", Comment: "traceId"},
			{Name: "request", DBType: "TEXT", Comment: "请求参数"},
			{Name: "response", DBType: "TEXT", Comment: "返回参数"},
		},
	}
	logger, err := mysqllog.New(cfg)
	if err != nil {
		return nil, err
	}

	return logger, nil
}
