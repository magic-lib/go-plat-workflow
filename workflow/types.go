// Package workflow 提供基于 rulego 规则链的持久化、组装和执行能力。
//
// 用户可以先注册可复用的节点配置和子链 DSL 到数据库，
// 然后按业务需要选取组件并定义连接关系，
// 系统自动生成完整的 rulego RuleChain JSON DSL 并执行。
package workflow

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// 错误定义
// ============================================================

var (
	// ErrNodeNotFound 节点未找到
	ErrNodeNotFound = fmt.Errorf("workflow: node not found")
	// ErrSubChainNotFound 子链未找到
	ErrSubChainNotFound = fmt.Errorf("workflow: sub chain not found")
	// ErrRootChainNotFound 根链未找到
	ErrRootChainNotFound = fmt.Errorf("workflow: root chain not found")
	// ErrProjectNotFound 项目未找到
	ErrProjectNotFound = fmt.Errorf("workflow: project not found")
	// ErrDSLBuildFailed DSL 组装失败
	ErrDSLBuildFailed = fmt.Errorf("workflow: dsl build failed")
	// ErrEngineNotLoaded 引擎未加载
	ErrEngineNotLoaded = fmt.Errorf("workflow: engine not loaded")
	// ErrExecutionFailed 流程执行失败
	ErrExecutionFailed = fmt.Errorf("workflow: execution failed")
	// ErrReleaseNotFound 发布版本未找到
	ErrReleaseNotFound = fmt.Errorf("workflow: release not found")
	// ErrReleaseInUse 当前生产版本不允许删除
	ErrReleaseInUse = fmt.Errorf("workflow: current release in use, cannot delete")
	// ErrActivityNotFound activity 未找到
	ErrActivityNotFound = fmt.Errorf("workflow: activity not found")
)

// ============================================================
// Activity 大类型（Kind）
// ============================================================

const (
	// ActivityKindRedis 默认类型：通过依赖 Redis 的 MQ 远程监听方式访问 activity。
	ActivityKindRedis = "redis"
	// ActivityKindHTTP HTTP 直连类型：根据 HTTPConfig 直接发起 HTTP 请求访问 activity。
	ActivityKindHTTP = "http"
)

// ActivityHTTPConfig HTTP 类型 activity 的调用配置。
// 序列化为 ActivityDef.HTTPConfig 字符串存储。
type ActivityHTTPConfig struct {
	// Method HTTP 方法，如 GET/POST/PUT/DELETE，默认 POST
	Method string `json:"method,omitempty"`
	// URL 请求地址，支持 {{key}} 占位符，由测试入参或环境变量替换
	URL string `json:"url,omitempty"`
	// Headers 自定义请求头（key-value）
	Headers map[string]string `json:"headers,omitempty"`
	// BodyTemplate 请求体模板（支持 {{key}} 占位符），GET 等无 body 方法可留空
	BodyTemplate string `json:"body_template,omitempty"`
}

// ============================================================
// 核心数据定义
// ============================================================

// ProjectDef 项目定义。用于多项目隔离管理。
type ProjectDef struct {
	// Project 项目唯一标识
	Project string `json:"project"`
	// Name 项目名称
	Name string `json:"name"`
	// Description 项目描述
	Description string `json:"description,omitempty"`
	// Status 状态：1=启用，0=禁用
	Status int8 `json:"status"`
	// CreatedBy 项目创建者用户名；普通用户在已有项目列表中仅展示自己创建的项目。
	CreatedBy string `json:"created_by,omitempty"`
	// SecretKeys 项目密钥列表（支持多个，每个含备注），仅用于页面保存（写入），
	// 通过 GetByID/List 查询时不返回，对外配置查询接口以任一匹配密钥鉴权。
	SecretKeys []SecretKeyItem `json:"secret_keys,omitempty"`
	// SecretKey 已废弃：单个项目密钥，请使用 SecretKeys。保留仅为向后兼容，不再使用。
	SecretKey string `json:"secret_key,omitempty"`
}

// SecretKeyItem 项目密钥项，支持一个项目配置多个密钥，并为每个密钥附加备注，
// 便于为不同账户分配不同密钥访问对外配置查询接口。
type SecretKeyItem struct {
	// Key 密钥明文（仅在保存/查询密钥接口中传递，列表接口不返回）
	Key string `json:"key"`
	// Remark 密钥备注，标识该密钥用途或所属账户
	Remark string `json:"remark,omitempty"`
}

// ProjectConfigSummary RootChain 概要信息（对外配置查询接口返回，避免泄露 DSL 详情）。
type ProjectConfigSummary struct {
	// ChainID 根链 ID
	ChainID string `json:"chain_id"`
	// Name 根链名称
	Name string `json:"name"`
	// Description 根链描述
	Description string `json:"description,omitempty"`
}

// ProjectConfigResponse 对外配置查询接口的返回结构。
// 传入正确的项目密钥后，返回该项目下的配置信息与可执行的 RootChains 列表，
// 但不包含 DSL 等敏感内容，避免整体配置泄密。
type ProjectConfigResponse struct {
	// Project 项目唯一标识
	Project string `json:"project"`
	// Name 项目名称
	Name string `json:"name"`
	// Description 项目描述
	Description string `json:"description,omitempty"`
	// EnvConfigs 项目下的环境配置列表（含环境变量/Redis/MySQL 连接信息）
	EnvConfigs []*EnvConfigDef `json:"env_configs,omitempty"`
	// RootChains 可执行的 RootChains 列表（仅概要信息）
	RootChains []*ProjectConfigSummary `json:"root_chains,omitempty"`
}

