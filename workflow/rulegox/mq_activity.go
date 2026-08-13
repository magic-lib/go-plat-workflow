package rulegox

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"github.com/magic-lib/go-plat-utils/plugins/activity"
	"github.com/magic-lib/go-plat-utils/templates"
	"github.com/magic-lib/go-plat-utils/utils/httputil"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-utils/conv"
	"github.com/magic-lib/go-plat-utils/utils"
	"github.com/redis/go-redis/v9"
	"log"

	mq "github.com/magic-lib/go-plat-mq/mq"
)

const (
	workflowTopic   = "workflow"
	workflowTimeout = 30 * time.Second
	activityTopic   = "activity"
	maxLogsSize     = 10000

	// 心跳上报间隔：每 10s 统一上报一次，多个 actName 只需一次 HSET 即可完成，减少请求量
	heartbeatInterval = 10 * time.Second

	// HeartbeatKeyPrefix ActivityLogKeyPrefix  Redis key 约定（服务端按相同命名读取）：
	//   心跳：workflow:heartbeat:<namespace>   —— Hash，field=actName，value=最近心跳时间戳(秒)
	//   日志：workflow:activity:log:<namespace> —— List，element 为 activityLogRecord 的 JSON
	//   namespace 即 getMQNamespace 生成的项目+环境标识
	HeartbeatKeyPrefix   = "workflow:heartbeat:"
	ActivityLogKeyPrefix = "workflow:activity:log:"
	HeaderTraceIdKey     = "X-Trace-Id"
	HeaderSpanIdKey      = "X-Span-Id"
	HeaderRootChainIdKey = "X-Root-Chain-Id"
)

