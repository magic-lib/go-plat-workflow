package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-workflow/workflow/config"
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
	reconcileInterval = config.DefaultTimeout
	// logBatchSize 单次从 redis list 拉取的日志条数
	logBatchSize = 100

	// offlineCheckInterval 离线巡检间隔：检查已发布 activity 是否有心跳
	offlineCheckInterval = 30 * time.Second
	// monitorRefreshInterval 刷新「已发布 activity 监控集合」的间隔
	monitorRefreshInterval = 5 * time.Minute
	// offlineGiveUp 持续离线超过该时长后停止重复提醒（视为长期下线，避免永久刷屏）
	offlineGiveUp = 24 * time.Hour
	// offlineStartupGrace 监控项的「纳入宽限期」：
	// 已发布上线但【从未收到心跳】的 activity 也要告警（服务重启后原本就离线的不能漏报），
	// 但刚启动 / 刚开启告警时心跳还没采集上来，需给一段时间的宽限，避免一次性误报全部。
	// worker 每 10s 上报一次、扫描每 5s 一次，2 分钟足够容纳启动与采集延迟。
	offlineStartupGrace = 2 * time.Minute
	// offlineAlertBatchMax 单条汇总告警最多列出的 activity 条目数
	offlineAlertBatchMax = 20
)

// 环境专属 webhook → 发送器缓存（同一个群只构造一次，避免每次告警重复创建）。
var (
	feishuSenderMu    sync.RWMutex
	feishuSenderCache = make(map[string]AlertSender)
)

// EnvConfigLister 由外部（service 层）实现，供收集器拉取所有环境配置以自动发现 Redis。
type EnvConfigLister interface {
	// ListAllEnvConfigs 返回系统中所有项目下的全部环境配置。
	ListAllEnvConfigs(ctx context.Context) ([]*EnvConfigDef, error)
}

// PublishedActivityLister 由外部（service 层）实现，供收集器获取「已发布上线」的 activity 集合。
// 离线告警只针对这些 activity：未加入发布（仅草稿 / 未被任何已发布根链引用）的
// 即使完全没有心跳也不告警，避免把「还没部署」误报成「掉线」。
type PublishedActivityLister interface {
	// ListPublishedActivities 返回指定项目下已发布到根链（当前生效版本）的 activity 标识。
	ListPublishedActivities(ctx context.Context, project string) ([]*PublishedActivityRef, error)
}

// AlertEnvLister 可选接口：直接列出【已开启告警】的环境配置。
// 由 service 层实现（走 SQL 过滤 alert_config 非空）；不实现时收集器回退为
// 全量环境列表后在内存筛选，行为一致。
type AlertEnvLister interface {
	// ListAlertEnvs 返回所有开启了告警的环境配置。
	ListAlertEnvs(ctx context.Context) ([]*EnvConfigDef, error)
}

// offlineDiag 离线告警诊断快照，用于回答「为什么没有告警」（hbLast 为空 / 监控集合为空等）。
type offlineDiag struct {
	// alertEnvs 开启告警的环境（project/env）；为空说明没有任何环境开启告警
	alertEnvs []string
	// noRedisEnv 开启告警但【没有可用 Redis 监听任务】的环境：
	// 没有监听任务就采集不到心跳 → hbLast 永远为空 → 该环境永远不会告警
	noRedisEnv []string
	// pubErr 最近一次查询「已发布 activity」的错误（非空说明监控集合可能构建失败）
	pubErr string
	// monCount 监控集合大小（已发布 activity × 开启告警的环境）
	monCount int
	// hbCount 已收到过心跳的 activity 实例数（hbLast 大小）
	hbCount int
	// matched 最近一次巡检中「监控集合 ∩ 已收到心跳」的数量；
	// 为 0 说明监控的 activity 一个心跳都没收到（未部署 / Redis 不通 / project-env 名对不上）
	matched int

	// ---- 心跳扫描统计（累加值），定位「hbLast 为什么为空」 ----
	// scanRounds 扫描轮次（为 0 = 没有任何 Redis 监听任务，即环境没配 Redis / 连不上）
	scanRounds int
	// scanKeys SCAN 匹配到的 workflow:heartbeat:* key 累计数（为 0 = Redis 里根本没有心跳 key）
	scanKeys int
	// scanFields 读取到的 hash field 累计数（每个 field = 一个上报中的 activity）
	scanFields int
	// scanFresh 通过 2 分钟 TTL 校验、实际写入缓存的条目累计数
	// （fields > 0 但 fresh = 0 → 心跳时间戳已过期，worker 早就停了）
	scanFresh int
	// scanErr 最近一次扫描失败原因
	scanErr string
	// scanAt 最近一次扫描时间（Unix 秒）
	scanAt int64
	// offlineCount 当前处于离线状态的实例数
	offlineCount int
	// updatedAt 最近一次更新时间
	updatedAt time.Time
}

