package workflow

import (
	"context"
	"strings"
	"testing"
	"time"
)

// captureAlertSender 测试用告警发送器：记录最近收到的告警，便于断言。
type captureAlertSender struct {
	ch chan string
}

func (s *captureAlertSender) SendAlert(ctx context.Context, title, content string) {
	select {
	case s.ch <- title + "\n" + content:
	default:
	}
}

// fakePublishedLister 返回固定的「已发布 activity」集合。
type fakePublishedLister struct {
	acts []*PublishedActivityRef
}

func (l *fakePublishedLister) ListPublishedActivities(ctx context.Context, project string) ([]*PublishedActivityRef, error) {
	return l.acts, nil
}

// fakeEnvLister 返回固定的环境配置（含告警配置），并实现 AlertEnvLister。
type fakeEnvLister struct {
	envs []*EnvConfigDef
}

func (l *fakeEnvLister) ListAllEnvConfigs(ctx context.Context) ([]*EnvConfigDef, error) {
	return l.envs, nil
}

func (l *fakeEnvLister) ListAlertEnvs(ctx context.Context) ([]*EnvConfigDef, error) {
	var out []*EnvConfigDef
	for _, e := range l.envs {
		if e != nil && e.AlertConfig.AlertEnabled() {
			out = append(out, e)
		}
	}
	return out, nil
}

// alertEnv 构造一个环境配置（enabled 决定是否开启告警）。
func alertEnv(project, env string, enabled bool, ac *EnvAlertConfig) *EnvConfigDef {
	if ac == nil {
		ac = &EnvAlertConfig{}
	}
	ac.Enabled = enabled
	if ac.Channel == "" {
		ac.Channel = AlertChannelFeishu
	}
	return &EnvConfigDef{Project: project, EnvName: env, AlertConfig: ac}
}

// newTestCollector 构造用于离线巡检测试的收集器。
func newTestCollector(envs []*EnvConfigDef, acts []*PublishedActivityRef) *ActivityCollector {
	return NewActivityCollectorWithAlert(nil, nil, &fakeEnvLister{envs: envs}, &fakePublishedLister{acts: acts})
}

