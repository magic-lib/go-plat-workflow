package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	// collectorHeartbeatPrefix 心跳 hash key 前缀（与 mq_worker.go 保持一致的命名）
	collectorHeartbeatPrefix = rulegox.HeartbeatKeyPrefix
	// collectorLogPrefix 执行日志 list key 前缀
	collectorLogPrefix = rulegox.ActivityLogKeyPrefix
	// collectorNodeLogPrefix node 运行日志 list key 前缀
	collectorNodeLogPrefix = rulegox.NodeLogKeyPrefix

	// heartbeatWindow 心跳统计窗口：最近 1 分钟
	heartbeatWindow = 60 * time.Second
	// heartbeatCacheTTL 心跳缓存保留时长：仅存 2 分钟以内
	heartbeatCacheTTL = 2 * time.Minute
	// heartbeatExpectedPerWindow 一个 actName 在 1 分钟窗口内期望的心跳次数（每 10s 一次 → 6 次）
	heartbeatExpectedPerWindow = 6
	// heartbeatScanInterval 周期扫描单个 redis 心跳 hash 的间隔
	heartbeatScanInterval = 5 * time.Second
	// reconcileInterval 重新发现环境 Redis 配置的间隔（增删监听任务）
	reconcileInterval = 30 * time.Second
	// logBatchSize 单次从 redis list 拉取的日志条数
	logBatchSize = 100
)

// EnvConfigLister 由外部（service 层）实现，供收集器拉取所有环境配置以自动发现 Redis。
type EnvConfigLister interface {
	// ListAllEnvConfigs 返回系统中所有项目下的全部环境配置。
	ListAllEnvConfigs(ctx context.Context) ([]*EnvConfigDef, error)
}

// redisTask 单个环境 Redis 的监听任务：持有独立的 redis 客户端与扫描协程。
type redisTask struct {
	project string
	env     string
	// redisCfgKey 用于快速判断配置是否发生变化（地址+库+用户名，不含密码）
	redisCfgKey string
	redisCli    *redis.Client
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// ActivityCollector 管理端收集器（多 Redis 监听管理器）：
//  1. 定时扫描系统中所有项目×环境的 EnvConfig，自动为每个配置了 Redis 的环境
//     建立监听任务，消费 worker 上报到 redis 的执行日志并落库；
//  2. 周期读取 worker 上报到 redis 的心跳 hash，维护最近 2 分钟的内存缓存，
//     供 Activity 列表进度条查询存活比例；
//  3. 当某个环境的 Redis 配置被移除（或环境被删除）时，对应监听任务被关闭，
//     避免对无效 redis 进行无用扫描。
type ActivityCollector struct {
	logRepo     ActivityLogStore
	nodeLogRepo NodeLogStore
	lister      EnvConfigLister

	// 心跳缓存：key = project|actName，value = 最近 2 分钟内的心跳时间戳（秒）切片
	hbMu    sync.RWMutex
	hbCache map[string][]int64

	// 各环境 Redis 监听任务：key = project|env
	taskMu sync.Mutex
	tasks  map[string]*redisTask

	stopCh chan struct{}
}

// NewActivityCollector 创建活动日志/心跳收集器。
// lister 用于发现各环境的 Redis 配置；logRepo 用于活动日志落库；nodeLogRepo 用于 node 运行日志落库。
func NewActivityCollector(logRepo ActivityLogStore, nodeLogRepo NodeLogStore, lister EnvConfigLister) *ActivityCollector {
	return &ActivityCollector{
		logRepo:     logRepo,
		nodeLogRepo: nodeLogRepo,
		lister:      lister,
		hbCache:     make(map[string][]int64),
		tasks:       make(map[string]*redisTask),
		stopCh:      make(chan struct{}),
	}
}

// Start 启动后台协调协程：周期性发现/回收各环境的 Redis 监听任务。
func (c *ActivityCollector) Start() {
	if c.lister == nil {
		log.Warn().Msg("activity collector: no env config lister, skip start")
		return
	}
	go c.reconcileLoop()
	log.Info().Msg("activity collector started")
}

// Stop 停止所有监听任务与协调协程。
func (c *ActivityCollector) Stop() {
	select {
	case <-c.stopCh:
		return
	default:
	}
	close(c.stopCh)
	c.taskMu.Lock()
	for key, t := range c.tasks {
		c.closeTaskLocked(key, t)
	}
	c.taskMu.Unlock()
}

// reconcileLoop 周期性重新发现环境 Redis 配置，增减监听任务。
func (c *ActivityCollector) reconcileLoop() {
	c.reconcile()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.reconcile()
		}
	}
}

