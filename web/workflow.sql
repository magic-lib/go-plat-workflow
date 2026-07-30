-- ============================================================
-- 工作流可视化配置系统 - 数据库表结构（最终版）
-- 数据库：MySQL 8.0+
-- 字符集：utf8mb4
-- ============================================================

-- ------------------------------------------------------------
-- 1. Activity 注册表（核心表）
--    注册所有可用的 Activity 类型，支持三种来源：
--    source_type=1 → 内置 Go 实现（action.Register 内存注册）
--    source_type=2 → 动态 HTTP 调用（通过配置 URL/方法 提供 Action 能力）
--    source_type=3 → 动态 gRPC 调用（通过配置 Addr/Service/Method 提供 Action 能力）
-- ------------------------------------------------------------
CREATE TABLE activities
(
    activity_id           BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    namespace             VARCHAR(50) NOT NULL COMMENT '命名空间，如 order / user / log / http / grpc',
    name                  VARCHAR(50) NOT NULL COMMENT 'Activity 名称，如 SetOrderInfo / CallAPI / CallGRPC',
    display_name          VARCHAR(50)          DEFAULT '' COMMENT '前端展示名称',
    description           VARCHAR(255)         DEFAULT '' COMMENT '功能描述',
    source_type           INT         NOT NULL DEFAULT 1 COMMENT '来源类型：1=内置Go实现(builtin)，2=动态HTTP调用(http)，3=动态gRPC调用(grpc)',

    -- ===== HTTP 动态类型（source_type=2）需填写以下字段 =====
    http_url              VARCHAR(100)         DEFAULT '' COMMENT 'HTTP 调用 URL（source_type=2 时必填）',
    http_method           VARCHAR(50)          DEFAULT 'POST' COMMENT 'HTTP 方法：GET/POST/PUT/DELETE',
    http_headers          JSON                 DEFAULT NULL COMMENT '默认请求头 {"Content-Type":"application/json",...}',
    http_body_template    TEXT                 DEFAULT NULL COMMENT '请求体模板（支持 {{变量}} 表达式）',
    http_timeout          INT                  DEFAULT 30 COMMENT 'HTTP 超时秒数',

    -- ===== gRPC 动态类型（source_type=3）需填写以下字段 =====
    grpc_addr             VARCHAR(100)         DEFAULT '' COMMENT 'gRPC 服务地址，如 localhost:50051（source_type=3 时必填）',
    grpc_service          VARCHAR(100)         DEFAULT '' COMMENT 'gRPC 完整服务名，如 order.OrderService',
    grpc_method           VARCHAR(50)          DEFAULT '' COMMENT 'gRPC 方法名，如 GetOrderInfo',
    grpc_insecure         BOOLEAN              DEFAULT TRUE COMMENT '是否使用明文连接（无 TLS）',
    grpc_timeout          INT                  DEFAULT 30 COMMENT 'gRPC 调用超时秒数',
    grpc_metadata         JSON                 DEFAULT NULL COMMENT 'gRPC metadata（相当于 HTTP Header），如 {"authorization":"Bearer xxx"}',
    grpc_request_template TEXT                 DEFAULT NULL COMMENT 'gRPC 请求体模板（JSON，支持 {{变量}} 表达式）',

    -- ===== 输入输出参数 Schema 定义（JSON）=====
    -- input_schema 格式（数组，定义该 Activity 接受哪些参数）：
    -- [{"key":"orderId","desc":"订单ID","type":"string","required":true,"default":"","policy":"frontend+"}]
    input_schema          JSON                 DEFAULT NULL COMMENT '输入参数 Schema 定义',
    -- output_schema 格式（数组，定义该 Activity 返回哪些字段，即 responses 的可选 key 列表）：
    -- [{"key":"orderName","desc":"订单名称","type":"string"}, ...]
    output_schema         JSON                 DEFAULT NULL COMMENT '输出参数 Schema 定义',

    -- ===== 默认控制参数 =====
    default_timeout       INT                  DEFAULT 0 COMMENT '默认超时秒数，0=不超时',
    default_ctx_cacheable BOOLEAN              DEFAULT FALSE COMMENT '默认是否启用流程级缓存',

    create_time           DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time           DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_namespace_name (namespace, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Activity 注册表（定义所有可用的 Activity 类型）';


-- ============================================================
-- 2. Activity 实例表（预配置的具体调用实例）
--    基于 activities 表中的类型，填入具体的运行参数
--    用户可提前创建、保存、复用实例，拖拽到流程中直接使用
--    config_json 结构根据 source_type 不同而变化：
--
--    source_type=1 (builtin)：
--      {}
--
--    source_type=2 (http)：
--      {"url":"https://api.example.com/order","method":"POST","headers":{"X-Token":"xxx"},"body":"{\"id\":\"{{.orderId}}\"}","timeout":30}
--
--    source_type=3 (grpc)：
--      {"addr":"localhost:50051","service":"order.OrderService","method":"GetOrder","insecure":true,"timeout":30,"metadata":{"auth":"xxx"}}
-- ============================================================
CREATE TABLE activity_instances
(
    instance_id     BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    activity_id     BIGINT      NOT NULL COMMENT '关联 Activity 定义 → activities.activity_id',
    instance_name   VARCHAR(50) NOT NULL COMMENT '实例名称，如"获取订单详情"、"查询用户积分"',
    description     VARCHAR(255)         DEFAULT '' COMMENT '实例描述',

    -- 实例级运行时配置（JSON，结构根据 activities.source_type 变化）
    config_json     JSON        NOT NULL COMMENT '运行时配置（http_url / grpc_addr 等）',

    -- 实例级输入参数默认值（JSON，支持模板变量 {{.xxx}}）
    -- [{"key":"orderId","value":"","policy":"frontend+"}]
    -- 拖入流程时，这些默认值会预填到参数面板里
    default_args    JSON                 DEFAULT NULL COMMENT '实例输入参数的默认值',

    -- 实例级输出参数模板（JSON，可选覆盖 activities 的 output_schema）
    output_template JSON                 DEFAULT NULL COMMENT '实例输出映射模板',

    -- 状态：1=enabled（启用），0=disabled（禁用）
    status          INT         NOT NULL DEFAULT 1 COMMENT '状态：0=禁用，1=启用',

    create_time     DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time     DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX           idx_activity_id (activity_id),
    INDEX           idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Activity 实例表（预配置的具体调用实例，可直接拖入流程）';

-- ------------------------------------------------------------
-- 2. 工作流表（存储完整的工作流定义）
--
--  节点类型说明（对应 ReactFlow 节点）：
--  type=1 → 开始入口节点（Start）：配置流程入口变量
--  type=2 → 条件节点（Condition）：条件判断，根据输出值走不同分支
--  type=3 → 策略节点（Strategy）：执行具体操作，关联一个 Activity
--  type=4 → 结束节点（End）：退出流程
--
--  json_config 字段存储前端 ReactFlow 的完整数据，结构如下：
--  {
--    "nodes": [ {...} ],
--    "edges": [ {...} ],
--    "variables": [ {"key":"userId","value":"","policy":""} ],
--    "responses": { "orderName": "{{step1.responses.orderName}}" }
--  }
-- ------------------------------------------------------------
CREATE TABLE workflows
(
    wf_id        BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    workflow_key VARCHAR(50) NOT NULL DEFAULT '' COMMENT '工作流唯一标识（YAML 中的 id 字段）',
    name         VARCHAR(50) NOT NULL COMMENT '工作流名称',
    description  VARCHAR(255)         DEFAULT '' COMMENT '工作流描述',

    -- 入口变量（JSON 数组，对应 YAML 的 variables 字段）
    variables    JSON                 DEFAULT NULL COMMENT '入口变量列表',

    -- 完整 YAML 内容（Go 引擎直接加载执行）
    yaml_content TEXT        NOT NULL COMMENT 'YAML 格式的工作流定义（供 Go 引擎执行）',
    -- 前端可视化数据（ReactFlow 的 nodes + edges + 完整配置）
    json_config  JSON        NOT NULL COMMENT 'JSON：{nodes, edges, variables, responses}',

    -- 最终响应配置（JSON，对应 YAML 的 responses 字段）
    responses    JSON                 DEFAULT NULL COMMENT '最终响应字段映射',

    -- 状态：1=draft（草稿），2=active（已激活），3=archived（已归档）
    status       INT                  DEFAULT 1 COMMENT '状态：1=草稿，2=已激活，3=已归档',
    version      INT                  DEFAULT 1 COMMENT '版本号，每次保存 +1',

    create_time  DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time  DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_workflow_key (workflow_key),
    INDEX        idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工作流定义表';

-- ------------------------------------------------------------
-- 3. 工作流执行记录表
--    状态：1=pending（等待中），2=running（执行中），3=success（成功），4=failed（失败），5=timeout（超时）
--    每次触发一个工作流执行时，创建一条执行记录
-- ------------------------------------------------------------
CREATE TABLE workflow_executions
(
    exec_id       BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    wf_id         BIGINT      NOT NULL COMMENT '关联工作流 → workflows.wf_id',
    execution_key VARCHAR(50) NOT NULL COMMENT '执行唯一标识（UUID）',
    -- 触发方式：1=manual（手动），2=api（接口调用），3=schedule（定时）
    trigger_type  INT                  DEFAULT 1 COMMENT '触发方式：1=手动，2=接口，3=定时',
    trigger_user  VARCHAR(50)          DEFAULT '' COMMENT '触发用户（用户名或 API Key）',

    input_params  JSON COMMENT '输入参数（覆盖 variables 的值）',
    output_result JSON COMMENT '最终输出结果',

    -- 状态：1=pending，2=running，3=success，4=failed，5=timeout
    status        INT         NOT NULL COMMENT '执行状态：1=等待中，2=执行中，3=成功，4=失败，5=超时',
    error_message TEXT COMMENT '错误信息',

    start_time    DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finish_time   DATETIME             DEFAULT NULL,

    INDEX         idx_wf_id (wf_id),
    UNIQUE KEY uk_execution_key (execution_key),
    INDEX         idx_status (status),
    INDEX         idx_start_time (start_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工作流执行记录表';

-- ------------------------------------------------------------
-- 4. 执行日志表（Activity 级明细日志）
--    一次工作流执行中的每个 Activity 执行完都会插入一条记录
--    状态：1=running（执行中），2=success（成功），3=failed（失败），4=skipped（跳过），5=timeout（超时）
-- ------------------------------------------------------------
CREATE TABLE execution_logs
(
    log_id        BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    exec_id       BIGINT      NOT NULL COMMENT '关联执行记录 → workflow_executions.exec_id',
    activity_id   BIGINT               DEFAULT NULL COMMENT '关联 Activity → activities.activity_id（null=非策略节点）',
    instance_id   BIGINT               DEFAULT NULL COMMENT '关联实例 → activity_instances.instance_id（null=非策略节点）',
    step_id       VARCHAR(50) NOT NULL DEFAULT '' COMMENT '步骤ID，如 step1 / step2',
    node_type     INT         NOT NULL DEFAULT 0 COMMENT '节点类型：1=开始，2=条件，3=策略，4=结束',

    -- 状态：1=running，2=success，3=failed，4=skipped，5=timeout
    status        INT     NOT NULL COMMENT '执行状态：1=执行中，2=成功，3=失败，4=跳过，5=超时',
    input_params  JSON                 DEFAULT NULL COMMENT '该步骤的输入参数',
    output_result JSON                 DEFAULT NULL COMMENT '该步骤的输出结果',
    error_message TEXT                 DEFAULT NULL COMMENT '错误信息',
    duration_ms   INT                  DEFAULT 0 COMMENT '耗时（毫秒）',

    create_time   DATETIME             DEFAULT CURRENT_TIMESTAMP,

    INDEX         idx_exec_id (exec_id),
    INDEX         idx_activity_id (activity_id),
    INDEX         idx_instance_id (instance_id),
    INDEX         idx_step_id (step_id),
    INDEX         idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='执行日志表（每个 Activity 执行一条记录）';