// TestCheckOfflineActivities 覆盖离线告警状态机：
// 首次离线告警 → 恢复通知 → 未开启告警的环境不告警 → 从未上报过心跳的不告警。
func TestCheckOfflineActivities(t *testing.T) {
	sender := &captureAlertSender{ch: make(chan string, 8)}
	SetAlertSender(sender)
	defer SetAlertSender(nil)

	envs := []*EnvConfigDef{
		alertEnv("demo", "prod", true, nil),
		alertEnv("demo", "test", false, nil), // 测试环境：关闭告警
	}
	acts := []*PublishedActivityRef{{Project: "demo", ActNamespace: "ns1", ActName: "actA"}}
	c := newTestCollector(envs, acts)
	c.refreshMonitored()

	// 监控集合应包含 prod（开启告警），不含 test（关闭告警）
	prodKey := cacheKey("demo", "prod", "ns1", "actA")
	testKey := cacheKey("demo", "test", "ns1", "actA")
	c.monMu.RLock()
	_, hasProd := c.monitored[prodKey]
	_, hasTest := c.monitored[testKey]
	c.monMu.RUnlock()
	if !hasProd {
		t.Fatalf("prod 环境开启告警，应在监控集合内")
	}
	if hasTest {
		t.Fatalf("test 环境未开启告警，不应在监控集合内")
	}

	now := time.Now()
	// 1) 曾经在线但心跳中断（10 分钟前）→ 应触发离线告警
	c.hbLast[prodKey] = now.Add(-10 * time.Minute).Unix()
	c.hbLast[testKey] = now.Add(-10 * time.Minute).Unix()
	c.checkOfflineActivities()

	select {
	case msg := <-sender.ch:
		if !strings.Contains(msg, "[工作流告警] Activity 离线") {
			t.Fatalf("期望离线告警，实际: %s", msg)
		}
		if !strings.Contains(msg, "actA") || !strings.Contains(msg, "prod") {
			t.Fatalf("告警内容应包含 activity 与环境: %s", msg)
		}
		if strings.Contains(msg, "test") {
			t.Fatalf("未开启告警的 test 环境不应出现在告警中: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到离线告警")
	}
	if _, ok := c.offline[prodKey]; !ok {
		t.Fatalf("离线状态未记录")
	}

	// 2) 心跳恢复 → 应发送恢复通知并清除离线状态
	c.hbLast[prodKey] = now.Unix()
	c.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		if !strings.Contains(msg, "Activity 已重新上线") {
			t.Fatalf("期望恢复通知，实际: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到恢复通知")
	}
	if _, ok := c.offline[prodKey]; ok {
		t.Fatalf("恢复后应清除离线状态")
	}

	// 3) 从未上报过心跳的 activity（不在 hbLast 中）：
	// 该测试环境没有 Redis 监听任务（hasRedis=false），故不告警。
	// 「有监听任务 + 超过宽限期」的从未心跳场景由 TestNeverOnlineAlert 覆盖。
	c2 := newTestCollector(envs, acts)
	c2.refreshMonitored()
	c2.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		t.Fatalf("从未上报心跳的 activity 不应告警: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestOfflineAlertRemind 验证持续离线时按间隔重复提醒。
func TestOfflineAlertRemind(t *testing.T) {
	sender := &captureAlertSender{ch: make(chan string, 8)}
	SetAlertSender(sender)
	defer SetAlertSender(nil)

	envs := []*EnvConfigDef{alertEnv("demo", "prod", true, nil)}
	acts := []*PublishedActivityRef{{Project: "demo", ActNamespace: "ns1", ActName: "actB"}}
	c := newTestCollector(envs, acts)
	c.refreshMonitored()
	key := cacheKey("demo", "prod", "ns1", "actB")

	now := time.Now()
	c.hbLast[key] = now.Add(-10 * time.Minute).Unix()
	c.checkOfflineActivities() // 首次告警
	select {
	case <-sender.ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到首次离线告警")
	}

	// 未到重复提醒间隔（默认 30 分钟）→ 不再发送
	c.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		t.Fatalf("未到提醒间隔不应重复告警: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}

	// 手动把上次告警时间提前 1 小时 → 应再次提醒
	c.offlineMu.Lock()
	c.offline[key].lastAlertAt = now.Add(-time.Hour)
	c.offlineMu.Unlock()
	c.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		if !strings.Contains(msg, "持续离线") {
			t.Fatalf("期望持续离线提醒，实际: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到持续离线提醒")
	}
}

// TestNeverOnlineAlert 覆盖「已发布上线但从未收到心跳」的场景：
// 服务重启后原本就离线的 activity 在 hbLast 里没有记录，必须仍然告警（用户反馈的真实问题）。
// 同时验证两个防误报前提：环境无 Redis 监听任务不告警、纳入宽限期内不告警。
func TestNeverOnlineAlert(t *testing.T) {
	sender := &captureAlertSender{ch: make(chan string, 8)}
	SetAlertSender(sender)
	defer SetAlertSender(nil)

	acts := []*PublishedActivityRef{{Project: "demo", ActNamespace: "ns1", ActName: "actD"}}

	// 1) 环境有 Redis 监听任务 + 已超过纳入宽限期 → 应告警（hbLast 中无任何记录）
	envs := []*EnvConfigDef{alertEnv("demo", "prod", true, nil)}
	c := newTestCollector(envs, acts)
	// 模拟该环境存在监听任务（否则判定为「采集不到心跳」而不告警）
	c.tasks[taskKey("demo", "prod")] = &redisTask{project: "demo", env: "prod"}
	c.refreshMonitored()
	key := cacheKey("demo", "prod", "ns1", "actD")
	// 把纳入时间提前，越过 offlineStartupGrace 宽限期
	c.monMu.Lock()
	c.monAddedAt[key] = time.Now().Add(-offlineStartupGrace - time.Minute)
	c.monMu.Unlock()

	c.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		if !strings.Contains(msg, "[工作流告警] Activity 离线") {
			t.Fatalf("期望离线告警，实际: %s", msg)
		}
		if !strings.Contains(msg, "从未上报心跳") {
			t.Fatalf("应标注「从未上报心跳」以区分「曾在线后掉线」，实际: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("从未收到心跳的已发布 activity 应告警")
	}

	// 2) 心跳恢复 → 发送恢复通知
	c.hbLast[key] = time.Now().Unix()
	c.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		if !strings.Contains(msg, "Activity 已重新上线") {
			t.Fatalf("期望恢复通知，实际: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("未收到恢复通知")
	}

	// 3) 环境【没有】Redis 监听任务 → 采集不到心跳，不能判定离线（防误报）
	c2 := newTestCollector(envs, acts)
	c2.refreshMonitored() // tasks 为空 → hasRedis=false
	c2.monMu.Lock()
	c2.monAddedAt[key] = time.Now().Add(-offlineStartupGrace - time.Minute)
	c2.monMu.Unlock()
	c2.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		t.Fatalf("环境无 Redis 监听任务时不应告警（采集不到心跳）: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}

	// 4) 宽限期内（刚纳入监控）→ 不告警，避免服务刚启动时一次性误报全部
	c3 := newTestCollector(envs, acts)
	c3.tasks[taskKey("demo", "prod")] = &redisTask{project: "demo", env: "prod"}
	c3.refreshMonitored() // 纳入时间 = 现在，仍在宽限期内
	c3.checkOfflineActivities()
	select {
	case msg := <-sender.ch:
		t.Fatalf("纳入宽限期内不应告警: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestParseNamespaceSafe 覆盖 parseNamespace 的段数不足场景：
// 历史上判断 len>=2 却直接取 parts[2]，两段 namespace 会 index out of range panic，
// 而该函数在心跳扫描协程里调用，panic 会直接杀掉进程 → hbLast 恒为空。
func TestParseNamespaceSafe(t *testing.T) {
	cases := []struct{ ns, project, env string }{
		{"workflow/demo/prod", "demo", "prod"},
		{"workflow/demo/", "demo", ""},     // env 为空：三段
		{"workflow/demo", "demo", ""},      // 仅两段：不得 panic
		{"demo", "", ""},                   // 仅一段：不得 panic
		{"", "", ""},                       // 空串：不得 panic
	}
	for _, c := range cases {
		p, e := parseNamespace(c.ns)
		if p != c.project || e != c.env {
			t.Fatalf("parseNamespace(%q) = (%q,%q), want (%q,%q)", c.ns, p, e, c.project, c.env)
		}
	}
}

// TestEnvAlertSettingOverride 验证环境级阈值 / 提醒间隔优先于全局配置。
func TestEnvAlertSettingOverride(t *testing.T) {
	threshold := 120
	remind := 0 // 显式 0 = 只提醒一次
	setting := buildEnvAlertSetting(&EnvAlertConfig{
		Enabled:       true,
		Channel:       AlertChannelFeishu,
		Webhook:       "https://example.com/hook",
		ThresholdSec:  &threshold,
		RemindMinutes: &remind,
	})
	if setting.threshold != 120*time.Second {
		t.Fatalf("环境阈值应生效，实际: %s", setting.threshold)
	}
	if setting.remind != 0 {
		t.Fatalf("环境提醒间隔为 0（只提醒一次），实际: %s", setting.remind)
	}
	if setting.webhook != "https://example.com/hook" {
		t.Fatalf("环境 webhook 未生效: %s", setting.webhook)
	}

	// 未配置的项回落全局
	fallback := buildEnvAlertSetting(&EnvAlertConfig{Enabled: true})
	if fallback.threshold <= 0 {
		t.Fatalf("未配置阈值应回落全局: %s", fallback.threshold)
	}
	if fallback.webhook != "" {
		t.Fatalf("未配置 webhook 应为空（用全局发送器）")
	}
}

// TestRefreshMonitoredClearsOfflineState 环境关闭告警后，其离线状态应被清理（不再告警）。
func TestRefreshMonitoredClearsOfflineState(t *testing.T) {
	envs := []*EnvConfigDef{alertEnv("demo", "prod", true, nil)}
	acts := []*PublishedActivityRef{{Project: "demo", ActNamespace: "ns1", ActName: "actC"}}
	c := newTestCollector(envs, acts)
	c.refreshMonitored()
	key := cacheKey("demo", "prod", "ns1", "actC")
	c.hbLast[key] = time.Now().Add(-time.Hour).Unix()
	c.offline[key] = &activityOfflineState{offlineAt: time.Now(), lastAlertAt: time.Now(), lastSeenAt: time.Now()}

	// 关闭该环境告警后刷新监控集合
	c.lister = &fakeEnvLister{envs: []*EnvConfigDef{alertEnv("demo", "prod", false, nil)}}
	c.refreshMonitored()
	if _, ok := c.offline[key]; ok {
		t.Fatalf("环境关闭告警后应清除离线状态")
	}
	c.monMu.RLock()
	_, stillMonitored := c.monitored[key]
	c.monMu.RUnlock()
	if stillMonitored {
		t.Fatalf("环境关闭告警后不应继续监控")
	}
}
