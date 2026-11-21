# 后端与 Agent 内部实现

`internal/` 按领域职责拆分包。后端 HTTP 服务由 `cmd/server` 组装，
Agent 运行时以 `analysis` 为核心，并由 `model`、`rag`、`history`
等包提供模型、检索和上下文能力。

## 调用关系

```text
cmd/server
  -> api
     -> account
     -> auth
     -> history
     -> analysis
        -> model
        -> rag

cmd/seed
  -> analysis

cmd/rag-index
  -> rag
```

## account

用户账号领域包，负责账号数据和服务层业务规则。

| 文件 | 作用 |
| --- | --- |
| `service.go` | `users` 表迁移、用户注册、密码校验登录和按 ID 查询。 |
| `service_integration_test.go` | 通过 `MYSQL_TEST_DSN` 验证真实 MySQL 下的账号服务行为。 |

## analysis

Agent 运行时和电商分析领域包，是项目最核心的目录。

| 文件 | 作用 |
| --- | --- |
| `service.go` | Agent 编排核心：创建 Run，绑定模型与工具，循环执行模型决策和工具调用，并输出最终报告。 |
| `tools.go` | 内置模式实现四个分析工具；文档模式只注册 `search_semantics` 并召回用户文档片段。 |
| `guard.go` | 只读 SQL 守卫：限制为单条 SELECT、检查表白名单、禁用危险关键字并强制限制返回行数。 |
| `catalog.go` | 内置电商指标目录和表结构，是 `search_semantics`、`get_schema` 的本地知识来源。 |
| `metric_vector.go` | 将指标描述、别名、公式和维度转换为向量文档，并把召回 ID 映射回指标目录。 |
| `models.go` | 电商业务库的 GORM 模型，对应订单、商品、渠道、地区、退款和库存表。 |
| `seed.go` | 演示业务数据生成逻辑，负责建表、清表和批量灌数。 |
| `types.go` | Agent 的状态、事件和 Run/Step/ToolCall/Artifact 数据结构。 |
| `store.go` | Agent 运行记录的持久化层，负责运行轨迹和产物的写入与聚合查询。 |
| `events.go` | 进程内事件中心，为前端 SSE 提供运行事件广播和历史回放。 |
| `dataset.go` | 用户文档知识库服务，管理元数据、向量索引、所有权隔离、内容去重和生命周期。 |
| `document_import.go` | Markdown、TXT、DOCX 解析，文本清洗、标题层级切块、段落合并和内容哈希。 |
| `document_import_test.go` | 覆盖文档解析、清洗、切块和内容哈希。 |
| `scope.go` | 把当前文档数据集封装进 `context`，让工具切换到对应向量检索范围。 |
| `guard_test.go` | 覆盖 SQL 守卫和语义指标搜索。 |

## api

HTTP 接入层，负责请求校验、鉴权、领域服务调用和响应编码。

| 文件 | 作用 |
| --- | --- |
| `server.go` | 创建 Gin 路由、中间件、健康检查，并挂载所有 API。 |
| `auth_handlers.go` | 注册、登录、当前用户接口和 JWT 鉴权中间件。 |
| `analysis_handlers.go` | Agent Run 的启动、查询、SSE 订阅和取消接口。 |
| `dataset_handlers.go` | 文档上传、知识库查询、重命名和删除接口。 |
| `server_test.go` | 使用假实现覆盖认证、健康检查、会话和数据集路由。 |

## auth

认证基础能力包。

| 文件 | 作用 |
| --- | --- |
| `auth.go` | bcrypt 密码哈希/校验和 JWT 签发/解析。 |
| `auth_test.go` | 覆盖密码、JWT、过期令牌和签名算法校验。 |

## config

运行配置包。

| 文件 | 作用 |
| --- | --- |
| `config.go` | 从环境变量读取 HTTP、MySQL、Redis、历史上下文和 JWT 配置并生成 DSN。 |
| `config_test.go` | 覆盖历史上下文窗口配置的加载和校验。 |

## history

会话历史与并发控制包，为 Agent 提供最近对话上下文。

| 文件 | 作用 |
| --- | --- |
| `models.go` | 会话、消息和 API 视图结构，映射 `chat_sessions`、`chat_messages`。 |
| `store.go` | MySQL 持久化、Redis 缓存、上下文加载、所有权校验和会话锁。 |
| `cache.go` | 缓存接口与 Redis 实现，包含 TTL 缓存和分布式锁。 |
| `store_test.go` | 覆盖标题生成、本地锁降级和 MySQL 集成测试。 |

## model

Agent 使用的对话模型适配包。

| 文件 | 作用 |
| --- | --- |
| `model.go` | 创建并复用火山方舟 Eino ChatModel，读取 `ARK_KEY`、`ARK_MODEL`。 |
| `model_test.go` | 验证默认模型和模型名环境变量覆盖。 |

## rag

检索增强生成包，负责知识库、指标描述和用户文档的向量索引与检索。

| 文件 | 作用 |
| --- | --- |
| `pipeline.go` | Ark Embedding、Milvus 索引/过滤检索、用户文档删除、集合重建和客户端缓存。 |
| `pipeline_test.go` | 覆盖切块、Embedding API 类型、检索参数和 collection 向量维度。 |
