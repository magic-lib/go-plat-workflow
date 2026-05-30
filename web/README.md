# 工作流配置生成器

> 前端 Vue 单文件 + 后端 Go + MySQL 持久化

## 项目结构

```
workflow-system/
├── frontend/
│   └── index.html      # Vue 3 单文件，无需构建，直接打开
└── backend/
    ├── main.go         # Go HTTP 服务（标准库，零框架依赖）
    ├── go.mod
    └── schema.sql      # 建表参考 SQL
```

## 快速启动

### 1. 准备 MySQL

```sql
-- 在 MySQL 中执行
CREATE DATABASE IF NOT EXISTS workflow_db CHARACTER SET utf8mb4;
```

### 2. 修改数据库配置

编辑 `backend/main.go` 顶部常量：

```go
const (
    DBHost     = "127.0.0.1"
    DBPort     = "3306"
    DBUser     = "root"
    DBPassword = "your_password"   // ← 改成你的密码
    DBName     = "workflow_db"
    ServerPort = ":8080"
)
```

### 3. 启动后端

```bash
cd backend
go mod tidy          # 拉取 go-sql-driver/mysql 依赖
go run main.go
# 输出：服务启动，监听 :8080
```

### 4. 打开前端

直接用浏览器打开 `frontend/index.html` 即可（file:// 协议即可使用）。

## API 说明

| 方法   | 路径                    | 说明         |
|--------|-------------------------|--------------|
| GET    | /api/workflows          | 获取列表     |
| POST   | /api/workflows          | 保存工作流   |
| GET    | /api/workflows/:id      | 获取详情     |
| PUT    | /api/workflows/:id      | 更新         |
| DELETE | /api/workflows/:id      | 删除         |
| GET    | /health                 | 健康检查     |

### 请求/响应格式

**POST /api/workflows**

```json
{
  "name": "测试工作流",
  "yaml_content": "name: 测试工作流\n...",
  "config": "{\"variables\":[...],\"activities\":[...],\"steps\":[...],\"responses\":[...]}"
}
```

**响应格式**

```json
{ "code": 0, "message": "success", "data": { "id": 1 } }
```

## 功能特性

- 可视化配置：变量、Activities、步骤、响应，全部支持增删改
- 实时 YAML 预览（语法高亮）
- 一键复制 YAML
- 保存到 MySQL（同时存储 YAML 文本和配置状态）
- 加载已保存的工作流（完整还原编辑器状态）
- 删除工作流