// reconcile 根据当前所有环境配置，确保监听任务与配置一致：
// 新增缺失的、关闭已不存在（或 Redis 配置被移除）的。
func (c *ActivityCollector) reconcile() {
	if c.lister == nil {
		return
	}
	envs, err := c.lister.ListAllEnvConfigs(context.Background())
	if err != nil {
		log.Warn().Err(err).Msg("activity collector: list env configs failed")
		return
	}

	// 期望的任务集合：key = project|env
	want := make(map[string]*EnvConfigDef, len(envs))
	for _, e := range envs {
		if e == nil || e.RedisConfig == nil || e.RedisConfig.Addr == "" {
			continue
		}
		want[taskKey(e.Project, e.EnvName)] = e
	}

	c.taskMu.Lock()
	// 关闭不再需要的任务
	for key, t := range c.tasks {
		if _, ok := want[key]; !ok {
			c.closeTaskLocked(key, t)
		}
	}
	// 新增/更新任务
	for key, e := range want {
		cur, ok := c.tasks[key]
		cfgKey := redisConfigKey(e.RedisConfig)
		if ok {
			if cur.redisCfgKey == cfgKey {
				continue // 配置未变，复用
			}
			// 配置变化：先关闭旧任务，再重建
			c.closeTaskLocked(key, cur)
		}
		if cli, cerr := NewActivityCollectorRedisClient(e.RedisConfig); cerr != nil {
			log.Warn().Err(cerr).Str("project", e.Project).Str("env", e.EnvName).
				Msg("activity collector: connect redis failed, skip env")
			continue
		} else {
			c.startTaskLocked(key, e, cli, cfgKey)
		}
	}
	c.taskMu.Unlock()
}

// startTaskLocked 在持有 taskMu 的情况下为指定环境启动监听任务。
func (c *ActivityCollector) startTaskLocked(key string, e *EnvConfigDef, cli *redis.Client, cfgKey string) {
	t := &redisTask{
		project:     e.Project,
		env:         e.EnvName,
		redisCfgKey: cfgKey,
		redisCli:    cli,
		stopCh:      make(chan struct{}),
	}
	c.tasks[key] = t

	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		c.collectLogsLoop(t)
	}()
	go func() {
		defer t.wg.Done()
		c.heartbeatScanLoop(t)
	}()
	log.Info().Str("project", e.Project).Str("env", e.EnvName).Msg("activity collector: redis task started")
}

// closeTaskLocked 在持有 taskMu 的情况下关闭指定监听任务。
func (c *ActivityCollector) closeTaskLocked(key string, t *redisTask) {
	delete(c.tasks, key)
	close(t.stopCh)
	t.wg.Wait()
	if t.redisCli != nil {
		_ = t.redisCli.Close()
	}
	log.Info().Str("project", t.project).Str("env", t.env).Msg("activity collector: redis task stopped")
}