type MetaDataHeader struct {
	RootChainID string         `json:"root_chain_id"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	Attributes  map[string]any `json:"attributes"`
}

func (m *MetaDataHeader) FromHeader(header http.Header) *MetaDataHeader {
	m.RootChainID = header.Get(HeaderRootChainIdKey)
	m.TraceID = header.Get(HeaderTraceIdKey)
	m.SpanID = header.Get(HeaderSpanIdKey)
	return m
}

func (m *MetaDataHeader) ToHeader(header http.Header) http.Header {
	if header == nil {
		header = http.Header{}
	}
	header.Set(HeaderRootChainIdKey, m.RootChainID)
	header.Set(HeaderTraceIdKey, m.TraceID)
	header.Set(HeaderSpanIdKey, m.SpanID)
	return header
}

// activityLogRecord 单次 activity 执行日志，序列化后写入 redis list，
// 由服务端消费并落库到对应 activity 的日志表。
type activityLogRecord struct {
	Project      string `json:"project"`       // 项目名
	Env          string `json:"env"`           // 环境
	ActNamespace string `json:"act_namespace"` // 活动命名空间
	ActName      string `json:"act_name"`      // 活动名称
	EventID      string `json:"event_id"`      // 消息/链路 ID
	Level        string `json:"level"`         // info / error
	Timestamp    int64  `json:"timestamp"`     // 日志时间戳（秒）
	DurationMs   int64  `json:"duration_ms"`   // 执行耗时（毫秒）
	Payload      any    `json:"payload,omitempty"`
	Result       any    `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	RootChainID  string `json:"root_chain_id,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
	SpanID       string `json:"span_id,omitempty"`
	Attributes   string `json:"attributes,omitempty"`
}

// MQWorker 分布式任务执行端（worker）。
// 部署在分布式节点上，订阅 workflow 相关的 topic，收到任务后实际执行节点或 rootChain，
// 并将结果（或错误）通过 asynq 的 ResultWriter 回写给调用方（mq.Request 同步等待）。
//
// 使用方式（在 worker 进程启动处）：
//
//	w, err := workflow.NewMQWorker(db, redisCfg)
//	if err != nil { panic(err) }
//	defer w.Stop()
type MQWorker struct {
	mqClient *mq.AsynqMessageQueue
	Project  string
	Env      string

	// 自建 redis 客户端，用于心跳上报与执行日志写入（与 MQ 共用同一 redis 实例）
	redisCli *redis.Client

	// 心跳注册表：key = actNamespace|actName，value = 注册信息
	hbMu       sync.Mutex
	heartbeats map[string]*activityHeartbeat
	hbStarted  bool
	hbCancel   chan struct{}
}

// activityHeartbeat 心跳注册项
type activityHeartbeat struct {
	ActNamespace string
	ActName      string
}

// NewMQWorker 创建 MQ worker，内部构建 asynq 消费端
func NewMQWorker(projectName, env string, redisCfg *conn.Connect) (*MQWorker, error) {
	cfg := &mq.AsynqMessageQueue{
		Namespace: getMQNamespace(projectName, env), //项目+环境命名空间
		Timeout:   workflowTimeout,
	}
	client, err := mq.NewAsynqMessageQueue(redisCfg, cfg)
	if err != nil {
		return nil, fmt.Errorf("create asynq mq client for worker: %w", err)
	}
	w := &MQWorker{
		mqClient:   client,
		Project:    projectName,
		Env:        env,
		heartbeats: make(map[string]*activityHeartbeat),
		hbCancel:   make(chan struct{}),
	}
	// 自建 redis 客户端（心跳与日志共用）；失败不阻断主流程，仅降级
	if rc, rErr := newRedisClient(redisCfg); rErr != nil {
		log.Println("mq_worker: init redis client failed, heartbeat/log disabled")
	} else {
		w.redisCli = rc
	}
	return w, nil
}

// newRedisClient 基于 conn.Connect 构建 go-redis 客户端
func newRedisClient(redisCfg *conn.Connect) (*redis.Client, error) {
	if redisCfg == nil || redisCfg.Host == "" || redisCfg.Port == "" {
		return nil, fmt.Errorf("redis config error")
	}
	db := 0
	if redisCfg.Database != "" {
		if n, err := strconv.Atoi(redisCfg.Database); err == nil {
			db = n
		}
	}
	opt := &redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisCfg.Host, redisCfg.Port),
		Username: redisCfg.Username,
		Password: redisCfg.Password,
		DB:       db,
	}
	cli := redis.NewClient(opt)
	if err := cli.Ping(context.Background()).Err(); err != nil {
		_ = cli.Close()
		return nil, err
	}
	return cli, nil
}

// SubscribeActivity 订阅指定 activity 并注册处理函数。
// 注册成功后：
//  1. 该 actName 加入心跳注册表，由统一的后台协程每 10s 上报一次心跳（多 actName 仅一次 HSET）。
//  2. mqHandler 每次被调用都会向 redis 写入一条执行日志，供服务端采集落库。
func (w *MQWorker) SubscribeActivity(actNamespace, actName string, handler utils.ContextAnyHandler) error {
	methodTopic := getActivityTopic(actNamespace, actName)
	mqHandler := func(ctx context.Context, event *mq.Event) (any, error) {
		start := time.Now()
		// 执行前记一条 info 日志
		w.pushActivityLog(actNamespace, actName, event, "info", start.Unix(), 0, event.Payload, nil, "", nil)

		resp, herr := handler(ctx, event.Payload)

		durationMs := time.Since(start).Milliseconds()
		if herr != nil {
			// 执行失败记一条 error 日志
			w.pushActivityLog(actNamespace, actName, event, "error", start.Unix(), durationMs, event.Payload, nil, herr.Error(), nil)
		} else {
			w.pushActivityLog(actNamespace, actName, event, "info", start.Unix(), durationMs, event.Payload, resp, "", nil)
		}
		return resp, herr
	}
	if err := w.mqClient.Subscribe(methodTopic, mqHandler); err != nil {
		return err
	}

	// 注册成功：加入心跳列表，并（首次）启动统一心跳上报协程
	w.registerHeartbeat(actNamespace, actName)
	return nil
}

// registerHeartbeat 将 activity 加入心跳注册表，首次注册时启动后台上报协程
func (w *MQWorker) registerHeartbeat(actNamespace, actName string) {
	w.hbMu.Lock()
	key := actNamespace + "|" + actName
	if _, ok := w.heartbeats[key]; !ok {
		w.heartbeats[key] = &activityHeartbeat{ActNamespace: actNamespace, ActName: actName}
	}
	if !w.hbStarted {
		w.hbStarted = true
		go w.heartbeatLoop()
	}
	w.hbMu.Unlock()
}

// heartbeatLoop 每 10s 统一上报一次心跳：用一个 HSET 写入所有已注册 actName 的当前时间戳，
// 多个 actName 只需一次网络请求即可完成。
func (w *MQWorker) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.hbCancel:
			return
		case <-ticker.C:
			w.reportHeartbeatOnce()
		}
	}
}

// reportHeartbeatOnce 单次上报：将所有已注册 actName 的时间戳批量写入同一个 hash key
func (w *MQWorker) reportHeartbeatOnce() {
	if w.redisCli == nil {
		return
	}
	w.hbMu.Lock()
	if len(w.heartbeats) == 0 {
		w.hbMu.Unlock()
		return
	}
	fields := make(map[string]any, len(w.heartbeats))
	now := time.Now().Unix()
	for _, hb := range w.heartbeats {
		fields[hb.ActName] = now
	}
	w.hbMu.Unlock()

	key := HeartbeatKeyPrefix + getMQNamespace(w.Project, w.Env)
	if err := w.redisCli.HSet(context.Background(), key, fields).Err(); err != nil {
		log.Println("mq_worker: report heartbeat failed")
		return
	}
	// 设置过期时间，避免 worker 异常退出后心跳永远残留（2 个周期 + 缓冲）
	_ = w.redisCli.Expire(context.Background(), key, heartbeatInterval*3).Err()
}

// pushActivityLog 向 redis list 推送一条 activity 执行日志（服务端消费后落库）。
// rootChainID 为根链 ID（可为空）；attributes 为动态附加属性（任意可序列化对象，可为 nil）。
// trace_id / span_id 优先从 event.Headers 中的 Trace-Id / Span-Id 提取（worker 发送时已注入）。
func (w *MQWorker) pushActivityLog(actNamespace, actName string, event *mq.Event, level string, ts, durationMs int64, payload, result any, errMsg string, attributes any) {
	if w.redisCli == nil {
		return
	}
	if event == nil {
		return
	}
	metaData := new(MetaDataHeader).FromHeader(event.Headers)
	if metaData.SpanID == "" {
		metaData.SpanID = event.Id
	}

	rec := activityLogRecord{
		Project:      w.Project,
		Env:          w.Env,
		ActNamespace: actNamespace,
		ActName:      actName,
		EventID:      eventIdOf(event),
		Level:        level,
		Timestamp:    ts,
		DurationMs:   durationMs,
		Payload:      payload,
		Result:       result,
		Error:        errMsg,
		RootChainID:  metaData.RootChainID,
		TraceID:      metaData.TraceID,
		SpanID:       metaData.SpanID,
		Attributes:   attrToJSONString(attributes),
	}
	key := ActivityLogKeyPrefix + getMQNamespace(w.Project, w.Env)
	pipe := w.redisCli.Pipeline()
	pipe.RPush(context.Background(), key, conv.String(rec))
	// 只保留最新 10000 条，超出自动砍掉旧数据，数字按需调整
	pipe.LTrim(context.Background(), key, 0-maxLogsSize, -1)
	_, err := pipe.Exec(context.Background())
	if err != nil {
		log.Println("mq_worker: push activity log failed, err:", err)
	}
}

// eventIdOf 安全获取 event 的 ID
func eventIdOf(event *mq.Event) string {
	if event == nil {
		return ""
	}
	return event.Id
}

// attrToJSONString 将任意属性值序列化为 JSON 字符串，便于存入 ActivityLogDef.Attributes（string 类型）。
// nil 时返回空串；其余按 JSON 序列化。
func attrToJSONString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
func (w *MQWorker) requestOneActivity(ctx context.Context, actNamespace, actName string, params any, headers http.Header) (*httputil.CommResponse, error) {
	methodTopic := getActivityTopic(actNamespace, actName)
	resp, err := w.mqClient.Request(ctx, &mq.Event{
		Id:      id.NewUUID(),
		Topic:   methodTopic,
		Payload: params,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	ev := new(mq.Event)
	err = conv.Unmarshal(resp.Params, ev)
	if err == nil {
		resp.Params = ev
	}
	return resp, nil
}

// RequestActivity 订阅指定 topic 并注册处理函数。
func (w *MQWorker) RequestActivity(ctx context.Context, act *activity.Activity, params any, headers http.Header) (*httputil.CommResponse, error) {
	if act == nil {
		return nil, fmt.Errorf("activity is nil")
	}
	actNamespace := act.ActNamespace
	actName := act.ActName
	argTemplate := act.ArgTemplate

	if actNamespace == "" || actName == "" {
		return nil, fmt.Errorf("act_namespace and act_name are required")
	}
	ruleEngine := templates.NewRuleExprEngine()
	argAny, err := ruleEngine.RenderObject(argTemplate, params)
	if err != nil {
		return nil, err
	}
	resp, err := w.requestOneActivity(ctx, actNamespace, actName, argAny, headers)
	if err != nil {
		return nil, err
	}
	responses := ""
	if len(act.Responses) > 0 {
		responses = conv.String(act.Responses)
	}
	data, err := ruleEngine.RenderObject(responses, resp.Data)
	if err != nil {
		return nil, err
	}
	// 需要对返回值类型进行判断，然后进行转换
	resp.Data = data
	return resp, nil
}

// Stop 停止消费端，并清理心跳协程与 redis 连接。
func (w *MQWorker) Stop() {
	// 停止心跳上报
	w.hbMu.Lock()
	if w.hbStarted {
		w.hbStarted = false
		close(w.hbCancel)
	}
	w.hbMu.Unlock()
	// 最后再上报一次，确保服务端能看到最新状态
	w.reportHeartbeatOnce()

	if w.redisCli != nil {
		_ = w.redisCli.Close()
	}
	w.mqClient.Close()
}

func getMQNamespace(projectName, env string) string {
	return fmt.Sprintf("%s/%s/%s", workflowTopic, projectName, env)
}
func getActivityTopic(actNamespace, actName string) string {
	return fmt.Sprintf("%s/%s/%s", activityTopic, actNamespace, actName)
}