// NodeDef 节点定义，对应 rulego RuleNode。
// 每个 NodeDef 代表一个可复用的规则节点配置，可以被多个规则链引用。
type NodeDef struct {
	// Project 所属项目，用于多项目隔离
	Project string `json:"project"`
	// NodeID 节点唯一标识（同一 project 内唯一），对应 rulego RuleNode.Id
	NodeID string `json:"node_id"`
	// Namespace 命名空间，用于按模块归类 Node 并支持前端过滤查询（仅作标签，无路由作用）
	Namespace string `json:"namespace,omitempty"`
	// Name 节点名称
	Name string `json:"name"`
	// Type 节点类型，如 jsFilter/jsTransform/log/restApiCall 等
	Type string `json:"type"`
	// DebugMode 调试模式
	DebugMode bool `json:"debug_mode"`
	// Configuration 节点配置，JSON 格式
	Configuration json.RawMessage `json:"configuration"`
	// AdditionalInfo 附加信息，JSON 格式
	AdditionalInfo json.RawMessage `json:"additional_info,omitempty"`
	// Params 节点参数定义（入参），JSON 数组格式，如 [{"key":"url","label":"请求URL","type":"string","required":true}]
	Params json.RawMessage `json:"params,omitempty"`
	// Outputs 节点返回值定义（出参），描述执行该节点后能得到哪些值，供下游节点引用。
	// 格式如 [{"key":"name","label":"用户名","type":"string","description":"查询出的用户名称"}]
	// 注意：不使用 omitempty，否则空数组/空值会被 json 序列化跳过，导致保存后读取不到、编辑回显丢失。
	Outputs json.RawMessage `json:"outputs"`
	// Kind 节点分类：condition（查询获取类）/ action（策略执行类）
	// condition 类节点可在连接中选择 True/False 分支，action 类仅 Success/Failure
	Kind string `json:"kind,omitempty"`
	// Category 节点分类，便于组织和筛选
	Category string `json:"category,omitempty"`
	// Tags 标签多选，便于组织和筛选（与 activities 一致）
	Tags []string `json:"tags,omitempty"`
	// Description 节点描述
	Description string `json:"description,omitempty"`
	// Status 状态：1=启用，0=禁用
	Status int8 `json:"status"`
	// Version 版本号
	Version string `json:"version,omitempty"`
	// NodeHeartbeats node 内各 activity 的心跳存活信息（管理端收集器实时计算，不入库）。
	// 仅当列表接口传入 env 参数时填充，用于前端在 node 名称后显示聚合心跳图标，
	// 以及悬停 pop 层展示每个 activity 的心跳状态。
	NodeHeartbeats []*NodeActivityHeartbeat `json:"node_heartbeats,omitempty"`
}

// NodeActivityHeartbeat node 内单个 activity 的心跳存活信息（带 activity 标识）。
type NodeActivityHeartbeat struct {
	// ActNamespace 活动命名空间
	ActNamespace string `json:"act_namespace"`
	// ActName 活动名称
	ActName string `json:"act_name"`
	// Ratio 最近 1 分钟心跳存活比例 [0,1]
	Ratio float64 `json:"ratio"`
	// Count 最近 1 分钟实际心跳次数
	Count int `json:"count"`
}

// SubChainDef 子规则链定义。
// 存储完整的 rulego SubChain DSL JSON，可被 RootChain 或其他子链通过 flow 节点引用。
// SubChain 的编排方式与 RootChain 一致（节点 + 连接），区别在于 ChainID 自动生成（如 F000012），
// 且 SubChain 不允许再嵌套引用其他 SubChain（避免循环引用和加载顺序问题）。
type SubChainDef struct {
	// Project 所属项目，用于多项目隔离
	Project string `json:"project"`
	// ChainID 子链唯一标识（同一 project 内唯一）
	ChainID string `json:"chain_id"`
	// Name 子链名称
	Name string `json:"name"`
	// Description 子链描述
	Description string `json:"description,omitempty"`
	// DSLJSON 完整的 rulego SubChainRuleChain DSL JSON
	DSLJSON string `json:"dsl_json"`
	// Status 状态：1=启用，0=禁用
	Status int8 `json:"status"`
	// SubChainIDs 引用的子链 ID 列表（逗号分隔），用于溯源（仅当本子链本身被嵌套编排时）
	SubChainIDs string `json:"sub_chain_ids,omitempty"`
	// NodeIDs 引用的节点 ID 列表（逗号分隔），用于溯源
	NodeIDs string `json:"node_ids,omitempty"`
	// ConnectionsData 连接关系 JSON（[]ConnectionDef），方便查看/修改
	ConnectionsData string `json:"connections_data,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖值 JSON，保存后可在下次编辑时恢复
	NodeParamOverrides string `json:"node_param_overrides,omitempty"`
}

// RootChainDef 根规则链定义。
// 存储组装后的 rulego RootChain DSL JSON，可直接用于创建 rulego 引擎。
type RootChainDef struct {
	// Project 所属项目，用于多项目隔离
	Project string `json:"project"`
	// ChainID 根链唯一标识（系统生成，形如 R000001，基于自增主键）
	ChainID string `json:"chain_id"`
	// ChainKey 根链业务键（全局唯一），用户可自定义，与 ChainID 均可用于调用主链
	ChainKey string `json:"chain_key,omitempty"`
	// Name 根链名称
	Name string `json:"name"`
	// Description 根链描述
	Description string `json:"description,omitempty"`
	// DSLJSON 组装后的完整 rulego RootChain DSL JSON
	DSLJSON string `json:"dsl_json"`
	// Status 状态：1=启用，0=禁用
	Status int8 `json:"status"`
	// NodeIDs 引用的节点 ID 列表（逗号分隔），用于溯源
	NodeIDs string `json:"node_ids,omitempty"`
	// SubChainIDs 引用的子链 ID 列表（逗号分隔），用于溯源
	SubChainIDs string `json:"sub_chain_ids,omitempty"`
	// ConnectionsData 连接关系 JSON（[]ConnectionDef），方便查看/修改
	ConnectionsData string `json:"connections_data,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖值 JSON，保存后可在下次编辑时恢复
	NodeParamOverrides string `json:"node_param_overrides,omitempty"`
}