// collectLogsLoop 持续从当前 redis 所有 workflow:activity:log:* list 消费日志并落库。
func (c *ActivityCollector) collectLogsLoop(t *redisTask) {
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}
		keys, err := c.scanKeys(t.redisCli, collectorLogPrefix+"*")
		if err != nil {
			log.Warn().Err(err).Str("project", t.project).Str("env", t.env).
				Msg("activity collector: scan log keys failed")
			time.Sleep(time.Second)
			continue
		}
		for _, key := range keys {
			c.drainLogKey(t.redisCli, key)
		}
		// 同时消费 node 运行日志（workflow:node:log:*）
		if c.nodeLogRepo != nil {
			nodeKeys, err := c.scanKeys(t.redisCli, collectorNodeLogPrefix+"*")
			if err != nil {
				log.Warn().Err(err).Str("project", t.project).Str("env", t.env).
					Msg("activity collector: scan node log keys failed")
			} else {
				for _, key := range nodeKeys {
					c.drainNodeLogKey(t.redisCli, key)
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// drainLogKey 从单个日志 list 中拉取并落库日志（非阻塞，最多 logBatchSize 条）。
func (c *ActivityCollector) drainLogKey(cli *redis.Client, key string) {
	for i := 0; i < logBatchSize; i++ {
		res, err := cli.LPop(context.Background(), key).Result()
		if err == redis.Nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("key", key).Msg("activity collector: lpop log failed")
			return
		}
		var rec ActivityLogDef
		if err := json.Unmarshal([]byte(res), &rec); err != nil {
			log.Warn().Err(err).Msg("activity collector: unmarshal log failed, skip")
			continue
		}
		// 兼容 worker 上报的 "error" 字段名（Def.ToDef 使用 error_msg）
		if rec.ErrorMsg == "" {
			rec.ErrorMsg = rec.Error
		}
		if rec.Timestamp == 0 {
			rec.Timestamp = time.Now().Unix()
		}
		if err := c.logRepo.Create(context.Background(), &rec); err != nil {
			log.Warn().Err(err).Msg("activity collector: save log failed")
		}
	}
}

// drainNodeLogKey 从单个 node 运行日志 list 中拉取并落库（非阻塞，最多 logBatchSize 条）。
func (c *ActivityCollector) drainNodeLogKey(cli *redis.Client, key string) {
	for i := 0; i < logBatchSize; i++ {
		res, err := cli.LPop(context.Background(), key).Result()
		if err == redis.Nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("key", key).Msg("activity collector: lpop node log failed")
			return
		}
		var rec NodeLogDef
		if err := json.Unmarshal([]byte(res), &rec); err != nil {
			log.Warn().Err(err).Msg("activity collector: unmarshal node log failed, skip")
			continue
		}
		// 兼容组件上报时使用的 "error" 字段名
		if rec.ErrorMsg == "" {
			rec.ErrorMsg = rec.Error
		}
		if rec.Timestamp == 0 {
			rec.Timestamp = time.Now().Unix()
		}
		if err := c.nodeLogRepo.Create(context.Background(), &rec); err != nil {
			log.Warn().Err(err).Msg("activity collector: save node log failed")
		}
	}
}

// heartbeatScanLoop 周期扫描当前 redis 心跳 hash 并写入全局缓存。
func (c *ActivityCollector) heartbeatScanLoop(t *redisTask) {
	ticker := time.NewTicker(heartbeatScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			c.scanHeartbeats(t)
		}
	}
}

// scanHeartbeats 扫描当前 redis 心跳 hash，读取每个 actName 最近心跳时间戳，更新全局缓存。
func (c *ActivityCollector) scanHeartbeats(t *redisTask) {
	keys, err := c.scanKeys(t.redisCli, collectorHeartbeatPrefix+"*")
	if err != nil {
		log.Warn().Err(err).Str("project", t.project).Str("env", t.env).
			Msg("activity collector: scan heartbeat keys failed")
		return
	}
	now := time.Now().Unix()
	fresh := make(map[string][]int64)
	for _, key := range keys {
		// 从 namespace 解析 project/env：key = workflow:heartbeat:workflow/<project>/<env>
		project, _ := parseNamespace(key[len(collectorHeartbeatPrefix):])
		fields, err := t.redisCli.HGetAll(context.Background(), key).Result()
		if err != nil {
			continue
		}
		for field, tsStr := range fields {
			// field = actNamespace|actName（见 mq_activity.go getActivityKey），拆开以区分 namespace
			actNamespace, actName := splitActivityField(field)
			if actName == "" {
				continue
			}
			ts, e := strconv.ParseInt(tsStr, 10, 64)
			if e != nil {
				continue
			}
			if now-ts > int64(heartbeatCacheTTL.Seconds()) {
				continue
			}
			ck := cacheKey(project, t.env, actNamespace, actName)
			fresh[ck] = append(fresh[ck], ts)
		}
	}
	if len(fresh) == 0 {
		return
	}
	// 合并到全局缓存（加锁），与旧缓存合并后再裁剪到 2 分钟窗口
	c.hbMu.Lock()
	for ck, tsList := range fresh {
		merged := append(c.hbCache[ck], tsList...)
		c.hbCache[ck] = trimToWindow(merged, now)
	}
	c.hbMu.Unlock()
}

