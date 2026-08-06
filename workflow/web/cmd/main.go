// Package main 工作流管理平台入口。
//
// 启动 Web 管理界面，提供 Node/SubChain/RootChain 的在线管理
// 和工作流的可视化组装与执行。
//
// 用法:
//
//	go run ./workflow/web/cmd/
//
// 环境变量:
//
//	DB_DSN      MySQL 连接串，默认: root:root@tcp(127.0.0.1:3306)/workflow?charset=utf8mb4&parseTime=True&loc=Local
//	LISTEN_ADDR 监听地址，默认: :8080
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"github.com/magic-lib/go-plat-workflow/workflow/web"
)

func main() {
	// 命令行参数
	var (
		dbDSN      string
		listenAddr string
	)
	flag.StringVar(&dbDSN, "dsn", getEnv("DB_DSN", "root:root@tcp(127.0.0.1:3306)/workflow?charset=utf8mb4&parseTime=True&loc=Local"), "MySQL DSN")
	flag.StringVar(&listenAddr, "addr", getEnv("LISTEN_ADDR", ":8080"), "HTTP 监听地址")
	flag.Parse()

	// 连接 MySQL
	log.Info().Str("dsn", maskDSN(dbDSN)).Msg("connecting to MySQL...")
	db, err := gorm.Open(mysql.Open(dbDSN), &gorm.Config{
		Logger: gormlog.Default.LogMode(gormlog.Warn),
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to MySQL")
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Info().Msg("MySQL connected, initializing workflow service...")

	// 创建 Web 服务（内部会自动建表；并启动按环境配置自动发现 Redis 的日志/心跳收集器）
	ws, err := web.NewWebServer(db)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create web server")
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
	log.Info().Msgf("=> Open http://localhost%s in your browser", listenAddr)

	if err := ws.ListenAndServe(listenAddr); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

// getEnv 获取环境变量，不存在则返回默认值。
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt 读取整数环境变量，不存在或解析失败返回默认值。
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// hostOf 从 host:port 中取出 host。
func hostOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// portOf 从 host:port 中取出 port。
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return "6379"
}

// itoa 整数转字符串。
func itoa(n int) string {
	return strconv.Itoa(n)
}

// maskDSN 隐藏密码部分用于日志输出。
func maskDSN(dsn string) string {
	if idx := strings.IndexByte(dsn, '@'); idx > 0 {
		return "***@***" + dsn[idx+4:]
	}
	return dsn
}
