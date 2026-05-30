-- 如果已有旧表，迁移脚本（可选）
-- ALTER TABLE workflows ADD COLUMN workflow_id VARCHAR(255) NOT NULL DEFAULT '' COMMENT '业务 ID' AFTER id;
-- ALTER TABLE workflows ADD COLUMN json_config LONGTEXT NOT NULL DEFAULT '' COMMENT '前端配置 JSON' AFTER yaml_content;
-- UPDATE workflows SET json_config = config WHERE json_config = '';

-- 建表（新环境）
CREATE DATABASE IF NOT EXISTS workflow_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE workflow_db;

CREATE TABLE IF NOT EXISTS workflows (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY           COMMENT '自增主键',
  workflow_id  VARCHAR(255) NOT NULL DEFAULT ''                  COMMENT '业务 ID（如 test_workflow）',
  name         VARCHAR(255) NOT NULL                             COMMENT '工作流名称',
  yaml_content LONGTEXT     NOT NULL                             COMMENT '生成的 YAML 内容',
  json_config  LONGTEXT     NOT NULL                             COMMENT '前端配置 JSON（用于回显编辑）',
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP   COMMENT '创建时间',
  updated_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
               ON UPDATE CURRENT_TIMESTAMP                       COMMENT '更新时间',
  INDEX idx_workflow_id (workflow_id),
  INDEX idx_name        (name),
  INDEX idx_updated     (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工作流配置表';
