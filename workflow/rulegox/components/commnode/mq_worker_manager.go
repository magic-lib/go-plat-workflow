package commnode

import (
	"fmt"
	"strings"
	"sync"

	"github.com/magic-lib/go-plat-utils/conn"
	"github.com/magic-lib/go-plat-workflow/workflow/rulegox"
)

// mqWorkerKey 缓存键：由 project + env + redis 配置共同决定。
// 注意：conn.Connect 含有 Extend map[string]any 字段，map 在 Go 中不可比较，
// 因此不能直接把 conn.Connect 作为 map 的 key（会编译报错）。
// 这里将 redis 连接的标识性字段拼成字符串作为 key 的一部分。
type mqWorkerKey struct {
	project string
	env     string
	redis   string
}

// redisConnKey 将 redis 连接配置归一化为可比较的字符串。
// 仅取决定「连到哪个 redis」的字段：驱动/协议/地址/端口/账号/密码/库。
// Extend 为扩展项且不可比较，不参与 key 计算。
func redisConnKey(c *conn.Connect) string {
	if c == nil {
		return ""
	}
	return strings.Join([]string{
		c.Driver,
		c.Protocol,
		c.Host,
		c.Port,
		c.Username,
		c.Password,
		c.Database,
	}, "|")
}

// mqWorkerManager 进程内 *rulegox.MQWorker 的复用缓存。
// 以 project + env + redis 配置为 key，避免重复创建 MQ 客户端与 redis 连接；
// 相同 key 的多次请求共享同一个 worker 实例。
//
// 注意：被缓存的 worker 由本管理器统一持有，调用方【不应】对其调用 Stop()。
// 进程退出时调用 CloseAll() 释放全部 worker 的底层连接。
type mqWorkerManager struct {
	mu      sync.Mutex
	workers map[mqWorkerKey]*rulegox.MQWorker
}

var defaultMQWorkerManager = &mqWorkerManager{
	workers: make(map[mqWorkerKey]*rulegox.MQWorker),
}

// errNilRedisConfig 表示未传入 redis 配置，无法构建缓存键与 worker。
var errNilRedisConfig = fmt.Errorf("redis config is required to get/cache MQWorker")

// MQWorkerFactory 缓存未命中时用于真正构建 worker 的工厂函数。
type MQWorkerFactory func(project, env string, redisCfg *conn.Connect) (*rulegox.MQWorker, error)

// GetMQWorker 返回与 (project, env, redisCfg) 对应的 *rulegox.MQWorker。
// 若缓存中已存在则直接复用；不存在时：
//   - factory 非 nil：调用 factory 创建、放入缓存后返回；
//   - factory 为 nil：说明调用方只想「查缓存」，此时返回 nil 与未命中错误，
//     由调用方决定回退逻辑（例如改走本地执行）。
func (m *mqWorkerManager) GetMQWorker(
	project, env string,
	redisCfg *conn.Connect,
	factory MQWorkerFactory,
) (*rulegox.MQWorker, error) {
	if redisCfg == nil {
		return nil, errNilRedisConfig
	}
	key := mqWorkerKey{
		project: project,
		env:     env,
		redis:   redisConnKey(redisCfg),
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.workers[key]; ok {
		return w, nil
	}

	if factory == nil {
		return nil, fmt.Errorf("MQWorker not cached for project=%s env=%s, and no factory provided", project, env)
	}

	w, err := factory(project, env, redisCfg)
	if err != nil {
		return nil, err
	}
	m.workers[key] = w
	return w, nil
}

// CloseAll 释放缓存中所有 worker 的底层连接（MQ 客户端与 redis 连接），
// 应在进程退出时调用。调用后缓存清空，再次 Get 会重新创建。
func (m *mqWorkerManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, w := range m.workers {
		w.Stop()
		delete(m.workers, k)
	}
}

// GetMQWorker 进程级便捷方法：复用 defaultMQWorkerManager 的缓存。
// factory 可传 nil，表示仅查询缓存（未命中时返回错误）。
func GetMQWorker(
	project, env string,
	redisCfg *conn.Connect,
	factory MQWorkerFactory,
) (*rulegox.MQWorker, error) {
	return defaultMQWorkerManager.GetMQWorker(project, env, redisCfg, factory)
}

// CloseAllMQWorkers 释放进程内全部缓存的 worker 连接（进程退出时调用）。
func CloseAllMQWorkers() {
	defaultMQWorkerManager.CloseAll()
}
