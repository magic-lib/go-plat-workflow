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

	// 3) 从未上报过心跳的 activity（不在 hbLast 中）不应告警
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