// activityOfflineState 单个 activity 实例（project+env+ns+name）的离线告警状态。
type activityOfflineState struct {
	// offlineAt 首次判定离线的时间（用于「持续离线」时长与放弃提醒的判断）
	offlineAt time.Time
	// neverOnline 是否属于「从未收到过心跳」（区别于「曾在线后掉线」）
	neverOnline bool
	// lastAlertAt 上次发送告警的时间（用于重复提醒间隔）
	lastAlertAt time.Time
	// lastSeenAt 最近一次收到心跳的时间
	lastSeenAt time.Time
}

// envAlertSetting 单个环境生效的告警设置快照（巡检时按此判定与发送）。
// 由环境的 EnvAlertConfig 与全局配置合并而来，避免巡检时反复解析配置。
type envAlertSetting struct {
	// webhook 该环境专属机器人地址；为空表示用全局发送器（全局 webhook）
	webhook string
	// channel 告警通道（目前仅 feishu）
	channel string
	// threshold 心跳中断超过该时长判定离线
	threshold time.Duration
	// remind 持续离线的重复提醒间隔（0 = 只提醒一次）
	remind time.Duration
	// hasRedis 该环境是否有可用的 Redis 监听任务。
	// false 表示管理端根本采集不到心跳（环境没配 Redis / 连不上），
	// 此时「没有心跳」不能作为离线依据，否则会把该环境所有已发布 activity 全误报一遍。
	hasRedis bool
}

// activityOfflineItem 一条待告警 / 待恢复通知的离线项。
type activityOfflineItem struct {
	project      string
	env          string
	actNamespace string
	actName      string
	// gap 距最近一次心跳的时长（恢复通知中表示离线总时长；从未在线时为 0）
	gap time.Duration
	// neverOnline true 表示【从未收到过心跳】（服务重启后仍在监控集合里却一直没有心跳），
	// 与「曾在线后掉线」区分开：前者通常是 worker 从未启动，后者是运行中掉线。
	neverOnline bool
	// target 该 activity 所属环境生效的告警设置（决定发到哪个群、阈值与提醒间隔）
	target *envAlertSetting
}

// gapText 返回该离线项的时长文案（区分「从未上报心跳」与「已离线 X」）。
func (it activityOfflineItem) gapText() string {
	if it.neverOnline {
		return "从未上报心跳"
	}
	return "已离线 " + it.gap.Round(time.Second).String()
}

// thresholdText 返回该离线项所属环境的判定阈值文案。
func (it activityOfflineItem) thresholdText() string {
	if it.target == nil || it.target.threshold <= 0 {
		return "阈值未配置"
	}
	return it.target.threshold.String()
}

// activityKeyInfo 解析后的心跳缓存键。
type activityKeyInfo struct {
	project      string
	env          string
	actNamespace string
	actName      string
}

// parseCacheKey 拆分 cacheKey（project|env|actNamespace|actName）。
// 命名空间与 activity 名不含 "|"（worker 上报时的 field 即按首个 "|" 切分），故用 SplitN 取 4 段。
func parseCacheKey(ck string) (activityKeyInfo, bool) {
	parts := strings.SplitN(ck, "|", 4)
	if len(parts) != 4 {
		return activityKeyInfo{}, false
	}
	return activityKeyInfo{
		project:      parts[0],
		env:          parts[1],
		actNamespace: parts[2],
		actName:      parts[3],
	}, true
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
	// pubLister 用于获取「已发布上线」的 activity 集合（离线告警的监控对象）。
	// 为 nil 时不进行离线告警（仅保留日志收集与心跳统计）。
	pubLister PublishedActivityLister

	// 心跳缓存：key = project|actName，value = 最近 2 分钟内的心跳时间戳（秒）切片
	hbMu    sync.RWMutex
	hbCache map[string][]int64

	// 每个 activity 实例（project|env|ns|name）最近一次心跳时间戳（秒）。
	// 不随 1 分钟窗口裁剪（hbCache 会丢弃 2 分钟前的数据），用于判定「曾经在线但已掉线」。
	hbLastMu sync.RWMutex
	hbLast   map[string]int64

	// 离线告警监控集合：key = cacheKey(project|env|ns|name) → 该环境生效的告警设置。
	// 仅包含「该环境开启了告警」且「该 activity 已发布上线」的组合。
	monMu     sync.RWMutex
	monitored map[string]*envAlertSetting
	// monAddedAt 每条监控项【首次纳入监控集合】的时间（key 同 monitored）。
	// 用于「从未收到心跳」场景的宽限判定：刚纳入（服务刚启动 / 刚开启告警）时
	// 心跳可能还没采集上来，不能立刻判定离线，否则会一次性误报全部。
	monAddedAt map[string]time.Time

	// 离线状态机：key = cacheKey，仅在判定离线期间存在
	offlineMu sync.Mutex
	offline   map[string]*activityOfflineState

	// 离线告警诊断快照（供 OfflineAlertStatus 与周期日志排查「为什么不告警」）
	diagMu sync.RWMutex
	diag   offlineDiag
	// lastDiagLogAt 上一次输出诊断日志的时间（节流，避免每 30s 刷屏）
	lastDiagLogAt time.Time

	// 各环境 Redis 监听任务：key = project|env
	taskMu sync.Mutex
	tasks  map[string]*redisTask

	stopCh chan struct{}
}

