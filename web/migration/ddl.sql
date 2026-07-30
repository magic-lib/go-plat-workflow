-- ============================================================
-- 工作流可视化配置系统 - 数据库表结构（Migration 模板）
-- 数据库：MySQL 8.0+
-- 字符集：utf8mb4
-- 注意：此文件为 Go embed 模板，{{.TablePrefix}} 在运行时替换为表前缀
--       原始 DDL 参考 web/workflow.sql
-- ============================================================

-- ------------------------------------------------------------
-- 1. Activity 注册表（核心表）
-- ------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{.TablePrefix}}activities
(
    activity_id           BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    acvivity_name          VARCHAR(50)          DEFAULT '' COMMENT '前端展示名称',
    action_namespace             VARCHAR(50) NOT NULL COMMENT '动作命名空间',
    action_name                  VARCHAR(50) NOT NULL COMMENT '动作名称',
    description           VARCHAR(150)         DEFAULT '' COMMENT '功能描述',
    source_type           INT         NOT NULL DEFAULT 1 COMMENT '来源类型：1=builtin，2=http，3=grpc',

    -- ===== HTTP 动态类型（source_type=2）=====
    http_url              VARCHAR(100)         DEFAULT '' COMMENT 'HTTP 调用 URL',
    http_method           VARCHAR(50)          DEFAULT 'POST' COMMENT 'HTTP 方法',
    http_headers          JSON                 DEFAULT NULL COMMENT '默认请求头',
    http_body_template    TEXT                 DEFAULT NULL COMMENT '请求体模板',
    http_timeout          INT                  DEFAULT 30 COMMENT 'HTTP 超时秒数',

    -- ===== gRPC 动态类型（source_type=3）=====
    grpc_addr             VARCHAR(100)         DEFAULT '' COMMENT 'gRPC 服务地址',
    grpc_service          VARCHAR(100)         DEFAULT '' COMMENT 'gRPC 完整服务名',
    grpc_method           VARCHAR(50)          DEFAULT '' COMMENT 'gRPC 方法名',
    grpc_insecure         BOOLEAN              DEFAULT TRUE COMMENT '是否使用明文连接',
    grpc_timeout          INT                  DEFAULT 30 COMMENT 'gRPC 调用超时秒数',
    grpc_metadata         JSON                 DEFAULT NULL COMMENT 'gRPC metadata',
    grpc_request_template TEXT                 DEFAULT NULL COMMENT 'gRPC 请求体模板',

    -- ===== 输入输出参数 Schema 定义 =====
    input_schema          JSON                 DEFAULT NULL COMMENT '输入参数 Schema 定义',
    output_schema         JSON                 DEFAULT NULL COMMENT '输出参数 Schema 定义',

    -- ===== 默认控制参数 =====
    default_timeout       INT                  DEFAULT 0 COMMENT '默认超时秒数',
    default_ctx_cacheable BOOLEAN              DEFAULT FALSE COMMENT '默认是否启用流程级缓存',

    create_time           DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time           DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_namespace_name (namespace, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Activity 注册表';


-- ============================================================
-- 2. Activity 实例表
-- ============================================================
CREATE TABLE IF NOT EXISTS {{.TablePrefix}}activity_instances
(
    instance_id     BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    activity_id     BIGINT      NOT NULL COMMENT '关联 Activity 定义',
    instance_name   VARCHAR(50) NOT NULL COMMENT '实例名称',
    description     VARCHAR(150)         DEFAULT '' COMMENT '实例描述',

    config_json     JSON        NOT NULL COMMENT '运行时配置（http_url / grpc_addr 等）',
    default_args    JSON                 DEFAULT NULL COMMENT '实例输入参数的默认值',
    output_template JSON                 DEFAULT NULL COMMENT '实例输出映射模板',

    status          INT         NOT NULL DEFAULT 1 COMMENT '状态：0=禁用，1=启用',

    create_time     DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time     DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX           idx_activity_id (activity_id),
    INDEX           idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Activity 实例表';


-- ------------------------------------------------------------
-- 3. 工作流表
-- ------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{.TablePrefix}}workflows
(
    wf_id        BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    workflow_key VARCHAR(50) NOT NULL DEFAULT '' COMMENT '工作流唯一标识',
    name         VARCHAR(50) NOT NULL COMMENT '工作流名称',
    description  VARCHAR(255)         DEFAULT '' COMMENT '工作流描述',

    variables    JSON                 DEFAULT NULL COMMENT '入口变量列表',
    yaml_content TEXT        NOT NULL COMMENT 'YAML 格式的工作流定义',
    json_config  JSON        NOT NULL COMMENT 'JSON 配置：{nodes, edges, variables, responses}',
    responses    JSON                 DEFAULT NULL COMMENT '最终响应字段映射',

    status       INT                  DEFAULT 1 COMMENT '状态：1=草稿，2=已激活，3=已归档',
    version      INT                  DEFAULT 1 COMMENT '版本号',

    create_time  DATETIME             DEFAULT CURRENT_TIMESTAMP,
    update_time  DATETIME             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_workflow_key (workflow_key),
    INDEX        idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工作流定义表';


-- ------------------------------------------------------------
-- 4. 工作流执行记录表
-- ------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{.TablePrefix}}workflow_executions
(
    exec_id       BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    wf_id         BIGINT      NOT NULL COMMENT '关联工作流',
    execution_key VARCHAR(50) NOT NULL COMMENT '执行唯一标识（UUID）',

    trigger_type  INT                  DEFAULT 1 COMMENT '触发方式：1=手动，2=接口，3=定时',
    trigger_user  VARCHAR(50)          DEFAULT '' COMMENT '触发用户',

    input_params  JSON COMMENT '输入参数',
    output_result JSON COMMENT '最终输出结果',

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
-- 5. 执行日志表
-- ------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{.TablePrefix}}execution_logs
(
    log_id        BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    exec_id       BIGINT      NOT NULL COMMENT '关联执行记录',
    activity_id   BIGINT               DEFAULT NULL COMMENT '关联 Activity 定义',
    instance_id   BIGINT               DEFAULT NULL COMMENT '关联 Activity 实例',
    step_id       VARCHAR(50) NOT NULL DEFAULT '' COMMENT '步骤ID',
    node_type     INT         NOT NULL DEFAULT 0 COMMENT '节点类型：1=开始，2=条件，3=策略，4=结束',

    status        INT     NOT NULL COMMENT '执行状态：1=执行中，2=成功，3=失败，4=跳过，5=超时',
    input_params  JSON                 DEFAULT NULL COMMENT '输入参数',
    output_result JSON                 DEFAULT NULL COMMENT '输出结果',
    error_message TEXT                 DEFAULT NULL COMMENT '错误信息',
    duration_ms   INT                  DEFAULT 0 COMMENT '耗时（毫秒）',

    create_time   DATETIME             DEFAULT CURRENT_TIMESTAMP,

    INDEX         idx_exec_id (exec_id),
    INDEX         idx_activity_id (activity_id),
    INDEX         idx_instance_id (instance_id),
    INDEX         idx_step_id (step_id),
    INDEX         idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='执行日志表';
