# Sentoris Protocol v1.0.0 架构总结文档

## 目录
- [1. 整体架构](#1-整体架构)
- [2. 核心组件详解](#2-核心组件详解)
- [3. 数据流与处理流程](#3-数据流与处理流程)
- [4. 关键功能模块](#4-关键功能模块)
- [5. 存储系统](#5-存储系统)
- [6. 部署架构](#6-部署架构)

---

## 1. 整体架构

### 1.1 架构图示

```
┌─────────────────────────────────────────────────────────────────┐
│                      Agent Applications (Top)                   │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   App 1     │  │   App 2     │  │   App 3     │             │
│  │ (OpenAI API)│  │(Custom API) │  │ (Direct SDK)│             │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘             │
└─────────┼──────────────────┼──────────────────┼──────────────────┘
          │                  │                  │
┌─────────┼──────────────────┼──────────────────┼──────────────────┐
│         │                  │                  │                  │
│         ▼                  ▼                  ▼                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    Sentoris Proxy                         │  │
│  │  ┌─────────────────────────────────────────────────────┐ │  │
│  │  │              HTTP Transport Layer                  │ │  │
│  │  │  ┌──────────────┐  ┌──────────────┐  ┌─────────┐  │ │  │
│  │  │  │ Auth Middle-│  │Headers Middle-│  │Handlers │  │ │  │
│  │  │  │   ware      │  │    ware      │  │         │  │ │  │
│  │  │  └──────────────┘  └──────────────┘  └─────────┘  │ │  │
│  │  └─────────────────────────────────────────────────────┘ │  │
│  │  ┌─────────────────────────────────────────────────────┐ │  │
│  │  │              Service Layer                          │ │  │
│  │  │  ┌────────┐  ┌──────────┐  ┌──────────┐  ┌───────┐ │ │  │
│  │  │  │Audit   │  │Gover-    │  │Router    │  │Hooks  │ │ │  │
│  │  │  │Signer  │  │nance     │  │(Model)   │  │System │ │ │  │
│  │  │  └────────┘  └──────────┘  └──────────┘  └───────┘ │ │  │
│  │  │  ┌──────────────────┐  ┌──────────────────┐        │ │  │
│  │  │  │Extensions        │  │Security & API    │        │ │  │
│  │  │  │Registry          │  │Keys Management   │        │ │  │
│  │  │  └──────────────────┘  └──────────────────┘        │ │  │
│  │  └─────────────────────────────────────────────────────┘ │  │
│  │  ┌─────────────────────────────────────────────────────┐ │  │
│  │  │              Storage Layer                          │ │  │
│  │  │  ┌──────────────┐              ┌──────────────┐     │ │  │
│  │  │  │  Redis       │              │  PostgreSQL  │     │ │  │
│  │  │  │  (Budget)    │              │  (Traces,    │     │ │  │
│  │  │  └──────────────┘              │  Providers,  │     │ │  │
│  │  │  ┌──────────────┐              │  RiskReports,│     │ │  │
│  │  │  │  In-Memory   │◄─Fallback───▶│  API Keys)  │     │ │  │
│  │  │  │  (Dev)       │              └──────────────┘     │ │  │
│  │  │  └──────────────┘                                   │ │  │
│  │  └─────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────┘  │
│         │                                           │            │
│         │                                           ▼            │
│         │                                    ┌──────────┐         │
│         │                                    │Sentoris  │         │
│         │                                    │  UI      │         │
│         │                                    │          │         │
│         │                                    └──────────┘         │
└─────────┼─────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│                     LLM Providers (Bottom)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │
│  │  OpenAI      │  │  DeepSeek    │  │  Qwen        │            │
│  │  (GPT-4o)    │  │  (DeepSeek)  │  │  (Qwen)      │            │
│  └──────────────┘  └──────────────┘  └──────────────┘            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                Sentoris LLM Mock (8081)                 │   │
│  │            (用于开发测试的Mock服务)                        │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 架构分层说明

Sentoris Protocol v1.0.0 采用经典的三层架构：

| 层级 | 说明 | 核心职责 |
|------|------|----------|
| **顶层（应用层）** | Agent Applications | 各类应用程序，通过标准OpenAI API或自定义API调用 |
| **中间层（代理层）** | Sentoris Proxy | 核心代理，提供可观察性、治理、审计等功能 |
| **底层（服务层）** | LLM Providers | 真实的LLM服务提供商（OpenAI、DeepSeek等）或Mock服务 |

---

## 2. 核心组件详解

### 2.1 Sentoris Proxy (核心代理)

**位置**: `/Users/williamlee/github/spec-trae/sentoris-proxy/`

**主要入口**: `cmd/sentoris-proxy/main.go`

**核心功能**:
- OpenAI API兼容的请求代理
- Trace记录与审计
- 预算控制与治理
- 模型路由与调度
- 钩子系统
- 扩展系统
- 管理监控API

**核心子模块**:

#### 2.1.1 HTTP 传输层 (`internal/transport/http/`)
- `handler.go`: 主要请求处理器，包含所有API端点
- `middleware/auth.go`: 认证中间件
- `middleware/headers.go`: 头部处理中间件
- `sentoris_headers.go`: Sentoris特定的头部定义
- `response_headers.go`: 响应头部处理

#### 2.1.2 服务层 (`internal/service/`)
- `audit/`: 审计签名与验证系统
  - `signer.go`: 基于SHA256的签名生成器
  - 使用JCS（JSON Canonicalization Scheme）规范化
  - 验证完整性与防篡改
  
- `governance/`: 治理系统
  - `evaluator.go`: 约束评估器
  - `budget_service.go`: 预算管理服务
  - `privacy_service.go`: 隐私保护服务
  
- `router/`: 模型路由系统
  - `model_router.go`: 多提供商模型调度
  
- `hooks/`: 钩子系统
  - `hooks.go`: 钩子注册与执行链
  - 预置钩子：PII检测、速率限制等
  
- `extensions/`: 扩展系统
  - `registry.go`: 扩展注册表
  - 内置扩展：Memory Firewall、Custom Rule
  
- `security/`: 安全系统
  - `key_manager.go`: API Key管理

#### 2.1.3 适配层 (`internal/adapter/`)
- `storage/`: 存储适配器
  - 支持 PostgreSQL（生产）
  - 支持 Redis（预算）
  - 支持 内存存储（开发/降级）
  
- `upstream/`: 上游LLM客户端
  - `real_client.go`: 真实LLM客户端
  - `fault_tolerant_client.go`: 容错客户端
  - `circuit_breaker.go`: 熔断器
  - `sse_reader.go`: SSE流读取器

#### 2.1.4 领域层 (`internal/domain/`)
- `trace.go`: Trace核心数据结构
  - 状态机：INIT → CONSTRAINT_EVAL → EXECUTING → VALIDATION → FINALIZED/FAILED
  - 包含输入、输出、观察值、证明、约束等完整信息

- `risk_report.go`: 风险报告结构

#### 2.1.5 配置层 (`internal/config/`)
- `config.go`: 配置加载与热更新
- 支持 YAML 配置文件
- 支持环境变量替换
- 默认配置 + 配置验证

#### 2.1.6 UI层 (`internal/ui/`)
- `server.go`: UI服务
- 静态文件服务
- API代理

### 2.2 Sentoris LLM Mock (模拟服务)

**位置**: `/Users/williamlee/github/spec-trae/sentoris-llm-mock/`

**主要入口**: `cmd/sentoris-llm-mock/main.go`

**核心功能**:
- 完全兼容 OpenAI Chat Completions API
- 支持流式与非流式响应
- 可配置的延迟、错误注入
- 自定义响应内容
- 主要用于开发测试

**关键特性**:
- 端口: 8081 (默认)
- 支持多种模型模拟（gpt-4o, deepseek, qwen）
- SSE 流式输出支持

---

## 3. 数据流与处理流程

### 3.1 Chat Completions 请求流程

```
1. Agent Application → Sentoris Proxy (POST /v1/chat/completions)
   ↓
2. HTTP Transport Layer
   - Auth Middleware: 验证API Key
   - Headers Middleware: 处理Sentoris头部
   ↓
3. Handler - 初始化Trace
   - 生成唯一Trace ID
   - 状态: INIT → CONSTRAINT_EVAL
   ↓
4. Governance Service - 约束评估
   - 预算检查（Redis）
   - 隐私约束检查
   - 可复现性约束检查
   ↓
5. Hook Chain - 钩子执行
   - PII检测
   - 速率限制
   - 自定义钩子
   ↓
6. Extensions - 扩展执行
   - Memory Firewall
   - Custom Rule
   ↓
7. Model Router - 模型路由
   - 选择目标Provider
   - 模型降级（如需要）
   ↓
8. Upstream Client - 上游调用
   - 调用真实LLM或Mock服务
   - SSE流处理（如需要）
   - 容错处理
   ↓
9. Audit Signer - 审计签名
   - 使用JCS规范化
   - 生成SHA256签名
   - 状态: EXECUTING → VALIDATION → FINALIZED
   ↓
10. Storage - 数据持久化
    - 保存Trace到PostgreSQL
    - 更新预算到Redis
    ↓
11. Response - 返回结果
    - 包含Sentoris头部（Trace ID、签名等）
    - 状态: FINALIZED
```

### 3.2 Trace 状态机

```
      ┌────────┐
      │  INIT  │
      └───┬────┘
          │
          ▼
┌──────────────────┐
│ CONSTRAINT_EVAL  │
└───┬────────┬─────┘
    │        │
    │        ▼
    │    ┌────────┐
    │    │ FAILED │
    │    └────────┘
    ▼
┌───────────┐
│ EXECUTING │
└───┬───────┘
    │
    ▼
┌────────────┐
│ VALIDATION │
└───┬────┬───┘
    │    │
    │    ▼
    │┌────────┐
    ││ FAILED │
    │└────────┘
    ▼
┌───────────┐
│ FINALIZED │
└───────────┘
```

---

## 4. 关键功能模块

### 4.1 Trace 与审计系统

**核心文件**: `internal/domain/trace.go`, `internal/service/audit/signer.go`

**Trace 结构**:
```go
type Trace struct {
    TraceID            string            // 唯一追踪ID
    ParentID           *string           // 父Trace ID（用于嵌套调用）
    SessionID          *string           // 会话ID
    ExecutionState     ExecutionState    // 当前执行状态
    Model              string            // 使用的模型
    Input              Input             // 输入信息
    Output             Output            // 输出信息
    Observations       Observations      // 观察值（token数、成本、延迟等）
    Proofs             Proofs            // 审计证明
    ConstraintsApplied ConstraintsApplied // 应用的约束
    CreatedAt          time.Time         // 创建时间
    TTLExpireAt        *time.Time        // 过期时间
    Extensions         map[string]any    // 扩展数据
}
```

**审计签名**:
- 使用 JCS (RFC 8785) 规范化JSON
- 生成 SHA256 哈希作为签名
- 可验证完整性与防篡改

### 4.2 预算与治理系统

**核心文件**: `internal/service/governance/evaluator.go`, `internal/service/governance/budget_service.go`

**预算策略**:
- `hard_stop`: 超过预算直接阻止
- `degrade_model`: 超过预算自动降级到更便宜的模型
- `soft_alert`: 超过预算只记录警告，继续执行

**隐私级别**:
- `raw`: 原始数据，无隐私处理
- `masked`: 掩码处理指定字段
- `hash_only`: 只存储哈希值

**可复现性模式**:
- `none`: 无可复现性保证
- `bounded`: 有限可复现性（相同种子）
- `strict`: 严格可复现性（完整重放）

### 4.3 钩子系统

**核心文件**: `internal/service/hooks/hooks.go`

**预置钩子**:
1. `NoopHook`: 空钩子
2. `PIIDetectorHook`: PII（个人身份信息）检测
3. `RateLimiterHook`: 速率限制

**执行策略**:
- `short_circuit`: 任一钩子失败即终止
- `all_execute`: 执行所有钩子

### 4.4 扩展系统

**核心文件**: `internal/service/extensions/registry.go`

**内置扩展**:
1. `MemoryFirewallExtension`: 内存防火墙（安全）
2. `CustomRuleExtension`: 自定义规则

**扩展元数据**:
- Namespace: 扩展命名空间
- Version: 版本号
- Status: 状态（active/deprecated等）
- Tags: 标签

### 4.5 模型路由系统

**核心文件**: `internal/service/router/model_router.go`

**功能**:
- 多提供商支持（OpenAI、DeepSeek、Qwen等）
- 模型降级映射
- 持久化Provider配置（PostgreSQL）
- 动态Provider管理

---

## 5. 存储系统

### 5.1 存储架构

```
┌─────────────────────────────────────────┐
│        Storage Adapter Layer            │
│  ┌─────────────┐   ┌─────────────┐   │
│  │PostgreSQL   │   │  Redis      │   │
│  │ (Primary)   │   │ (Budget)    │   │
│  └──────┬──────┘   └──────┬──────┘   │
│         │◄────Fallback───►│          │
│         ▼                 ▼          │
│  ┌─────────────────────────────┐    │
│  │   In-Memory Storage         │    │
│  │   (Dev / Fallback)          │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────────┘
```

### 5.2 PostgreSQL 存储

**表结构**:
1. `traces`: Trace记录
2. `providers`: Provider配置
3. `api_keys`: API Key
4. `risk_reports`: 风险报告

**连接配置** (`config.yaml`):
```yaml
postgresql:
  dsn: "postgres://postgres:postgres@localhost:5432/sentoris?sslmode=disable"
```

### 5.3 Redis 存储

**用途**:
- 预算追踪（高读写）
- 缓存

**支持模式**:
- single (单机)
- sentinel (哨兵)
- cluster (集群)

**配置** (`config.yaml`):
```yaml
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  mode: "single"
```

### 5.4 内存存储

**用途**:
- 开发环境
- 降级方案（数据库连接失败）
- 测试

**特点**:
- 无需外部依赖
- 重启数据丢失

---

## 6. 部署架构

### 6.1 Docker Compose 架构

**文件**: `docker-compose.yml`

```yaml
services:
  # Redis (预算存储)
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
  
  # PostgreSQL (主存储)
  postgres:
    image: postgres:15-alpine
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: sentoris
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
  
  # Sentoris Proxy
  sentoris-proxy:
    build:
      context: ./sentoris-proxy
    ports:
      - "8080:8080"
    depends_on:
      - redis
      - postgres
  
  # Sentoris LLM Mock
  sentoris-llm-mock:
    build:
      context: ./sentoris-llm-mock
    ports:
      - "8081:8081"
```

### 6.2 服务端口映射

| 服务 | 端口 | 协议 | 说明 |
|------|------|------|------|
| Sentoris Proxy | 8080 | HTTP | 主要API服务 |
| Sentoris LLM Mock | 8081 | HTTP | Mock LLM服务 |
| Redis | 6379 | TCP | 缓存/预算 |
| PostgreSQL | 5432 | TCP | 主数据库 |
| Metrics | 9090 | HTTP | Prometheus指标 |

### 6.3 监控与可观测性

**指标系统**:
- Prometheus: `:9090/metrics`
- OpenTelemetry: 可配置

**监控API** (`/v1/monitor/*`):
- `GET /v1/monitor/traces`: 获取Trace列表
- `GET /v1/monitor/traces-list`: 分页Trace列表
- `GET /v1/monitor/models`: 模型列表
- `GET /v1/monitor/budget`: 预算状态
- `GET /v1/monitor/risk-reports`: 风险报告
- `GET /v1/monitor/extensions`: 扩展列表
- `GET /v1/monitor/metrics`: 指标数据

### 6.4 健康检查

**端点**: `GET /health`

**响应**:
```json
{
  "status": "ok"
}
```

---

## 7. 开发与扩展

### 7.1 添加新的 Provider

1. 在 `config.yaml` 中配置
2. 或通过管理API动态添加
3. 使用 `ModelRouter.AddProvider()`

### 7.2 添加新的 Hook

1. 实现 `Hook` 接口
2. 注册到 `HookRegistry`
3. 在配置中启用

### 7.3 添加新的 Extension

1. 实现 `Extension` 接口
2. 创建 `ExtensionRegistryEntry`
3. 注册到 `ExtensionRegistry`

---

## 8. 关键技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 主要语言 | Go 1.21+ | 高性能、并发友好 |
| 数据库 | PostgreSQL 15+ | 关系型数据库，ACID |
| 缓存 | Redis 7+ | 高性能K/V存储 |
| Web框架 | 标准库 `net/http` | 轻量、高性能 |
| 序列化 | JSON + JCS | 标准化JSON |
| 监控 | Prometheus + OpenTelemetry | 可观测性 |
| 容器化 | Docker + Docker Compose | 部署管理 |
| Mock服务 | Gin | 轻量Web框架 |

---

## 9. 核心协议实现要点

### 9.1 Sentoris 协议头部

**请求头**:
- `X-Sentoris-Trace-Parent`: 父Trace ID
- `X-Sentoris-Session-Id`: 会话ID
- `X-Sentoris-Constraints`: 约束配置（JSON）
- `X-Sentoris-Extensions`: 扩展配置（JSON）

**响应头**:
- `X-Sentoris-Trace-Id`: Trace ID
- `X-Sentoris-Audit-Signature`: 审计签名
- `X-Sentoris-State`: 最终状态
- `X-Sentoris-Cost-USD`: 成本（USD）

### 9.2 协议版本

当前版本: v1.0.0

兼容性: 完全兼容 OpenAI Chat Completions API

---

## 10. 文件结构总览

```
spec-trae/
├── sentoris-proxy/              # 核心代理服务
│   ├── cmd/                     # 可执行程序入口
│   │   ├── sentoris-proxy/     # 主服务
│   │   ├── sentoris-ui/        # UI服务
│   │   └── load-test/          # 负载测试
│   ├── internal/                # 内部代码
│   │   ├── adapter/            # 适配器（存储、上游）
│   │   ├── config/             # 配置管理
│   │   ├── domain/             # 领域模型
│   │   ├── service/            # 业务服务
│   │   ├── transport/          # 传输层（HTTP）
│   │   └── ui/                 # UI服务
│   ├── pkg/                     # 公共包
│   ├── config.yaml             # 配置文件
│   └── docker-compose.yml      # Docker部署
│
├── sentoris-llm-mock/          # LLM Mock服务
│   ├── cmd/
│   │   └── sentoris-llm-mock/
│   ├── internal/
│   │   └── handler/
│   └── docker-compose.yml
│
└── USER_GUIDE.md               # 用户指南
```

---

## 总结

Sentoris Protocol v1.0.0 是一个功能完整、架构清晰的LLM代理系统：

1. **三层架构**: 应用层、代理层、服务层，职责明确
2. **核心能力**: Trace审计、预算治理、模型路由、钩子扩展
3. **存储设计**: 多级存储（PostgreSQL+Redis+内存），自动降级
4. **可观测性**: Prometheus+OpenTelemetry，完整的监控API
5. **开发友好**: Mock服务、内存存储、完整的测试工具

该系统为LLM应用提供了企业级的可观察性、安全性和治理能力！