// HeartbeatRatio 返回指定环境（env）与 actNamespace 下 activity 最近 1 分钟的心跳存活比例与心跳次数。
// 比例 = min(实际心跳次数, 期望次数) / 期望次数，范围 [0,1]。
// env 为空时回退为跨环境聚合（使用旧全局缓存键 project|actName）。
func (c *ActivityCollector) HeartbeatRatio(project, env, actNamespace, actName string) (float64, int) {
	ck := cacheKey(project, env, actNamespace, actName)
	if env == "" {
		ck = project + "|" + actName // 兼容未选环境时的全局聚合
	}
	c.hbMu.RLock()
	tsList := c.hbCache[ck]
	c.hbMu.RUnlock()

	now := time.Now().Unix()
	windowStart := now - int64(heartbeatWindow.Seconds())
	count := 0
	for _, ts := range tsList {
		if ts >= windowStart {
			count++
		}
	}
	ratio := float64(count) / float64(heartbeatExpectedPerWindow)
	if ratio > 1 {
		ratio = 1
	}
	return ratio, count
}

// scanKeys 使用 SCAN 迭代匹配给定模式的 key（避免 KEYS 阻塞）。
func (c *ActivityCollector) scanKeys(cli *redis.Client, pattern string) ([]string, error) {
	var out []string
	iter := cli.Scan(context.Background(), 0, pattern, 0).Iterator()
	for iter.Next(context.Background()) {
		out = append(out, iter.Val())
	}
	return out, iter.Err()
}

// ============================================================
// 辅助函数
// ============================================================

func taskKey(project, env string) string {
	return project + "|" + env
}

func cacheKey(project, env, actNamespace, actName string) string {
	return project + "|" + env + "|" + actNamespace + "|" + actName
}

// splitActivityField 拆分 worker 上报的 field（actNamespace|actName），返回 namespace 与 actName。
func splitActivityField(field string) (actNamespace, actName string) {
	parts := strings.SplitN(field, "|", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", field
}

// redisConfigKey 生成用于判断配置是否变化的指纹（不含密码）。
func redisConfigKey(cfg *RedisConfig) string {
	if cfg == nil {
		return ""
	}
	return fmt.Sprintf("%s|%d|%s", cfg.Addr, cfg.DB, cfg.Username)
}

// trimToWindow 保留窗口内的时间戳，并去重（窗口外丢弃）。
func trimToWindow(tsList []int64, now int64) []int64 {
	minTs := now - int64(heartbeatCacheTTL.Seconds())
	out := make([]int64, 0, len(tsList))
	seen := make(map[int64]struct{})
	for _, ts := range tsList {
		if ts < minTs {
			continue
		}
		if _, ok := seen[ts]; ok {
			continue
		}
		seen[ts] = struct{}{}
		out = append(out, ts)
	}
	return out
}

// parseNamespace 从 "workflow/<project>/<env>" 中解析 project 与 env。
func parseNamespace(ns string) (project, env string) {
	parts := strings.SplitN(ns, "/", 3)
	if len(parts) >= 2 {
		return parts[1], parts[2]
	}
	return "", ""
}

// NewActivityCollectorRedisClient 基于 RedisConfig 构建 redis 客户端（带连接探测）。
func NewActivityCollectorRedisClient(cfg *RedisConfig) (*redis.Client, error) {
	if cfg == nil || cfg.Addr == "" {
		return nil, fmt.Errorf("redis config error")
	}
	opt := &redis.Options{
		Addr:     cfg.Addr,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	cli := redis.NewClient(opt)
	if err := cli.Ping(context.Background()).Err(); err != nil {
		_ = cli.Close()
		return nil, err
	}
	return cli, nil
}