// RootChainReleaseDef 根链发布版本定义。
// 每次发布将根链草稿快照为一个不可变版本，生产环境使用 IsCurrent=true 的版本。
// 发布记录只增不改，保留完整历史以便回滚。
type RootChainReleaseDef struct {
	// Project 所属项目
	Project string `json:"project"`
	// ChainID 根链 ID
	ChainID string `json:"chain_id"`
	// Version 发布版本号（同一 project+chain_id 内从 1 递增）
	Version int `json:"version"`
	// Name 根链名称（发布时快照）
	Name string `json:"name"`
	// Description 根链描述（发布时快照）
	Description string `json:"description,omitempty"`
	// DSLJSON 发布时的完整 rulego RootChain DSL JSON 快照
	DSLJSON string `json:"dsl_json"`
	// NodeIDs 引用的节点 ID 列表（逗号分隔），用于溯源
	NodeIDs string `json:"node_ids,omitempty"`
	// SubChainIDs 引用的子链 ID 列表（逗号分隔），用于溯源
	SubChainIDs string `json:"sub_chain_ids,omitempty"`
	// ConnectionsData 连接关系 JSON（[]ConnectionDef）
	ConnectionsData string `json:"connections_data,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖值 JSON
	NodeParamOverrides string `json:"node_param_overrides,omitempty"`
	// IsCurrent 是否为生产环境当前使用的版本
	IsCurrent bool `json:"is_current"`
	// PublishedAt 发布时间
	PublishedAt time.Time `json:"published_at"`
}

// TestCaseDef 测试用例定义。
// 保存一次"测试 Execute Tab 时输入的完整配置"，挂载在某个 RootChain 或 SubChain 上，
// 方便后续直接复用、调试，无需每次重新填写账号/参数。
type TestCaseDef struct {
	// Project 所属项目
	Project string `json:"project"`
	// CaseID 测试用例唯一标识（如 T000001，同 project 内唯一）
	CaseID string `json:"case_id"`
	// OwnerID 测试用例所属对象 ID（RootChain 的 ChainID 或 SubChain 的 ChainID）
	OwnerID string `json:"owner_id"`
	// OwnerType 所属对象类型：root / sub
	OwnerType string `json:"owner_type"`
	// Name 测试用例名称（用户可读，如"正常登录-测试账号A"）
	Name string `json:"name"`
	// ChainID 执行时使用的 chain_id
	ChainID string `json:"chain_id"`
	// ChainName 执行时使用的 chain_name
	ChainName string `json:"chain_name"`
	// NodeIDs 节点 ID 列表（逗号分隔字符串存储）
	NodeIDs string `json:"node_ids,omitempty"`
	// SubChainIDs 子链 ID 列表（逗号分隔字符串存储）
	SubChainIDs string `json:"sub_chain_ids,omitempty"`
	// ConnectionsData 连接关系 JSON（[]ConnectionDef）
	ConnectionsData string `json:"connections_data,omitempty"`
	// Payload 测试输入 payload（JSON 字符串）
	Payload string `json:"payload,omitempty"`
	// DebugMode 是否开启调试模式
	DebugMode bool `json:"debug_mode,omitempty"`
	// UseRelease 是否使用已发布版本执行
	UseRelease bool `json:"use_release,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖值 JSON
	NodeParamOverrides string `json:"node_param_overrides,omitempty"`
	// LastResult 最近一次执行结果的快照（JSON 字符串），便于直接查看历史测试结果
	LastResult string `json:"last_result,omitempty"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// ============================================================
// 环境配置
// ============================================================

// EnvVar 环境变量（key-value 形式）。
// 用于在不同环境下注入不同的环境级配置（如账号、token、开关）。
type EnvVar struct {
	// Key 变量名
	Key string `json:"key"`
	// Value 变量值
	Value string `json:"value"`
	// Desc 变量说明（可选）
	Desc string `json:"desc,omitempty"`
}

// RedisConfig 不同环境下的 Redis 连接配置。
type RedisConfig struct {
	// Addr Redis 地址，如 127.0.0.1:6379
	Addr string `json:"addr"`
	// Password 密码（可选）
	Password string `json:"password,omitempty"`
	// DB 数据库序号
	DB int `json:"db"`
	// Username 用户名（Redis 6+ ACL，可选）
	Username string `json:"username,omitempty"`
}

// MySQLConfig 不同环境下的 MySQL 连接配置。
type MySQLConfig struct {
	// Host 主机
	Host string `json:"host"`
	// Port 端口
	Port int `json:"port"`
	// User 用户名
	User string `json:"user"`
	// Password 密码
	Password string `json:"password"`
	// DBName 数据库名
	DBName string `json:"db_name"`
	// DSN 完整 DSN（若填写则优先使用，忽略上面的拆分字段）
	DSN string `json:"dsn,omitempty"`
	// Params 额外连接参数，如 charset=utf8mb4&parseTime=True（可选）
	Params string `json:"params,omitempty"`
}

// EnvConfigDef 环境配置定义。
// 挂在某个 project 下，按 EnvName 区分多个环境（如 dev/test/prod）。
// 每个环境可配置环境变量、Redis、MySQL 连接信息，供后续使用。
type EnvConfigDef struct {
	// Project 所属项目
	Project string `json:"project"`
	// EnvName 环境名（同一 project 内唯一），如 dev/test/prod
	EnvName string `json:"env_name"`
	// Description 环境描述
	Description string `json:"description,omitempty"`
	// EnvVars 环境变量列表（key-value）
	EnvVars []EnvVar `json:"env_vars,omitempty"`
	// RedisConfig Redis 连接配置
	RedisConfig *RedisConfig `json:"redis_config,omitempty"`
	// MySQLConfig MySQL 连接配置
	MySQLConfig *MySQLConfig `json:"mysql_config,omitempty"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// EnvConfigStore 环境配置仓储接口。
type EnvConfigStore interface {
	// Upsert 创建或更新环境配置（按 project + env_name 冲突时更新）
	Upsert(ctx context.Context, def *EnvConfigDef) error
	// GetByName 按项目+环境名查询
	GetByName(ctx context.Context, project, envName string) (*EnvConfigDef, error)
	// ListByProject 列出指定项目下所有环境配置
	ListByProject(ctx context.Context, project string) ([]*EnvConfigDef, error)
	// Delete 删除环境配置（按 project + env_name）
	Delete(ctx context.Context, project, envName string) error
}

// ConnectionDef 连接关系定义，对应 rulego NodeConnection。
type ConnectionDef struct {
	// FromID 源节点 ID
	FromID string `json:"from_id"`
	// ToID 目标节点 ID
	ToID string `json:"to_id"`
	// Type 连接类型，如 "Success"/"Failure"/"True"/"False"
	Type string `json:"type"`
	// Label 连接标签（可选）
	Label string `json:"label,omitempty"`
}

// BuildRequest 组装根链请求。
type BuildRequest struct {
	// Project 所属项目
	Project string `json:"project"`
	// ChainID 目标根链 ID（可选，不填则自动生成形如 R000001）
	ChainID string `json:"chain_id,omitempty"`
	// ChainKey 业务键（可选）：全局唯一，不填则自动生成；与 ChainID 均可用于调用主链
	ChainKey string `json:"chain_key,omitempty"`
	// ChainName 目标根链名称
	ChainName string `json:"chain_name"`
	// Description 根链描述
	Description string `json:"description,omitempty"`
	// NodeIDs 需要包含的节点 ID 列表
	NodeIDs []string `json:"node_ids"`
	// SubChainIDs 需要包含的子链 ID 列表
	SubChainIDs []string `json:"sub_chain_ids,omitempty"`
	// Connections 节点间的连接关系
	Connections []ConnectionDef `json:"connections"`
	// DebugMode 是否开启调试模式
	DebugMode bool `json:"debug_mode,omitempty"`
	// Configuration 根链级别配置
	Configuration json.RawMessage `json:"configuration,omitempty"`
	// FirstNodeIndex 第一个节点的索引，默认为 0
	FirstNodeIndex int `json:"first_node_index,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖，key=nodeID, value=覆盖的配置键值对
	// 例: {"N000001": {"url": "https://real-api.example.com", "timeout": 30}}
	NodeParamOverrides map[string]map[string]interface{} `json:"node_param_overrides,omitempty"`
}

// BuildSubChainRequest 编排方式组装子链请求。
// 子链的编排方式与 RootChain 完全一致（节点 + 连接 + 可选嵌套子链引用），
// 区别仅在于 ChainID 为空时自动生成（如 F000012）。
type BuildSubChainRequest struct {
	// Project 所属项目
	Project string `json:"project"`
	// ChainID 子链 ID（可选，创建时为空则自动生成，如 F000012）
	ChainID string `json:"chain_id,omitempty"`
	// ChainName 子链名称
	ChainName string `json:"chain_name"`
	// Description 子链描述
	Description string `json:"description,omitempty"`
	// NodeIDs 需要包含的节点 ID 列表
	NodeIDs []string `json:"node_ids"`
	// SubChainIDs 需要嵌套引用的子链 ID 列表（可选）
	SubChainIDs []string `json:"sub_chain_ids,omitempty"`
	// Connections 节点/子链间的连接关系
	Connections []ConnectionDef `json:"connections"`
	// DebugMode 是否开启调试模式
	DebugMode bool `json:"debug_mode,omitempty"`
	// Configuration 子链级别配置
	Configuration json.RawMessage `json:"configuration,omitempty"`
	// FirstNodeIndex 第一个节点的索引，默认为 0
	FirstNodeIndex int `json:"first_node_index,omitempty"`
	// NodeParamOverrides 节点实例参数覆盖，key=nodeID, value=覆盖的配置键值对
	NodeParamOverrides map[string]map[string]interface{} `json:"node_param_overrides,omitempty"`
}

// ============================================================
// Store 仓储接口
// ============================================================

// ProjectStore 项目仓储接口。
type ProjectStore interface {
	// Create 创建项目
	Create(ctx context.Context, def *ProjectDef) error
	// GetByID 按项目 ID 查询
	GetByID(ctx context.Context, project string) (*ProjectDef, error)
	// List 列出所有启用的项目
	List(ctx context.Context) ([]*ProjectDef, error)
	// Update 更新项目
	Update(ctx context.Context, def *ProjectDef) error
	// Delete 软删除项目
	Delete(ctx context.Context, project string) error
	// GetSecrets 按项目 ID 查询所有密钥明文（用于对外配置查询接口鉴权）
	GetSecrets(ctx context.Context, project string) ([]string, error)
}

// NodeStore 节点仓储接口。
type NodeStore interface {
	// Create 创建节点
	Create(ctx context.Context, def *NodeDef) error
	// BatchUpsert 批量 upsert 节点（按 project + node_id 冲突时更新全部字段）
	BatchUpsert(ctx context.Context, defs []*NodeDef) error
	// GetByID 按项目+节点 ID 查询
	GetByID(ctx context.Context, project, nodeID string) (*NodeDef, error)
	// ListByIDs 按项目+节点 ID 列表批量查询
	ListByIDs(ctx context.Context, project string, nodeIDs []string) ([]*NodeDef, error)
	// List 列出指定项目下的节点。onlyEnabled=true 时仅返回启用状态（用于编排组装），false 时返回全部（含禁用，用于管理列表）。
	List(ctx context.Context, project, namespace string, onlyEnabled bool) ([]*NodeDef, error)
	// Update 更新节点
	Update(ctx context.Context, def *NodeDef) error
	// Delete 软删除节点（按 project + nodeID）
	Delete(ctx context.Context, project, nodeID string) error
}

// SubChainStore 子规则链仓储接口。
type SubChainStore interface {
	// Create 创建子链
	Create(ctx context.Context, def *SubChainDef) error
	// BatchUpsert 批量 upsert 子链（按 project + chain_id 冲突时更新全部字段）
	BatchUpsert(ctx context.Context, defs []*SubChainDef) error
	// GetByID 按项目+子链 ID 查询
	GetByID(ctx context.Context, project, chainID string) (*SubChainDef, error)
	// ListByIDs 按项目+子链 ID 列表批量查询
	ListByIDs(ctx context.Context, project string, chainIDs []string) ([]*SubChainDef, error)
	// List 列出指定项目下的子链。onlyEnabled=true 时仅返回启用状态（用于编排组装），false 时返回全部（含禁用，用于管理列表）。
	List(ctx context.Context, project string, onlyEnabled bool) ([]*SubChainDef, error)
	// Update 更新子链
	Update(ctx context.Context, def *SubChainDef) error
	// Delete 软删除子链（按 project + chainID）
	Delete(ctx context.Context, project, chainID string) error
}

// RootChainStore 根规则链仓储接口。
type RootChainStore interface {
	// Create 创建根链
	Create(ctx context.Context, def *RootChainDef) error
	// GetByID 按项目+根链 ID 查询
	GetByID(ctx context.Context, chainID string) (*RootChainDef, error)
	// GetByKey 按项目+ChainKey 查询（project 与 chain_key 联合唯一）
	GetByKey(ctx context.Context, project, chainKey string) (*RootChainDef, error)
	// List 列出指定项目下所有启用的根链
	List(ctx context.Context, project string) ([]*RootChainDef, error)
	// Update 更新根链
	Update(ctx context.Context, def *RootChainDef) error
	// Delete 物理删除根链（按 project + chainID，历史由发布快照保留）
	Delete(ctx context.Context, project, chainID string) error
}

// TestCaseStore 测试用例仓储接口。
type TestCaseStore interface {
	// Create 创建测试用例
	Create(ctx context.Context, def *TestCaseDef) error
	// Update 更新测试用例
	Update(ctx context.Context, def *TestCaseDef) error
	// GetByID 按项目+CaseID 查询
	GetByID(ctx context.Context, project, caseID string) (*TestCaseDef, error)
	// ListByOwner 列出指定 owner（root/sub + chainID）下所有测试用例
	ListByOwner(ctx context.Context, project, ownerID string) ([]*TestCaseDef, error)
	// Delete 删除测试用例（按 project + caseID）
	Delete(ctx context.Context, project, caseID string) error
	// NextCaseID 生成下一个测试用例自动 ID（如 T000001）
	NextCaseID(ctx context.Context, project string) (string, error)
}

// RootChainReleaseStore 根链发布版本仓储接口。
type RootChainReleaseStore interface {
	// Create 创建发布版本
	Create(ctx context.Context, def *RootChainReleaseDef) error
	// ListByChain 列出指定根链的所有发布版本（按版本号倒序）
	ListByChain(ctx context.Context, project, chainID string) ([]*RootChainReleaseDef, error)
	// GetByVersion 查询指定发布版本
	GetByVersion(ctx context.Context, project, chainID string, version int) (*RootChainReleaseDef, error)
	// GetCurrent 查询生产环境当前使用的发布版本
	GetCurrent(ctx context.Context, project, chainID string) (*RootChainReleaseDef, error)
	// ListCurrentByProject 列出指定项目下所有根链的当前发布版本
	ListCurrentByProject(ctx context.Context, project string) ([]*RootChainReleaseDef, error)
	// MaxVersion 查询指定根链的最大发布版本号（无记录返回 0）
	MaxVersion(ctx context.Context, project, chainID string) (int, error)
	// SetCurrent 将指定版本设为当前生产版本（同链其他版本置为非当前）
	SetCurrent(ctx context.Context, project, chainID string, version int) error
}

// ActivityDef activity 模板定义。
// 存储可复用的 activity 配置模板，按 project 隔离。
// 配置使用 github.com/magic-lib/go-plat-utils/plugins/activity 中的 Activity 结构体字段，
// 在 Node 编辑器中 type=activity 时可通过下拉选择 activity 一键填充配置。
type ActivityDef struct {
	// Project 所属项目，用于多项目隔离
	Project string `json:"project"`
	// ActivityID activity 唯一标识（同一 project 内唯一），格式 A+6 位数字，如 A000001
	ActivityID string `json:"activity_id"`
	// Name 展示名称，方便在 UI 中识别和选择
	Name string `json:"name"`
	// ActNamespace 活动命名空间，对应 action.RegisterActor 的 Namespace
	ActNamespace string `json:"act_namespace"`
	// ActName 活动名称，对应 action.RegisterActor 的 Action 名称
	ActName string `json:"act_name"`
	// ActivityType 活动类型（可选），优先级高于通过 namespace+name 推导的类型
	ActivityType string `json:"activity_type,omitempty"`
	// Kind activity 大类型，决定测试时执行的访问方式。取值见 ActivityKind* 常量。
	// 当前默认 ActivityKindRedis（依赖 Redis 的 MQ 远程监听方式）；ActivityKindHTTP 走 HTTP 直连。
	Kind string `json:"kind,omitempty"`
	// HTTPConfig 当 Kind=ActivityKindHTTP 时生效，描述 HTTP 调用所需的配置（method/url/headers/body 模板）。
	HTTPConfig string `json:"http_config,omitempty"`
	// Arguments 默认参数绑定配置，JSON 格式（[]*param.BindConfig 数组）
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// ArgTemplate 参数模板字符串，支持 {{id.responses.field}} 语法
	ArgTemplate string `json:"arg_template,omitempty"`
	// Responses 自定义返回值映射，JSON 格式
	Responses json.RawMessage `json:"responses,omitempty"`
	// ReturnValues 返回值设置列表（每一项从活动返回值 map 中提取指定 key 重命名输出）。
	// 若某一项 Key 为空，表示该返回值项返回活动返回的所有内容。
	ReturnValues json.RawMessage `json:"return_values,omitempty"`
	// Status 状态：1=启用，0=禁用
	Status int8 `json:"status"`
	// Description 描述
	Description string `json:"description,omitempty"`
	// Tags 标签列表，用于列表筛选与分组
	Tags []string `json:"tags,omitempty"`
	// TestStatus 测试状态汇总：success(至少一条成功) / failed(有记录但都失败) / none(无记录)
	TestStatus string `json:"test_status,omitempty"`
	// Heartbeat 心跳存活信息（管理端收集器实时计算，不入库）
	Heartbeat *ActivityHeartbeatInfo `json:"heartbeat,omitempty"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// ActivityReturnValue 返回值设置项。
// 用于从活动返回值（map）中提取指定 key 重命名为返回值名称输出。
//   - Name 返回值名称（必填），作为下游引用时的标识。
//   - Key 从返回值 map 中取值的 key。为空表示返回活动返回的所有内容；
//     非空表示从返回值 map 中取对应 key 的值。
//     可选值来源于测试成功记录的返回结果 map 中所有的 key。
type ActivityReturnValue struct {
	// Name 返回值名称（必填）
	Name string `json:"name"`
	// Key 取值 key；空表示返回全部内容
	Key string `json:"key,omitempty"`
	// Type 返回值类型，用于输出时做格式转换。常用取值：string/number/boolean/array/object/json。
	// 空表示不做转换（保持原值）。
	Type string `json:"type,omitempty"`
}

type ActivityLogValue struct {
	RootChainID string         `json:"root_chain_id"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	Attributes  map[string]any `json:"attributes"`
}

// ActivityStore activity 模板仓储接口。
type ActivityStore interface {
	// Create 创建 activity
	Create(ctx context.Context, def *ActivityDef) error
	// GetByID 按项目+activity ID 查询
	GetByID(ctx context.Context, project, activityID string) (*ActivityDef, error)
	// GetByNamespaceName 按项目 + act_namespace + act_name 查询 activity 模板（唯一键定位）。
	// 节点编排里的 activity.Activity.Id 不能稳定对应 ActivityDef.ID，故使用 namespace+name 反查。
	GetByNamespaceName(ctx context.Context, project, actNamespace, actName string) (*ActivityDef, error)
	// List 列出指定项目下所有启用的 activity
	List(ctx context.Context, project string) ([]*ActivityDef, error)
	// Update 更新 activity
	Update(ctx context.Context, def *ActivityDef) error
	// Delete 软删除 activity（按 project + activityID）
	Delete(ctx context.Context, project, activityID string) error
	// NextActivityID 生成下一个 activity 自动 ID（如 A000001）
	NextActivityID(ctx context.Context) (string, error)
}

// ============================================================
// Node 内多 Activity 编排（方案 A：仅在单个 activity 类型节点内部执行）
// ============================================================

// Activity 执行模式：并行 / 串行。
// 用于 activity 类型节点内部编排多个 activity 时，声明每个 activity 相对前一个节点的执行方式。
const (
	// ActivityModeParallel 并行：与前一个已完成的节点（或上游）同时发起执行。
	// 首个元素前的并行元素会与首个元素一起并发执行。
	ActivityModeParallel = "parallel"
	// ActivityModeSerial 串行：等前一个元素执行完成后再顺序执行。
	ActivityModeSerial = "serial"
)

// ActivityItemDef 单个 activity 编排项，对应 activity 类型节点内部编排列表（二维 stages）中的一项。
// 与 ActivityDef 的区别：这里只描述「在哪个 namespace 下执行哪个 activity」以及参数绑定，
// 参数/返回值映射等复用所选 activity 模板（ActNamespace+ActName 唯一确定一个 activity 模板）。
//
// 二维编排模型（与 commnode.ActivityNode 的 stages 一致）：
//   - 外层数组（stages）按序「串行」执行；
//   - 内层数组（同一 stage）内的多个 activity「并行」执行；
//   - 后续 stage / 后续 activity 的参数可引用前面 activity 的返回值（{{id.responses}}），
//     或引用节点参数定义（{{node_param_key}}）。
type ActivityItemDef struct {
	// ActNamespace 活动命名空间（对应 action.RegisterActor 的 Namespace）。
	ActNamespace string `json:"act_namespace"`
	// ActName 活动名称（对应 action.RegisterActor 的 Action 名称）。
	ActName string `json:"act_name"`
	// Mode 已废弃：并行/串行改由二维 stages 结构表达（外层串行、内层并行），前端不再使用此字段。
	// Deprecated: 使用 NodeActivityConfig.Stages 二维数组替代。
	Mode string `json:"mode,omitempty"`
	// Name 展示名称（可选，方便 UI 识别，不入库逻辑使用）。
	Name string `json:"name,omitempty"`
	// Args 参数绑定：key 为 activity 的参数名（对应 arguments 中的 key），value 描述该参数的值来源。
	// 可选；为空时 worker 使用 activity 模板中的默认参数值。
	Args map[string]ActivityArgBind `json:"args,omitempty"`
}

// ActivityArgBind 单个 activity 参数的取值绑定。
//   - Source="value"：使用 Value 作为固定值（可填空字符串表示用 activity 模板默认值）。
//   - Source="ref_act"：引用本节点中「排在当前 activity 之前」的某个 activity 的输出（返回值或参数值），
//     Ref 为引用路径，形如 {{id.responses}} / {{id.responses.field}} / {{id.arguments.key}}（id 为前序 activity 实例 id）。
//   - Source="ref_node"：引用本节点参数定义中的某个 key，Ref 形如 {{node_param_key}}。
type ActivityArgBind struct {
	// Source 值来源："value"（固定值，默认）、"ref_act"（引用前序 activity）、"ref_node"（引用本节点参数）。
	Source string `json:"source,omitempty"`
	// Value 固定值来源时填写。
	Value string `json:"value,omitempty"`
	// Ref 引用来源时的引用路径（见 ActivityArgBind 注释中的语法）。
	Ref string `json:"ref,omitempty"`
}

// ActivityArgSourceValue 固定值来源标识。
const ActivityArgSourceValue = "value"

// ActivityArgSourceRefAct 引用前序 activity 输出（返回值/参数值）来源标识。
const ActivityArgSourceRefAct = "ref_act"

// ActivityArgSourceRefNode 引用本节点参数定义来源标识。
const ActivityArgSourceRefNode = "ref_node"

// ActivityRefPathPrefix 引用路径语法：以 "{{" 开头、以 "}}" 结尾，中间为引用表达式。
// 引用表达式语法（与 commnode.ActivityNode 内部执行模型一致）：
//   - {{id}}                前序 activity 实例 id 的返回值整体（等价 {{id.responses}}）
//   - {{id.responses}}      前序 activity 返回值整体
//   - {{id.responses.field}} 前序 activity 返回值内的某个字段
//   - {{id.arguments.key}}  前序 activity 被传入的参数值内的某个字段
//   - {{node_param_key}}   本节点参数定义中的某个 key（运行时作为消息 data 的顶层 key）
//
// 内部执行结构（公共约定 / 运行时数据模型）：
// 每个 activity 执行后，其运行时上下文以「实例 id」为键暴露在共享变量空间 paramx.ParamCtx 中，
// 结构等价于：
//
//	map[instanceId]struct{
//	    responses map[string]any // 该 activity 的返回值（整体可用 {{id.responses}}，单字段用 {{id.responses.field}}）
//	    arguments map[string]any // 调用该 activity 时传入的参数值（单字段用 {{id.arguments.key}}）
//	}

// NodeActivityConfig activity 类型节点的内部编排配置。
// 约定存放在 NodeDef.Configuration.node_config.stages（二维数组）中。
//   - 外层数组：串行执行的「阶段（stage）」；
//   - 内层数组：同一阶段内「并行」执行的 activity 列表。
//
// 兼容旧结构：
//   - 旧扁平数组 configuration.activities（含 mode）在读取时由前端转换为 stages；
//   - 当 stages 为空且 node_config.act_namespace/act_name 存在时，worker 回退执行单个 activity。
//
// 引用语义（与 commnode.ActivityNode 一致，详见 ActivityRefPathPrefix 注释）：
//   - 后续 activity 参数可引用前面 activity 的返回值或参数值，路径形如 {{id.responses.field}} / {{id.arguments.key}}；
//   - 也可引用本节点参数定义，路径形如 {{node_param_key}}。
//   - 重要约束：同一 stage 内的 activity 是「并行同时」执行的，彼此不可保证先后，故**不能互相引用**；
//     只有「串行排在前面」的 stage 中的 activity 才可被引用（前端 prevActivitiesBefore 已按此约束过滤）。
type NodeActivityConfig struct {
	// Stages 二维编排：外层串行（阶段），内层并行（同阶段 activity）。
	// 每个元素结构同 ActivityItemDef（含 act_namespace/act_name/id/arguments）。
	Stages [][]ActivityItemDef `json:"stages"`
}

// ============================================================
// Activity 测试记录
// ============================================================

// ActivityTestRecordDef activity 测试记录定义。
// 每次"测试单个 activity"时，保存本次测试传入的参数、环境变量以及返回结果，方便后期查看。
type ActivityTestRecordDef struct {
	// Project 所属项目
	Project string `json:"project"`
	// RecordID 测试记录唯一标识（如 T000001，同 project 内唯一）
	RecordID string `json:"record_id"`
	// ActivityID 被测 activity ID
	ActivityID string `json:"activity_id"`
	// ActivityName 被测 activity 名称（快照，便于查看）
	ActivityName string `json:"activity_name"`
	// EnvName 测试使用的环境名（决定 Redis 等依赖配置）
	EnvName string `json:"env_name,omitempty"`
	// InputParams 测试时传入的参数（JSON 字符串）
	InputParams string `json:"input_params"`
	// EnvVars 测试时使用的环境变量（JSON 字符串，可选，配合 env_name 使用）
	EnvVars string `json:"env_vars,omitempty"`
	// Status 测试结果：success / fail
	Status string `json:"status"`
	// Result activity 返回结果（JSON 字符串）
	Result string `json:"result,omitempty"`
	// ErrorMsg 失败时的错误信息
	ErrorMsg string `json:"error_msg,omitempty"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
}

// ActivityTestRecordStore activity 测试记录仓储接口。
type ActivityTestRecordStore interface {
	// Create 创建测试记录
	Create(ctx context.Context, def *ActivityTestRecordDef) error
	// GetByID 按项目+记录 ID 查询
	GetByID(ctx context.Context, project, recordID string) (*ActivityTestRecordDef, error)
	// ListByActivity 列出指定 activity 下所有测试记录（按时间倒序）
	ListByActivity(ctx context.Context, project, activityID string) ([]*ActivityTestRecordDef, error)
	// Delete 删除测试记录（按 project + record_id）
	Delete(ctx context.Context, project, recordID string) error
	// NextRecordID 生成下一个测试记录自动 ID（如 T000001）
	NextRecordID(ctx context.Context, project string) (string, error)
	// ListTestStatusByActivities 批量查询每个 activity 的测试状态汇总（可按 env 限定环境）。
	// 返回值：activity_id -> "success"(至少一条成功) / "failed"(有记录但无成功) / "none"(无记录)。
	ListTestStatusByActivities(ctx context.Context, project string, env string, activityIDs []string) (map[string]string, error)
}

// ============================================================
// Activity 执行日志
// ============================================================

// ActivityHeartbeatInfo 心跳存活信息，用于前端进度条展示。
type ActivityHeartbeatInfo struct {
	// Ratio 最近 1 分钟心跳存活比例 [0,1]
	Ratio float64 `json:"ratio"`
	// Count 最近 1 分钟实际心跳次数
	Count int `json:"count"`
}

// ActivityLogDef activity 执行日志定义。
// 由管理端收集器消费 worker 上报的日志后落库，前端可按 activity 查看与检索。
type ActivityLogDef struct {
	// ID 自增主键
	ID uint `json:"id"`
	// Project 所属项目
	Project string `json:"project"`
	// Env 环境名
	Env string `json:"env"`
	// ActNamespace 活动命名空间
	ActNamespace string `json:"act_namespace"`
	// ActName 活动名称
	ActName string `json:"act_name"`
	// EventID 消息/链路 ID
	EventID string `json:"event_id,omitempty"`
	// Level 日志级别：info / error
	Level string `json:"level"`
	// Timestamp 日志时间戳（unix 秒）
	Timestamp int64 `json:"timestamp"`
	// DurationMs 执行耗时（毫秒）
	DurationMs int64 `json:"duration_ms,omitempty"`
	// Payload 请求入参（任意 JSON，由 worker 上报，可能为对象）
	Payload json.RawMessage `json:"payload,omitempty"`
	// Result 执行结果（任意 JSON，由 worker 上报，可能为对象）
	Result json.RawMessage `json:"result,omitempty"`
	// ErrorMsg 错误信息（兼容 worker 上报的 "error" 字段名）
	ErrorMsg string `json:"error_msg,omitempty"`
	// Error 兼容 worker 上报的 "error" 字段名（采集时合并到 ErrorMsg）
	Error string `json:"error,omitempty"`
	// RootChainID 根链 ID，用于按根链聚合查询整个流程的日志
	RootChainID string `json:"root_chain_id,omitempty"`
	// TraceID 链路 ID（分布式追踪），用于串联一次完整执行的所有日志
	TraceID string `json:"trace_id,omitempty"`
	// SpanID 跨度 ID（分布式追踪），标识本次 activity 执行的跨度
	SpanID string `json:"span_id,omitempty"`
	// NodeSpanID 所属 node 的 span ID（由 ActivityNode 在执行时注入），
	// 用于关联 wf_node_logs（node 侧使用相同语义的 span_id），实现 node -> activity 的层级关联。
	NodeSpanID string `json:"node_span_id,omitempty"`
	// Attributes 动态附加属性（JSON 字符串），用于存储与本次执行相关的自定义键值对，
	// 例如业务维度标记、扩展上下文等，便于后续按属性检索或展示。
	Attributes string `json:"attributes,omitempty"`
	// CreatedAt 落库时间
	CreatedAt time.Time `json:"created_at"`
}

// ActivityLogFilter 日志检索过滤条件。
type ActivityLogFilter struct {
	// Level 级别精确匹配（info/error）
	Level string `json:"level,omitempty"`
	// ActNamespace 命名空间精确匹配
	ActNamespace string `json:"act_namespace,omitempty"`
	// ActName 活动名称精确匹配
	ActName string `json:"act_name,omitempty"`
	// EventID 链路 ID 精确匹配
	EventID string `json:"event_id,omitempty"`
	// Env 环境名精确匹配（如 dev/test/prod）
	Env string `json:"env,omitempty"`
	// RootChainID 根链 ID 精确匹配
	RootChainID string `json:"root_chain_id,omitempty"`
	// TraceID 链路 ID 精确匹配
	TraceID string `json:"trace_id,omitempty"`
	// SpanID 跨度 ID 精确匹配
	SpanID string `json:"span_id,omitempty"`
	// NodeSpanID 所属 node 的 span ID 精确匹配，用于按 node 关联查询其下所有 activity
	NodeSpanID string `json:"node_span_id,omitempty"`
	// Keyword 关键词模糊匹配 payload/result/error_msg
	Keyword string `json:"keyword,omitempty"`
	// Start 时间范围起点（unix 秒，包含）
	Start int64 `json:"start,omitempty"`
	// End 时间范围终点（unix 秒，包含）
	End int64 `json:"end,omitempty"`
	// Limit 返回条数上限（默认 50，最大 1000）
	Limit int `json:"limit,omitempty"`
	// Offset 分页偏移量（与 Limit 配合）
	Offset int `json:"offset,omitempty"`
}

// ActivityLogPage 日志分页查询结果，包含当前页数据与总条数。
type ActivityLogPage struct {
	List     []*ActivityLogDef `json:"list"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// ActivityLogStore activity 执行日志仓储接口。
type ActivityLogStore interface {
	// Create 创建一条执行日志
	Create(ctx context.Context, def *ActivityLogDef) error
	// ListByActivity 列出指定 activity 的执行日志，支持按字段过滤、关键词搜索与分页（按时间倒序）
	// actName 为活动唯一标识（act_name），用于限定当前 activity 范围
	ListByActivity(ctx context.Context, project, actName string, filter *ActivityLogFilter) ([]*ActivityLogDef, int64, error)
	// DeleteByActivity 删除指定 activity 的全部日志
	DeleteByActivity(ctx context.Context, project, actName string) error
}

// NodeLogDef node 运行日志：记录单个 node 执行时的入参与返回值，便于在前端查看每个 node 的运行情况。
// 由管理端 collector 从 redis（workflow:node:log:<namespace>）消费后落库 wf_node_logs。
type NodeLogDef struct {
	ID         uint   `json:"id"`
	Project    string `json:"project"`
	Env        string `json:"env"`
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name"`
	EventID    string `json:"event_id"`
	Level      string `json:"level"`
	Timestamp  int64  `json:"timestamp"`
	DurationMs int64  `json:"duration_ms"`
	// Payload node 执行的全部入参（全局参数 + 本节点参数），JSON 字符串
	Payload json.RawMessage `json:"payload"`
	// Arguments node 执行的输入参数（取自 payload.arguments），JSON 字符串，便于按 node 直接查看入参
	Arguments json.RawMessage `json:"arguments"`
	// Result node 执行后的返回值（按本节点 responses 配置提取），JSON 字符串
	Result string `json:"result"`
	// ErrorMsg 执行错误信息（成功为空）
	ErrorMsg string `json:"error_msg"`
	// Error 兼容 worker/组件上报时使用的 "error" 字段名
	Error string `json:"error"`
	// TraceID 本次执行的分布式追踪 ID，用于回查本次执行产生的 activity 日志（wf_activity_logs.trace_id）
	TraceID     string `json:"trace_id"`
	RootChainID string `json:"root_chain_id"`
	SpanID      string `json:"span_id"`
	// RelationType 该 node 执行完成后往下传递的连接类型（relationType），
	// 对应 rulego 的 TellSuccess/TellFailure/TellNext 等，取值如 Success/Failure/True/False 或自定义字符串。
	// 用于回查本次 node 走了哪条分支链路。
	RelationType string    `json:"relation_type"`
	CreatedAt    time.Time `json:"created_at"`
}

// NodeLogFilter node 运行日志查询过滤条件。
type NodeLogFilter struct {
	// Level 级别精确匹配（info/error）
	Level string `json:"level,omitempty"`
	// NodeID node 精确匹配
	NodeID string `json:"node_id,omitempty"`
	// NodeName node 名称模糊匹配
	NodeName string `json:"node_name,omitempty"`
	// Env 环境名精确匹配
	Env string `json:"env,omitempty"`
	// TraceID 链路 ID 精确匹配（用于跨 node/activity 回查）
	TraceID string `json:"trace_id,omitempty"`
	// Keyword 关键词模糊匹配 payload/result/error_msg
	Keyword string `json:"keyword,omitempty"`
	// Limit 每页条数
	Limit int `json:"limit,omitempty"`
	// Offset 偏移量
	Offset int `json:"offset,omitempty"`
}

// NodeLogStore node 运行日志仓储接口。
type NodeLogStore interface {
	// Create 创建一条 node 运行日志
	Create(ctx context.Context, def *NodeLogDef) error
	// ListByNode 列出指定 node 的运行日志，按时间倒序，支持分页
	ListByNode(ctx context.Context, project, nodeID string, limit, offset int) ([]*NodeLogDef, int64, error)
	// ListByFilter 按条件全局查询 node 运行日志（按时间倒序），支持分页
	ListByFilter(ctx context.Context, project string, f *NodeLogFilter) ([]*NodeLogDef, int64, error)
}

// ============================================================
// DSLBuilder 接口
// ============================================================

// DSLBuilder 规则链 DSL 组装器接口。
type DSLBuilder interface {
	// Build 根据 BuildRequest 组装生成 RootChainDSL JSON
	Build(ctx context.Context, req *BuildRequest) (*RootChainDef, error)
}

// ============================================================
// JSON 值类型（用于 GORM 序列化 Configuration/AdditionalInfo）
// ============================================================

// JSONMap JSON map 类型，实现 sql.Scanner 和 driver.Valuer 接口。
type JSONMap map[string]interface{}

// Scan 实现 sql.Scanner 接口。
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan JSONMap: expected []byte, got %T", value)
	}
	return json.Unmarshal(bytes, j)
}

// Value 实现 driver.Valuer 接口。
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