// NewActivityCollector 创建活动日志/心跳收集器。
// lister 用于发现各环境的 Redis 配置；logRepo 用于活动日志落库；nodeLogRepo 用于 node 运行日志落库。
// 等价于 NewActivityCollectorWithAlert(..., nil)：不启用离线告警。
func NewActivityCollector(logRepo ActivityLogStore, nodeLogRepo NodeLogStore, lister EnvConfigLister) *ActivityCollector {
	return NewActivityCollectorWithAlert(logRepo, nodeLogRepo, lister, nil)
}

// NewActivityCollectorWithAlert 创建收集器，并可指定「已发布 activity」查询器以启用离线告警。
// pubLister 为 nil 时不启用离线告警（仅日志收集 + 心跳统计）。
func NewActivityCollectorWithAlert(logRepo ActivityLogStore, nodeLogRepo NodeLogStore,
	lister EnvConfigLister, pubLister PublishedActivityLister) *ActivityCollector {
	return &ActivityCollector{
		logRepo:     logRepo,
		nodeLogRepo: nodeLogRepo,
		lister:      lister,
		pubLister:   pubLister,
		hbCache:     make(map[string][]int64),
		hbLast:      make(map[string]int64),
		monitored:   make(map[string]*envAlertSetting),
		monAddedAt:  make(map[string]time.Time),
		offline:     make(map[string]*activityOfflineState),
		tasks:       make(map[string]*redisTask),
		stopCh:      make(chan struct{}),
	}
}

