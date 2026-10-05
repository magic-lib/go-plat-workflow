package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// ============================================================
// 根链版本变更 —— 跨副本广播失效（Redis pub/sub）
// ============================================================
//
// 背景：InvokeRootChain 的 DSL 缓存与 rulego 引擎池都是【进程内】缓存。
// 多副本部署时，发布/回滚请求只会打到其中一个副本，只有该副本失效缓存并切到新版本，
// 其余副本仍使用旧 DSL 缓存与旧引擎实例，直到 TTL/数量策略回收或进程重启才更新。
//
// 方案：版本变更时通过 Redis pub/sub 广播一条失效事件，所有副本订阅该频道，
// 收到后各自失效本地缓存 → 全副本立即切到新版本，无需等待策略回收或重启。
//
// 通道选择：复用 env_config 中各环境已配置的 Redis（无需新增配置项）。
//   - 广播端：向该 project 下所有配置了 Redis 的 env 各发一次（按 addr+db 去重）；
//   - 订阅端：订阅所有 env 的 Redis（按 addr+db 去重），env 变更会自动对齐。
//     这样服务任一 project/env 的副本都能收到与自己相关的事件。
//
// 降级：Redis 未配置或不可用时仅记录日志并跳过，不影响主流程与单机部署。
//
// 注意：订阅端只做失效，不再广播，否则会形成无限循环。

const (
	// rootChainInvalidateChannel 根链失效广播频道名。
	rootChainInvalidateChannel = "workflow:rootchain:invalidate"
	// pubSubRefreshInterval 订阅集合刷新间隔（env 配置变更时自动对齐）。
	pubSubRefreshInterval = 3 * time.Minute
	// pubSubPublishTimeout 单次广播的超时时间。
	pubSubPublishTimeout = 5 * time.Second
)

// 变更原因（用于日志与订阅端区分处理）。
const (
	reasonPublish    = "publish"
	reasonRollback   = "rollback"
	reasonSetCurrent = "set_current"
	reasonDelete     = "delete"
)

// rootChainInvalidateEvent 广播事件内容。
type rootChainInvalidateEvent struct {
	// Project 项目名。
	Project string `json:"project"`
	// ChainKey 根链 key。
	ChainKey string `json:"chain_key"`
	// Reason 变更原因：publish / rollback / set_current / delete。
	Reason string `json:"reason"`
	// SenderID 发送方实例标识，订阅端据此忽略自己发出的消息
	// （Redis pub/sub 会把消息回发给所有订阅者，包括发送方自己）。
	SenderID string `json:"sender_id"`
	// At 事件时间（Unix 秒）。
	At int64 `json:"at"`
}

// redisAddrKey Redis 实例去重标识（同一实例被多个 env 共用时只建一个订阅）。
func redisAddrKey(cfg *workflow.RedisConfig) string {
	if cfg == nil {
		return ""
	}
	return fmt.Sprintf("%s|%d", cfg.Addr, cfg.DB)
}

// redisSubscriber 单个 Redis 实例的订阅句柄。
type redisSubscriber struct {
	cli    *redis.Client
	cancel context.CancelFunc
}

// invalidatePubSub 根链失效事件的广播与订阅管理器。
type invalidatePubSub struct {
	svc *WorkflowService
	// selfID 本实例标识，用于忽略自己发出的广播。
	selfID string

	mu        sync.Mutex
	subs      map[string]*redisSubscriber
	stop      chan struct{}
	closeOnce sync.Once
	started   bool
}

// newInvalidatePubSub 创建管理器（此时不建立连接）。
func newInvalidatePubSub(svc *WorkflowService) *invalidatePubSub {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return &invalidatePubSub{
		svc:    svc,
		selfID: fmt.Sprintf("%s-%d", host, os.Getpid()),
		subs:   make(map[string]*redisSubscriber),
		stop:   make(chan struct{}),
	}
}

// Start 启动订阅刷新循环（后台常驻）。
func (m *invalidatePubSub) Start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	go m.refreshLoop()
}

// Stop 停止全部订阅与刷新循环。
func (m *invalidatePubSub) Stop() {
	m.closeOnce.Do(func() {
		close(m.stop)
	})
	m.mu.Lock()
	for k, s := range m.subs {
		s.cancel()
		if err := s.cli.Close(); err != nil {
			log.Debug().Err(err).Str("redis", k).Msg("Close invalidate subscription client")
		}
		delete(m.subs, k)
	}
	m.mu.Unlock()
}

// refreshLoop 定期对齐订阅集合。
func (m *invalidatePubSub) refreshLoop() {
	m.refresh(context.Background())

	ticker := time.NewTicker(pubSubRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.refresh(context.Background())
		}
	}
}

// refresh 对齐订阅集合：为新增的 Redis 建立订阅，停止已移除的。
// 任何错误仅记录日志，保证 Redis 异常时不影响主流程。
func (m *invalidatePubSub) refresh(ctx context.Context) {
	envs, err := m.svc.envConfigRepo.ListAll(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("Skip invalidate subscription refresh: list env configs failed")
		return
	}

	wanted := make(map[string]*workflow.RedisConfig)
	for _, e := range envs {
		if e == nil || e.RedisConfig == nil || e.RedisConfig.Addr == "" {
			continue
		}
		wanted[redisAddrKey(e.RedisConfig)] = e.RedisConfig
	}

	// 1) 停止不再需要的订阅
	m.mu.Lock()
	for k, s := range m.subs {
		if _, ok := wanted[k]; ok {
			continue
		}
		s.cancel()
		if cerr := s.cli.Close(); cerr != nil {
			log.Debug().Err(cerr).Str("redis", k).Msg("Close stale invalidate subscription")
		}
		delete(m.subs, k)
		log.Info().Str("redis", k).Msg("Root chain invalidate subscription removed")
	}
	// 2) 收集待新建的订阅
	toStart := make([]*workflow.RedisConfig, 0, len(wanted))
	for k, cfg := range wanted {
		if _, ok := m.subs[k]; !ok {
			toStart = append(toStart, cfg)
		}
	}
	m.mu.Unlock()

	for _, cfg := range toStart {
		m.startSubscribe(cfg)
	}
}