// Start 启动后台协调协程：周期性发现/回收各环境的 Redis 监听任务，并启动离线巡检。
func (c *ActivityCollector) Start() {
	if c.lister == nil {
		log.Warn().Msg("activity collector: no env config lister, skip start")
		return
	}
	go c.reconcileLoop()
	if c.pubLister != nil {
		c.refreshMonitored() // 先构建监控集合，避免首轮巡检无对象
		go c.offlineAlertLoop()
	} else {
		log.Info().Msg("activity collector: no published activity lister, offline alert disabled")
	}
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
		// 从 payload 提取 arguments 作为输入参数单独存字段，便于按 node 直接查看入参
		if len(rec.Arguments) == 0 && len(rec.Payload) > 0 {
			var p map[string]json.RawMessage
			if err := json.Unmarshal(rec.Payload, &p); err == nil {
				if args, ok := p["arguments"]; ok {
					rec.Arguments = args
				}
			}
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
// hbLast 是【唯一】在这里写入的：先 SCAN 心跳 key → HGetAll 读 field → 解析时间戳并过滤 → 写入。
func (c *ActivityCollector) scanHeartbeats(t *redisTask) {
	keys, err := c.scanKeys(t.redisCli, collectorHeartbeatPrefix+"*")
	if err != nil {
		log.Warn().Err(err).Str("project", t.project).Str("env", t.env).
			Msg("activity collector: scan heartbeat keys failed")
		c.recordHbScan(0, 0, 0, err.Error())
		return
	}
	now := time.Now().Unix()
	fresh := make(map[string][]int64)
	fieldCount := 0
	for _, key := range keys {
		// 从 namespace 解析 project/env：key = workflow:heartbeat:workflow/<project>/<env>
		// 【必须用 key 里的 env，不能用 t.env】——多个环境常共用同一个 Redis（或同一 DB），
		// SCAN 会把其它环境的心跳 key 也扫出来；若一律记为 t.env，会把 test 的心跳当成 prod 的，
		// 既造成跨环境串数据，也可能让 prod 的离线判定永远拿不到真实数据。
		project, keyEnv := parseNamespace(key[len(collectorHeartbeatPrefix):])
		if keyEnv == "" {
			keyEnv = t.env // 老数据/异常 key 兜底
		}
		fields, err := t.redisCli.HGetAll(context.Background(), key).Result()
		if err != nil {
			continue
		}
		for field, tsStr := range fields {
			fieldCount++
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
			ck := cacheKey(project, keyEnv, actNamespace, actName)
			fresh[ck] = append(fresh[ck], ts)
		}
	}
	freshCount := 0
	for _, tsList := range fresh {
		freshCount += len(tsList)
	}
	c.recordHbScan(len(keys), fieldCount, freshCount, "")

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

	// 记录「最近一次心跳时间」（不随窗口裁剪），供离线判定使用：
	// hbCache 会在 2 分钟后丢弃数据，仅靠它无法判断「曾经在线但现已掉线」。
	c.hbLastMu.Lock()
	for ck, tsList := range fresh {
		var maxTs int64
		for _, ts := range tsList {
			if ts > maxTs {
				maxTs = ts
			}
		}
		if maxTs > c.hbLast[ck] {
			c.hbLast[ck] = maxTs
		}
	}
	c.hbLastMu.Unlock()
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

// ============================================================
// Activity 离线告警
// ============================================================
// 判定口径（三条同时满足才告警，避免噪声）：
//  1. 总开关开启：custom.normal.activity_offline_alert_enabled（默认 true）；
//  2. 环境开启告警：该环境告警配置 alert_config.enabled = true（逐环境开关，屏蔽测试/开发环境）；
//  3. activity 已发布上线：出现在当前生效版本的根链发布快照中（含子链传递引用）；
//  4. 无有效心跳，两种情形都算离线：
//     a) 曾在线后掉线：hbLast 有记录，且 now - 最后心跳 ≥ 该环境阈值；
//     b) 从未收到心跳：hbLast 无记录 —— 覆盖「服务重启时就已经离线」的场景，
//        但需满足两个前提才判定（见下），否则会大面积误报。
//
// 关于 (b) 的两个防误报前提：
//   - 该环境必须有可用的 Redis 监听任务（setting.hasRedis）：否则管理端根本采集不到心跳，
//     「无心跳」不代表离线，跳过（否则会把该环境所有已发布 activity 全误报一遍）；
//   - 必须超过纳入宽限期 offlineStartupGrace（默认 2 分钟）：监控项刚纳入时
//     （服务刚启动 / 环境刚开启告警）心跳还没采集上来，需等待再判定。
//     宽限从【首次纳入监控】起算，不随每 5 分钟的监控刷新重置。

// offlineAlertLoop 周期执行：刷新监控集合 + 巡检离线状态。
func (c *ActivityCollector) offlineAlertLoop() {
	checkTicker := time.NewTicker(offlineCheckInterval)
	monTicker := time.NewTicker(monitorRefreshInterval)
	defer checkTicker.Stop()
	defer monTicker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-monTicker.C:
			c.refreshMonitored()
		case <-checkTicker.C:
			c.checkOfflineActivities()
		}
	}
}

// alertEnvs 扫描当前【开启了告警】的环境配置。
// 优先使用 AlertEnvLister（SQL 层过滤），否则回退为全量环境列表后内存筛选。
func (c *ActivityCollector) alertEnvs() []*EnvConfigDef {
	if al, ok := c.lister.(AlertEnvLister); ok {
		envs, err := al.ListAlertEnvs(context.Background())
		if err == nil {
			return envs
		}
		log.Warn().Err(err).Msg("offline alert: list alert envs failed, fallback to all envs")
	}
	envs, err := c.lister.ListAllEnvConfigs(context.Background())
	if err != nil {
		log.Warn().Err(err).Msg("offline alert: list env configs failed")
		return nil
	}
	out := make([]*EnvConfigDef, 0, len(envs))
	for _, e := range envs {
		if e != nil && e.AlertConfig.AlertEnabled() {
			out = append(out, e)
		}
	}
	return out
}

// refreshMonitored 重建离线告警监控集合：
// 对每个【开启告警】的环境，取其项目下所有【已发布上线】的 activity。
// 发布快照是项目级的，而心跳按 project+env 上报，因此需展开为 project|env|ns|name 的组合。
func (c *ActivityCollector) refreshMonitored() {
	if c.pubLister == nil || c.lister == nil {
		return
	}
	envs := c.alertEnvs()
	// 诊断：记录开启告警的环境，以及其中「没有 Redis 监听任务」的（采集不到心跳）
	envNames := make([]string, 0, len(envs))
	noRedis := make([]string, 0)
	// envKey → 该环境是否有可用的 Redis 监听任务
	redisOK := make(map[string]bool, len(envs))
	c.taskMu.Lock()
	for _, e := range envs {
		if e == nil || e.Project == "" || e.EnvName == "" {
			continue
		}
		envNames = append(envNames, e.Project+"/"+e.EnvName)
		if _, ok := c.tasks[taskKey(e.Project, e.EnvName)]; !ok {
			noRedis = append(noRedis, e.Project+"/"+e.EnvName)
			redisOK[e.Project+"/"+e.EnvName] = false
		} else {
			redisOK[e.Project+"/"+e.EnvName] = true
		}
	}
	c.taskMu.Unlock()

	if len(envs) == 0 {
		c.monMu.Lock()
		c.monitored = make(map[string]*envAlertSetting)
		c.monAddedAt = make(map[string]time.Time)
		c.monMu.Unlock()
		c.offlineMu.Lock()
		c.offline = make(map[string]*activityOfflineState)
		c.offlineMu.Unlock()
		c.recordDiag(func(d *offlineDiag) {
			d.alertEnvs = envNames
			d.noRedisEnv = noRedis
			d.pubErr = ""
			d.monCount = 0
		})
		return
	}
	// project → 开启告警的环境（含该环境生效的告警设置）
	alertEnvs := make(map[string]map[string]*envAlertSetting)
	for _, e := range envs {
		if e == nil || !e.AlertConfig.AlertEnabled() || e.Project == "" || e.EnvName == "" {
			continue
		}
		bucket, ok := alertEnvs[e.Project]
		if !ok {
			bucket = make(map[string]*envAlertSetting)
			alertEnvs[e.Project] = bucket
		}
		setting := buildEnvAlertSetting(e.AlertConfig)
		setting.hasRedis = redisOK[e.Project+"/"+e.EnvName]
		bucket[e.EnvName] = setting
	}
	next := make(map[string]*envAlertSetting)
	var pubErr string
	for project, envSettings := range alertEnvs {
		acts, err := c.pubLister.ListPublishedActivities(context.Background(), project)
		if err != nil {
			log.Warn().Err(err).Str("project", project).Msg("offline alert: list published activities failed")
			pubErr = err.Error()
			continue
		}
		for _, a := range acts {
			if a == nil || a.ActName == "" {
				continue
			}
			for env, setting := range envSettings {
				next[cacheKey(project, env, a.ActNamespace, a.ActName)] = setting
			}
		}
	}
	c.recordDiag(func(d *offlineDiag) {
		d.alertEnvs = envNames
		d.noRedisEnv = noRedis
		d.pubErr = pubErr
		d.monCount = len(next)
	})
	if len(noRedis) > 0 {
		log.Warn().Strs("envs", noRedis).
			Msg("offline alert: env has alert enabled but no redis listener task (no heartbeat can be collected)")
	}
	c.monMu.Lock()
	// 保留已存在监控项的首次纳入时间，仅为【新增项】记录当前时间：
	// 宽限期从「被纳入监控」起算，而不是从每次刷新起算，避免每 5 分钟重置一次宽限。
	addedAt := make(map[string]time.Time, len(next))
	nowAdded := time.Now()
	for ck := range next {
		if old, ok := c.monAddedAt[ck]; ok {
			addedAt[ck] = old
		} else {
			addedAt[ck] = nowAdded
		}
	}
	c.monitored = next
	c.monAddedAt = addedAt
	c.monMu.Unlock()

	// 监控集合变化后，已不在集合内的离线状态需要清理（如环境关闭告警 / activity 下线）
	c.offlineMu.Lock()
	for ck := range c.offline {
		if _, ok := next[ck]; !ok {
			delete(c.offline, ck)
		}
	}
	c.offlineMu.Unlock()
}

// recordDiag 在锁内更新诊断快照。
func (c *ActivityCollector) recordDiag(fn func(d *offlineDiag)) {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	fn(&c.diag)
	c.diag.updatedAt = time.Now()
}

// recordHbScan 累加记录一轮心跳扫描的统计（用于定位 hbLast 为空的原因）。
func (c *ActivityCollector) recordHbScan(keys, fields, fresh int, errStr string) {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	c.diag.scanRounds++
	c.diag.scanKeys += keys
	c.diag.scanFields += fields
	c.diag.scanFresh += fresh
	if errStr != "" {
		c.diag.scanErr = errStr
	}
	c.diag.scanAt = time.Now().Unix()
}

// OfflineAlertStatus 返回离线告警的当前诊断快照，用于排查「为什么没有告警」。
// 关注字段：
//   - alert_envs 为空 → 没有任何环境开启告警（去环境配置页开启）；
//   - no_redis_env 非空 → 这些环境开启了告警但采集不到心跳（未配 Redis / Redis 连不上）；
//   - monitored=0 → 该项目下没有「已发布上线」的 activity（根链未发布，或 pub_err 有错）；
//   - heartbeat_keys=0 → 一个心跳都没收到（worker 没起 / Redis 与 worker 的不是同一个 / DB 不一致）；
//   - matched=0 且上面都正常 → 监控的 activity 与实际心跳的 project|env|ns|name 对不上。
func (c *ActivityCollector) OfflineAlertStatus() map[string]any {
	c.diagMu.RLock()
	d := c.diag
	c.diagMu.RUnlock()
	c.hbLastMu.RLock()
	hbCount := len(c.hbLast)
	c.hbLastMu.RUnlock()
	c.offlineMu.Lock()
	offlineCount := len(c.offline)
	c.offlineMu.Unlock()
	enabled, thresholdSec, remindMin := config.GetActivityOfflineAlertPolicy()
	return map[string]any{
		"global_enabled":    enabled,
		"threshold_seconds": thresholdSec,
		"remind_minutes":    remindMin,
		"alert_envs":        append([]string(nil), d.alertEnvs...),
		"no_redis_env":      append([]string(nil), d.noRedisEnv...),
		"published_error":   d.pubErr,
		"monitored":         d.monCount,
		"heartbeat_keys":    hbCount,
		"matched":           d.matched,
		"offline":           offlineCount,
		"updated_at":        d.updatedAt.Unix(),
		// 心跳扫描统计（累加值），定位 hbLast 为空：
		//   scan_rounds=0  → 没有任何 Redis 监听任务（环境没配 Redis / 连不上）
		//   scan_keys=0    → Redis 里没有 workflow:heartbeat:* key（worker 没上报 / Redis 或 DB 不一致）
		//   scan_fields>0 但 scan_fresh=0 → 心跳时间戳超过 2 分钟（worker 早已停止）
		"hb_scan_rounds": d.scanRounds,
		"hb_scan_keys":   d.scanKeys,
		"hb_scan_fields": d.scanFields,
		"hb_scan_fresh":  d.scanFresh,
		"hb_scan_err":    d.scanErr,
		"hb_scan_at":     d.scanAt,
	}
}

// buildEnvAlertSetting 将环境的告警配置与全局配置合并为巡检用的设置快照。
// 环境未配置的项（nil）回落到全局 activity_offline_alert_* 配置。
func buildEnvAlertSetting(ac *EnvAlertConfig) *envAlertSetting {
	_, gThresholdSec, gRemindMin := config.GetActivityOfflineAlertPolicy()
	setting := &envAlertSetting{channel: ac.EffectiveChannel()}
	if ac != nil {
		setting.webhook = strings.TrimSpace(ac.Webhook)
	}
	// 阈值：环境优先，未配或 <=0 用全局
	if ac != nil && ac.ThresholdSec != nil && *ac.ThresholdSec > 0 {
		setting.threshold = time.Duration(*ac.ThresholdSec) * time.Second
	} else {
		setting.threshold = time.Duration(gThresholdSec) * time.Second
	}
	if setting.threshold <= 0 {
		setting.threshold = time.Duration(config.DefaultActivityOfflineAlertThresholdSec) * time.Second
	}
	// 提醒间隔：环境显式配置（含 0）优先，未配用全局
	if ac != nil && ac.RemindMinutes != nil {
		setting.remind = time.Duration(*ac.RemindMinutes) * time.Minute
	} else {
		setting.remind = time.Duration(gRemindMin) * time.Minute
	}
	return setting
}

// checkOfflineActivities 巡检监控集合内的 activity：
// 心跳中断超过【该环境配置的阈值】→ 告警（首次 + 按间隔重复提醒）；心跳恢复 → 发送恢复通知。
// 告警按环境配置的目标（专属 webhook / 全局发送器）分别发送。
func (c *ActivityCollector) checkOfflineActivities() {
	enabled, _, _ := config.GetActivityOfflineAlertPolicy()
	if !enabled {
		return
	}

	c.monMu.RLock()
	mon := make(map[string]*envAlertSetting, len(c.monitored))
	monAdded := make(map[string]time.Time, len(c.monAddedAt))
	for k, v := range c.monitored {
		mon[k] = v
	}
	for k, v := range c.monAddedAt {
		monAdded[k] = v
	}
	c.monMu.RUnlock()

	now := time.Now()
	c.hbLastMu.RLock()
	snap := make(map[string]int64, len(c.hbLast))
	hbCount := len(c.hbLast)
	for k, v := range c.hbLast {
		if _, ok := mon[k]; ok {
			snap[k] = v
		}
	}
	c.hbLastMu.RUnlock()

	// 诊断：监控集合与心跳数据的匹配情况（matched=0 是「不告警」的最常见根因）
	c.recordDiag(func(d *offlineDiag) { d.hbCount = hbCount; d.matched = len(snap) })
	// 节流输出诊断日志（每 5 分钟一次；监控为空或未匹配到任何心跳时用 Warn，便于直接发现根因）
	c.diagMu.Lock()
	needLog := (len(mon) == 0 || len(snap) == 0) && time.Since(c.lastDiagLogAt) > monitorRefreshInterval
	if needLog {
		c.lastDiagLogAt = time.Now()
		alertEnvs := append([]string(nil), c.diag.alertEnvs...)
		noRedis := append([]string(nil), c.diag.noRedisEnv...)
		pubErr := c.diag.pubErr
		c.diagMu.Unlock()
		log.Warn().Int("monitored", len(mon)).Int("heartbeat_keys", hbCount).Int("matched", len(snap)).
			Strs("alert_envs", alertEnvs).Strs("no_redis_env", noRedis).Str("published_error", pubErr).
			Msg("offline alert: no activity matched for monitoring, check env alert config / redis / published activities")
	} else {
		c.diagMu.Unlock()
	}

	var newOffline, remindOffline, recovered []activityOfflineItem

	c.offlineMu.Lock()
	for ck, lastTs := range snap {
		info, ok := parseCacheKey(ck)
		if !ok {
			continue
		}
		setting := mon[ck]
		if setting == nil {
			continue
		}
		last := time.Unix(lastTs, 0)
		gap := now.Sub(last)
		if gap < setting.threshold {
			continue
		}
		st := c.offline[ck]
		if st == nil {
			// 新增离线：首次告警
			st = &activityOfflineState{offlineAt: now, lastSeenAt: last, lastAlertAt: now}
			c.offline[ck] = st
			newOffline = append(newOffline, activityOfflineItem{
				project: info.project, env: info.env, target: setting,
				actNamespace: info.actNamespace, actName: info.actName, gap: gap,
			})
			continue
		}
		st.lastSeenAt = last
		// 长期下线（如已废弃的 worker）：超过 giveUp 后停止重复提醒并移出状态表，避免永久刷屏
		if now.Sub(st.offlineAt) > offlineGiveUp {
			delete(c.offline, ck)
			continue
		}
		if setting.remind > 0 && now.Sub(st.lastAlertAt) >= setting.remind {
			st.lastAlertAt = now
			remindOffline = append(remindOffline, activityOfflineItem{
				project: info.project, env: info.env, target: setting,
				actNamespace: info.actNamespace, actName: info.actName, gap: gap,
			})
		}
	}
	// 【从未收到心跳】的判定：已发布上线、所在环境能采集心跳，但纳入监控后一直没有心跳。
	// 覆盖「服务重启时就已经离线」的场景 —— 此时 hbLast 里根本没有记录，
	// 若只按 hbLast 判定就会漏报（用户反馈的真实问题）。
	// 两个前提避免误报：
	//   ① setting.hasRedis：环境没有 Redis 监听任务时管理端采集不到任何心跳，
	//      此时「无心跳」不代表离线，跳过（否则会把该环境全部已发布 activity 误报一遍）；
	//   ② 超过 offlineStartupGrace 宽限期：刚启动 / 刚开启告警时心跳还没采集上来，需等待。
	for ck, setting := range mon {
		if setting == nil || !setting.hasRedis {
			continue
		}
		if _, ok := snap[ck]; ok {
			continue // 有心跳记录，走上面的「心跳中断」判定
		}
		addedAt, ok := monAdded[ck]
		if !ok || addedAt.IsZero() {
			addedAt = now // 兜底：未记录纳入时间时按当前时间起算宽限
		}
		if now.Sub(addedAt) < offlineStartupGrace {
			continue // 宽限期内不判定
		}
		info, ok := parseCacheKey(ck)
		if !ok {
			continue
		}
		st := c.offline[ck]
		if st == nil {
			st = &activityOfflineState{offlineAt: now, lastAlertAt: now, neverOnline: true}
			c.offline[ck] = st
			newOffline = append(newOffline, activityOfflineItem{
				project: info.project, env: info.env, target: setting,
				actNamespace: info.actNamespace, actName: info.actName, neverOnline: true,
			})
			continue
		}
		st.neverOnline = true
		if now.Sub(st.offlineAt) > offlineGiveUp {
			delete(c.offline, ck)
			continue
		}
		if setting.remind > 0 && now.Sub(st.lastAlertAt) >= setting.remind {
			st.lastAlertAt = now
			remindOffline = append(remindOffline, activityOfflineItem{
				project: info.project, env: info.env, target: setting,
				actNamespace: info.actNamespace, actName: info.actName, neverOnline: true,
			})
		}
	}
	// 恢复判定：仍在监控集合内且心跳已恢复
	for ck, st := range c.offline {
		setting, ok := mon[ck]
		if !ok || setting == nil {
			continue
		}
		lastTs, ok := snap[ck]
		if !ok {
			continue
		}
		if now.Sub(time.Unix(lastTs, 0)) < setting.threshold {
			if info, ok2 := parseCacheKey(ck); ok2 {
				recovered = append(recovered, activityOfflineItem{
					project: info.project, env: info.env, target: setting,
					actNamespace: info.actNamespace, actName: info.actName,
					gap: now.Sub(st.offlineAt),
				})
			}
			delete(c.offline, ck)
		}
	}
	c.offlineMu.Unlock()

	// 按告警目标（环境专属 webhook / 全局发送器）分组发送，避免跨环境的告警串群
	sendActivityOfflineAlerts("Activity 离线", newOffline)
	sendActivityOfflineAlerts("Activity 持续离线", remindOffline)
	sendActivityRecoveredAlerts(recovered)
}

// groupByTarget 按告警目标分组：key = webhook（空串表示用全局发送器）。
func groupByTarget(items []activityOfflineItem) map[string][]activityOfflineItem {
	groups := make(map[string][]activityOfflineItem)
	for _, it := range items {
		target := ""
		if it.target != nil {
			target = it.target.webhook
		}
		groups[target] = append(groups[target], it)
	}
	return groups
}

// sendActivityOfflineAlerts 按目标分组汇总发送离线告警（未配置告警发送器时仅记日志）。
func sendActivityOfflineAlerts(kind string, items []activityOfflineItem) {
	for target, group := range groupByTarget(items) {
		if len(group) == 0 {
			continue
		}
		threshold := group[0].thresholdText()
		lines := make([]string, 0, len(group)+4)
		lines = append(lines, fmt.Sprintf("离线数量: %d", len(group)))
		lines = append(lines, fmt.Sprintf("判定阈值: 超过 %s 未上报心跳", threshold))
		for i, it := range group {
			if i >= offlineAlertBatchMax {
				lines = append(lines, fmt.Sprintf("… 其余 %d 个省略", len(group)-offlineAlertBatchMax))
				break
			}
			lines = append(lines, fmt.Sprintf("%d) 项目: %s｜环境: %s｜命名空间: %s｜Activity: %s｜%s",
				i+1, it.project, it.env, it.actNamespace, it.actName, it.gapText()))
		}
		lines = append(lines, "说明: 该 Activity 已发布到线上根链。若从未上报心跳，通常是 worker 未启动或心跳 Redis 配置不一致；")
		lines = append(lines, "      若是心跳中断，说明 worker 已停止或异常退出。两种情况都会导致流程调用失败。")
		lines = append(lines, "时间: "+time.Now().Format("2006-01-02 15:04:05"))
		log.Warn().Str("kind", kind).Int("count", len(group)).Str("target", target).Msg("activity offline detected")
		sendAlertToTarget(target, "[工作流告警] "+kind, strings.Join(lines, "\n"))
	}
}

// sendActivityRecoveredAlerts 按目标分组汇总发送恢复通知。
func sendActivityRecoveredAlerts(items []activityOfflineItem) {
	for target, group := range groupByTarget(items) {
		if len(group) == 0 {
			continue
		}
		lines := make([]string, 0, len(group)+3)
		lines = append(lines, fmt.Sprintf("恢复数量: %d", len(group)))
		for i, it := range group {
			if i >= offlineAlertBatchMax {
				lines = append(lines, fmt.Sprintf("… 其余 %d 个省略", len(group)-offlineAlertBatchMax))
				break
			}
			lines = append(lines, fmt.Sprintf("%d) 项目: %s｜环境: %s｜命名空间: %s｜Activity: %s｜恢复前状态: %s",
				i+1, it.project, it.env, it.actNamespace, it.actName,
				func() string {
					if it.neverOnline {
						return "从未上报心跳"
					}
					return "离线时长 " + it.gap.Round(time.Second).String()
				}()))
		}
		lines = append(lines, "时间: "+time.Now().Format("2006-01-02 15:04:05"))
		log.Info().Int("count", len(group)).Str("target", target).Msg("activity offline recovered")
		sendAlertToTarget(target, "[工作流恢复] Activity 已重新上线", strings.Join(lines, "\n"))
	}
}

// sendAlertToTarget 向指定目标发送告警：
// target 为空 → 用全局发送器（全局飞书 webhook）；否则用该环境专属 webhook 构造发送器。
func sendAlertToTarget(target, title, content string) {
	if target == "" {
		SendAlert(context.Background(), title, content)
		return
	}
	sender := feishuSenderFor(target)
	if sender == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Msgf("offline alert: panic when sending alert, title=%s, err=%v", title, r)
			}
		}()
		sender.SendAlert(context.Background(), title, content)
	}()
}

// feishuSenderFor 按 webhook 取（或创建）飞书发送器，避免每次告警重复构造。
func feishuSenderFor(webhook string) AlertSender {
	if webhook == "" {
		return nil
	}
	feishuSenderMu.RLock()
	s, ok := feishuSenderCache[webhook]
	feishuSenderMu.RUnlock()
	if ok {
		return s
	}
	feishuSenderMu.Lock()
	defer feishuSenderMu.Unlock()
	if s, ok = feishuSenderCache[webhook]; ok {
		return s
	}
	s = &feishuAlertSender{webhook: webhook}
	feishuSenderCache[webhook] = s
	return s
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
// 【坑】原实现判断 len(parts)>=2 却直接取 parts[2]：当 namespace 只有两段（如 "workflow/demo"，
// 缺少 env）时会 index out of range panic；而本函数在心跳扫描协程里调用，panic 会直接杀掉
// 整个进程（goroutine 里的 panic 无法被外层 recover）。因此段数不足时必须安全返回。
func parseNamespace(ns string) (project, env string) {
	parts := strings.SplitN(ns, "/", 3)
	switch len(parts) {
	case 3:
		return parts[1], parts[2]
	case 2:
		return parts[1], ""
	default:
		return "", ""
	}
}

// TestRedisConnect 探测 Redis 连通性并返回简要服务端信息，供管理端「测试连接」使用。
// 只做只读操作（PING / INFO / DBSIZE），不写入任何数据；ctx 需带超时，避免错误地址把请求挂住。
// 返回字段：ok / ping / latency_ms / server_version / db_size 等。
func TestRedisConnect(ctx context.Context, cfg *RedisConfig) (map[string]any, error) {
	if cfg == nil || cfg.Addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	cli := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		// 地址写错时尽快失败并给出明确错误，避免默认重试链（多次退避会拖到数秒并刷日志）
		MaxRetries: 1,
	})
	defer func() { _ = cli.Close() }()

	start := time.Now()
	pong, err := cli.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"ok":         true,
		"ping":       pong,
		"latency_ms": time.Since(start).Milliseconds(),
	}
	// 以下为补充信息，取不到不影响「是否连通」的结论
	if info, ierr := cli.Info(ctx, "server").Result(); ierr == nil {
		for _, line := range strings.Split(info, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "redis_version:") {
				out["server_version"] = strings.TrimPrefix(line, "redis_version:")
				break
			}
		}
	}
	if n, derr := cli.DBSize(ctx).Result(); derr == nil {
		out["db_size"] = n
	}
	return out, nil
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