// isStopped 判断管理器是否已停止。
func (m *invalidatePubSub) isStopped() bool {
	select {
	case <-m.stop:
		return true
	default:
		return false
	}
}

// startSubscribe 对单个 Redis 建立订阅并启动接收协程。
func (m *invalidatePubSub) startSubscribe(cfg *workflow.RedisConfig) {
	if m.isStopped() {
		return
	}
	cli, err := workflow.NewActivityCollectorRedisClient(cfg)
	if err != nil {
		log.Debug().Err(err).Str("addr", cfg.Addr).Msg("Skip invalidate subscription: redis unavailable")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	sub := cli.Subscribe(ctx, rootChainInvalidateChannel)
	key := redisAddrKey(cfg)

	m.mu.Lock()
	// 并发刷新可能已建立同 key 订阅，避免重复
	if _, exists := m.subs[key]; exists {
		m.mu.Unlock()
		cancel()
		if cerr := sub.Close(); cerr != nil {
			log.Debug().Err(cerr).Msg("Close duplicate subscription")
		}
		if cerr := cli.Close(); cerr != nil {
			log.Debug().Err(cerr).Msg("Close duplicate subscription client")
		}
		return
	}
	m.subs[key] = &redisSubscriber{cli: cli, cancel: cancel}
	m.mu.Unlock()

	log.Info().Str("redis", key).Str("channel", rootChainInvalidateChannel).
		Msg("Root chain invalidate subscription established")

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Any("panic", rec).Msg("Recovered panic in invalidate subscription loop")
			}
		}()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				if cerr := sub.Close(); cerr != nil {
					log.Debug().Err(cerr).Msg("Close subscription on ctx done")
				}
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				m.handleMessage(msg.Payload)
			}
		}
	}()
}

// handleMessage 处理收到的失效事件（只失效本地，不再广播，避免循环）。
func (m *invalidatePubSub) handleMessage(payload string) {
	var ev rootChainInvalidateEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		log.Debug().Err(err).Msg("Ignore malformed root chain invalidate event")
		return
	}
	if ev.Project == "" || ev.ChainKey == "" {
		return
	}
	if ev.SenderID == m.selfID {
		return // 忽略自己发出的广播
	}

	// 失效本地 DSL 缓存，并把在线版本标记为未知：
	// 下次调用会重新解析到新版本，从而全副本立即切到新版本。
	m.svc.ClearChainRootByKey(ev.Project, ev.ChainKey)

	// 根链已被删除：连同该链在引擎池中的各版本实例一并清理，避免残留。
	if ev.Reason == reasonDelete {
		m.svc.invalidateChainPoolEntry(cacheKeyOf(ev.Project, ev.ChainKey))
	}

	log.Info().Str("project", ev.Project).Str("chain_key", ev.ChainKey).Str("reason", ev.Reason).
		Msg("Root chain cache invalidated by broadcast from peer instance")
}

// broadcast 向该 project 下所有配置了 Redis 的 env 广播失效事件（异步、去重、失败降级）。
func (m *invalidatePubSub) broadcast(project, chainKey, reason string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().Any("panic", rec).Msg("Recovered panic during root chain invalidate broadcast")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), pubSubPublishTimeout)
	defer cancel()

	envs, err := m.svc.envConfigRepo.ListByProject(ctx, project)
	if err != nil {
		log.Debug().Err(err).Str("project", project).Msg("Skip invalidate broadcast: list env configs failed")
		return
	}

	payload, err := json.Marshal(rootChainInvalidateEvent{
		Project:  project,
		ChainKey: chainKey,
		Reason:   reason,
		SenderID: m.selfID,
		At:       time.Now().Unix(),
	})
	if err != nil {
		log.Debug().Err(err).Msg("Skip invalidate broadcast: marshal event failed")
		return
	}

	seen := make(map[string]struct{})
	for _, e := range envs {
		if e == nil || e.RedisConfig == nil || e.RedisConfig.Addr == "" {
			continue
		}
		key := redisAddrKey(e.RedisConfig)
		if _, dup := seen[key]; dup {
			continue // 同一 Redis 实例只发一次
		}
		seen[key] = struct{}{}

		cli, cerr := workflow.NewActivityCollectorRedisClient(e.RedisConfig)
		if cerr != nil {
			log.Debug().Err(cerr).Str("addr", e.RedisConfig.Addr).Msg("Skip invalidate broadcast: redis unavailable")
			continue
		}
		if perr := cli.Publish(ctx, rootChainInvalidateChannel, payload).Err(); perr != nil {
			log.Debug().Err(perr).Str("addr", e.RedisConfig.Addr).Msg("Root chain invalidate broadcast failed")
		}
		if cerr2 := cli.Close(); cerr2 != nil {
			log.Debug().Err(cerr2).Msg("Close broadcast redis client")
		}
	}
}
