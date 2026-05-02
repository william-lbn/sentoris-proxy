# Sentoris Proxy 端到端测试场景文档

## 概述

本文档详细描述了 Sentoris Proxy 的所有端到端测试场景，涵盖：
- 正常请求流程（流式/非流式）
- 预算治理（硬停止/降级/软告警）
- 隐私脱敏（raw/masked/hash_only）
- 可复现性（none/bounded/strict）
- 钩子系统（PII检测/速率限制）
- 扩展系统
- 错误处理
- Replay + Diff 闭环
- UI数据校验
- Redis/PostgreSQL 数据一致性校验

## 测试环境

### 依赖服务
- **Redis** (port 6379): 预算存储
- **PostgreSQL** (port 5432): Trace/RiskReport/Provider/API Key存储
- **Senteris Proxy** (port 8080): 主服务
- **Mock LLM** (port 8081): Mock LLM服务
- **UI**: 可视化界面

### 启动方式
```bash
cd sentoris-proxy
docker-compose up -d
# 或者使用根目录的docker-compose
cd /Users/williamlee/github/spec-trae
docker-compose up -d
```

---

## 测试场景分类

### 1. 基础请求场景 (Basic Scenarios)

#### 场景 1.1: 非流式基础请求 - E2E-001

**测试目的**: 验证正常非流式请求完整流程

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "Hello, how are you?"}
  ],
  "max_tokens": 100,
  "temperature": 0.7
}
```

**预期输出**:
- HTTP 200 OK
- 完整响应结构 (id, object, created, model, choices, usage)
- Sentoris-Trace-Id 响应头
- Sentoris-Signature 响应头

**数据校验**:
- PostgreSQL: Trace表有完整记录
- PostgreSQL: 审计签名有效
- UI: 可查看Trace详情

---

#### 场景 1.2: 流式基础请求 - E2E-002

**测试目的**: 验证正常流式请求完整流程

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "Tell me a short story about AI"}
  ],
  "max_tokens": 100,
  "stream": true
}
```

**预期输出**:
- HTTP 200 OK
- Content-Type: text/event-stream
- 完整SSE数据流
- Sentoris-Trace-Id 响应头

**数据校验**:
- PostgreSQL: Trace表有完整记录
- UI: 可查看Trace详情
- UI: 可查看流式输出

---

#### 场景 1.3: 多轮对话会话 - E2E-003

**测试目的**: 验证多轮对话与会话ID的关联

**输入参数 (第一轮)**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello, my name is Alice"}],
  "max_tokens": 50
}
```
Headers: X-Sentoris-Session-ID: test-session-001

**输入参数 (第二轮)**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "What's my name?"}],
  "max_tokens": 50
}
```
Headers: X-Sentoris-Session-ID: test-session-001

**数据校验**:
- PostgreSQL: 两条Trace有相同的SessionID
- UI: 会话视图显示完整对话历史

---

#### 场景 1.4: 模型路由测试 - E2E-004

**测试目的**: 验证多模型路由功能

**测试模型**:
- gpt-4o
- deepseek-chat  
- qwen-turbo

**数据校验**:
- PostgreSQL: Trace记录显示正确的模型
- UI: 不同模型的Trace可正确显示

---

### 2. 预算治理场景 (Budget Governance)

#### 场景 2.1: 预算硬停止 - E2E-100

**测试目的**: 验证预算硬停止策略

**前置条件**:
- Redis: 设置会话预算 0.005 USD

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Generate a very long text..."}],
  "max_tokens": 2000
}
```
Headers:
- Sentoris-Budget-Limit: 0.005
- Sentoris-Budget-Strategy: hard_stop

**预期输出**:
- HTTP 400或429 Too Many Requests
- Sentoris-Error: budget_exhausted

**数据校验**:
- Redis: 预算正确扣除
- PostgreSQL: Trace记录为FAILED状态
- UI: 显示预算耗尽警告

---

#### 场景 2.2: 预算降级策略 - E2E-101

**测试目的**: 验证预算不足时自动降级到更便宜的模型

**前置条件**:
- Redis: 设置会话预算 0.001 USD

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Budget-Limit: 0.001
- Sentoris-Budget-Strategy: degrade_model

**预期输出**:
- HTTP 200 OK
- 实际使用的模型被降级 (比如gpt-4o -> qwen-turbo)
- Sentoris-Warning 头提示降级

**数据校验**:
- PostgreSQL: Trace记录显示降级后的模型
- PostgreSQL: ConstraintsApplied记录降级策略
- Redis: 预算正确扣除
- UI: 显示降级警告

---

#### 场景 2.3: 预算软告警 - E2E-102

**测试目的**: 验证预算软告警策略

**前置条件**:
- Redis: 设置会话预算 0.01 USD

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Budget-Limit: 0.01
- Sentoris-Budget-Strategy: soft_alert
- Sentoris-Budget-Alert-Threshold: 0.5

**预期输出**:
- HTTP 200 OK
- Sentoris-Warning 头提示预算使用百分比

**数据校验**:
- Redis: 预算正确扣除
- PostgreSQL: Trace记录包含警告信息
- UI: 显示预算使用进度

---

#### 场景 2.4: 并发预算原子性 - E2E-103

**测试目的**: 验证高并发场景下预算扣减的原子性

**前置条件**:
- Redis: 设置会话预算 1.00 USD

**测试方法**:
- 并发发送 50 个请求
- 每个请求预算 0.03 USD
- 预期约 33 个成功，17 个失败

**数据校验**:
- Redis: 总预算扣除不超过 1.00 USD
- PostgreSQL: 成功Trace数不超过 33
- PostgreSQL: 所有Trace的CostEstimatedUSD之和不超过 1.00 USD

---

### 3. 隐私脱敏场景 (Privacy Masking)

#### 场景 3.1: Raw 级别 (无脱敏) - E2E-200

**测试目的**: 验证 raw 级别隐私策略

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "My email is alice@example.com, phone: +1-555-123-4567"}
  ],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Privacy-Level: raw

**数据校验**:
- PostgreSQL: Trace.Input 保留完整原文
- UI: 显示完整输入

---

#### 场景 3.2: Masked 级别 - E2E-201

**测试目的**: 验证 masked 级别隐私策略

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "My email is alice@example.com, phone: +1-555-123-4567"}
  ],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Privacy-Level: masked
- Sentoris-Privacy-Masked-Fields: $.messages[0].content

**数据校验**:
- PostgreSQL: Trace.Input 中邮箱和电话被替换为 ***
- UI: 显示脱敏后的内容
- PostgreSQL: Metadata标记敏感信息类型

---

#### 场景 3.3: Hash Only 级别 - E2E-202

**测试目的**: 验证 hash_only 级别隐私策略

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "My email is alice@example.com"}
  ],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Privacy-Level: hash_only

**数据校验**:
- PostgreSQL: Trace.Input 只保存内容的哈希值
- UI: 显示哈希值，不显示原文

---

#### 场景 3.4: JSONPath 字段级脱敏 - E2E-203

**测试目的**: 验证基于JSONPath的精确字段脱敏

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "Normal message"},
    {"role": "user", "content": "Secret: 12345"}
  ],
  "max_tokens": 50
}
```
Headers:
- Sentoris-Privacy-Level: masked
- Sentoris-Privacy-Masked-Fields: $.messages[1].content

**数据校验**:
- PostgreSQL: 第一条消息保留，第二条被脱敏
- UI: 正确显示脱敏效果

---

### 4. 可复现性场景 (Reproducibility)

#### 场景 4.1: None 可复现性 - E2E-300

**测试目的**: 验证 none 可复现性（无保证）

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Generate a random number between 1 and 100"}],
  "max_tokens": 20
}
```
Headers:
- Sentoris-Reproducibility: none

**数据校验**:
- PostgreSQL: 不强制要求seed

---

#### 场景 4.2: Bounded 可复现性 - E2E-301

**测试目的**: 验证 bounded 可复现性（有界保证）

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Generate a random number between 1 and 100"}],
  "max_tokens": 20,
  "seed": 42
}
```
Headers:
- Sentoris-Reproducibility: bounded

**数据校验**:
- PostgreSQL: 记录seed值
- Redis: 会话级别可复现性缓存

---

#### 场景 4.3: Strict 可复现性 - E2E-302

**测试目的**: 验证 strict 可复现性（严格保证）

**输入参数 (两次相同请求)**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Generate a random number between 1 and 100"}],
  "max_tokens": 20,
  "seed": 42,
  "temperature": 0
}
```
Headers:
- Sentoris-Reproducibility: strict

**数据校验**:
- PostgreSQL: 两条Trace的Output.Response相同
- UI: 两次响应内容一致

---

### 5. 钩子系统场景 (Hook System)

#### 场景 5.1: PII 检测钩子 - E2E-400

**测试目的**: 验证PII信息检测功能

**测试用例**:
1. 包含邮箱: alice@example.com
2. 包含电话: +1-555-123-4567
3. 包含信用卡: 4111-1111-1111-1111
4. 包含社保号: 123-45-6789

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "My email is alice@example.com and my card is 4111-1111-1111-1111"}],
  "max_tokens": 50
}
```

**数据校验**:
- PostgreSQL: Trace.Input.Metadata 记录检测到的PII类型
- UI: 显示PII检测警告标签
- PostgreSQL: ExecutionState包含PII检测信息

---

#### 场景 5.2: 速率限制钩子 - E2E-401

**测试目的**: 验证会话级速率限制

**前置条件**:
- 设置速率限制: 5 requests/minute

**测试方法**:
- 快速连续发送 10 个请求

**预期输出**:
- 前 5 个: 200 OK
- 第 6-10 个: 429 Too Many Requests

**数据校验**:
- Redis: 计数器正确计数
- PostgreSQL: 失败Trace记录速率限制错误
- UI: 显示速率限制警告

---

#### 场景 5.3: 钩子链执行 - E2E-402

**测试目的**: 验证多个钩子按顺序执行

**钩子链配置**:
1. PII检测钩子
2. 速率限制钩子
3. 自定义规则钩子

**数据校验**:
- PostgreSQL: Metadata记录所有钩子执行结果
- UI: 显示完整钩子执行日志

---

### 6. 扩展系统场景 (Extension System)

#### 场景 6.1: Memory Firewall 扩展 - E2E-410

**测试目的**: 验证 Memory Firewall 扩展

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50,
  "extensions": {
    "sentoris.ai/v1/memory_firewall": {
      "max_memory": 1024
    }
  }
}
```

**数据校验**:
- PostgreSQL: 记录扩展调用
- UI: 显示扩展执行结果

---

#### 场景 6.2: 自定义规则扩展 - E2E-411

**测试目的**: 验证自定义规则扩展

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50,
  "extensions": {
    "x-acme-corp/v1/custom-rule": {
      "rule_id": "rule1"
    }
  }
}
```

---

### 7. 错误处理场景 (Error Handling)

#### 场景 7.1: 无效模型 - E2E-500

**输入参数**:
```json
{
  "model": "invalid-model-name-xyz",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50
}
```

**预期输出**:
- HTTP 400 Bad Request
- Sentoris-Error: invalid_model

**数据校验**:
- PostgreSQL: 记录错误状态
- UI: 显示友好错误消息

---

#### 场景 7.2: 版本不兼容 - E2E-501

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}]
}
```
Headers:
- Sentoris-Version: 999.0.0

**预期输出**:
- HTTP 400 Bad Request
- Sentoris-Error: version_mismatch

---

#### 场景 7.3: 流式错误截断 - E2E-502

**测试目的**: 验证流式错误的规范处理

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 10000,
  "stream": true
}
```
Headers:
- Sentoris-Budget-Limit: 0.0001
- Sentoris-Budget-Strategy: hard_stop

**预期输出**:
- 初始200 OK，开始流式输出
- 预算耗尽时发送规范SSE错误事件
- 错误包含Trace-ID和版本信息

**数据校验**:
- PostgreSQL: Trace记录包含错误详情
- UI: 正确显示流式错误

---

#### 场景 7.4: 上游服务超时 - E2E-503

**测试目的**: 验证上游超时的容错处理

**数据校验**:
- PostgreSQL: Trace记录EXECUTING -> FAILED状态转换
- UI: 显示超时错误

---

#### 场景 7.5: 认证失败 - E2E-504

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}]
}
```
Headers:
- Authorization: Bearer invalid-token

**预期输出**:
- HTTP 401 Unauthorized
- Sentoris-Error: authentication_failed

---

### 8. Replay + Diff 闭环场景 (Replay + Diff)

#### 场景 8.1: 基础 Replay 功能 - E2E-600

**测试目的**: 验证Replay基本功能

**前置条件**:
- 已有一个基线Trace (trace_id: baseline-001)

**Replay 输入**:
```json
{
  "baseline_trace_id": "baseline-001",
  "model": "gpt-4o"
}
```

**预期输出**:
- 新的Trace (candidate) 生成
- RiskReport 生成并保存
- 模型与基线相同

**数据校验**:
- PostgreSQL: 两条Trace都存在
- PostgreSQL: RiskReport存在
- UI: RiskReport可视化展示
- UI: 差异对比视图

---

#### 场景 8.2: 模型替换 Replay - E2E-601

**测试目的**: 验证模型替换后的对比

**Replay 输入**:
```json
{
  "baseline_trace_id": "baseline-001",
  "model": "deepseek-chat"
}
```

**数据校验**:
- PostgreSQL: RiskReport标记model_changed: true
- PostgreSQL: 两条Trace的Model字段不同
- UI: 高亮显示模型差异

---

#### 场景 8.3: 高风险输出差异 - E2E-602

**测试目的**: 验证高风险差异的检测

**前置条件**:
- 基线Trace: 输出 "Yes"
- Candidate Trace: 输出完全不同的内容

**数据校验**:
- PostgreSQL: RiskReport的RiskAssessment.RiskLevel: high
- PostgreSQL: TokenDiff.SimilarityRatio < 0.3
- UI: 高风险红色警告
- UI: Recommendation: block_release

---

#### 场景 8.4: 中风险差异 - E2E-603

**测试目的**: 验证中等风险差异

**数据校验**:
- PostgreSQL: RiskAssessment.RiskLevel: medium
- PostgreSQL: SimilarityRatio: 0.3-0.7
- UI: 黄色警告
- UI: Recommendation: review_required

---

#### 场景 8.5: 低风险差异 - E2E-604

**测试目的**: 验证低风险差异

**数据校验**:
- PostgreSQL: RiskAssessment.RiskLevel: low
- PostgreSQL: SimilarityRatio > 0.7
- UI: 绿色通过
- UI: Recommendation: approve

---

#### 场景 8.6: 焦点字段对比 - E2E-605

**测试目的**: 验证焦点字段对比功能

**Replay 输入**:
```json
{
  "baseline_trace_id": "baseline-001",
  "focus_fields": ["$.choices[0].message.content", "$.usage.total_tokens"]
}
```

**数据校验**:
- PostgreSQL: RiskReport.FocusFields包含指定字段
- UI: 只展示焦点字段的差异

---

#### 场景 8.7: 字段级风险分析 - E2E-606

**测试目的**: 验证字段级风险分析

**数据校验**:
- PostgreSQL: RiskAssessment.FieldRisks数组完整
- 每个字段: Path, OldValue, NewValue, RiskLevel, Confidence, ChangeType
- UI: 字段级差异展示

---

### 9. UI 场景 (UI Scenarios)

#### 场景 9.1: Trace 列表视图 - E2E-700

**测试内容**:
- 访问 /ui/traces
- 分页显示
- 筛选功能 (按模型/时间/状态)
- 排序功能

**数据校验**:
- UI显示与PostgreSQL数据一致
- 筛选结果正确

---

#### 场景 9.2: Trace 详情视图 - E2E-701

**测试内容**:
- 访问 /ui/traces/:trace_id
- 显示完整信息:
  - 输入输出
  - 审计签名
  - 执行状态历史
  - 约束信息
  - 成本和token统计

**数据校验**:
- UI显示与PostgreSQL数据完全一致

---

#### 场景 9.3: 会话视图 - E2E-702

**测试内容**:
- 访问 /ui/sessions/:session_id
- 显示会话内所有Trace
- 显示预算使用情况

**数据校验**:
- UI: 会话预算与Redis一致
- UI: Trace列表与PostgreSQL一致

---

#### 场景 9.4: RiskReport 视图 - E2E-703

**测试内容**:
- 访问 /ui/risk-reports/:report_id
- 显示基线和候选Trace对比
- 显示相似度评分
- 显示风险评估
- 显示字段级差异

**数据校验**:
- UI与RiskReport数据一致

---

#### 场景 9.5: 审计视图 - E2E-704

**测试内容**:
- 访问 /ui/audit
- 显示审计签名验证状态
- 显示数据完整性验证

---

### 10. 数据一致性场景 (Data Consistency)

#### 场景 10.1: PostgreSQL 数据完整性 - E2E-800

**验证内容**:
1. Trace表记录完整
2. RiskReport表记录完整
3. Provider表记录正确
4. APIKey表记录正确
5. 外键关系正确

---

#### 场景 10.2: Redis 预算一致性 - E2E-801

**验证内容**:
1. budget:session-id 总预算
2. budget:reserved:session-id 预留金额
3. budget:used:session-id 已使用金额
4. 三者关系正确: budget >= reserved + used

---

#### 场景 10.3: 审计签名验证 - E2E-802

**验证内容**:
1. 每个Trace都有签名
2. 签名可正确验证
3. 数据篡改后签名验证失败

---

#### 场景 10.4: JSON规范化验证 - E2E-803

**验证内容**:
1. Trace数据按JCS规范规范化
2. 相同数据规范化后相同
3. 基于规范化数据的签名正确

---

### 11. 边界条件场景 (Edge Cases)

#### 场景 11.1: 空输入 - E2E-900

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": []
}
```

**预期输出**:
- HTTP 400 Bad Request

---

#### 场景 11.2: 超长输入 - E2E-901

**输入参数**:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Very long text... (100KB+)" }],
  "max_tokens": 100
}
```

**数据校验**:
- PostgreSQL: 可正常保存超长数据
- UI: 可正常显示/折叠

---

#### 场景 11.3: 特殊字符 - E2E-902

**输入内容**:
- Emoji: 😊🚀
- Unicode: 中文、日文、阿拉伯文
- 特殊符号: @#$%^&*()

**数据校验**:
- PostgreSQL: 正确保存特殊字符
- UI: 正确显示特殊字符

---

#### 场景 11.4: 零成本请求 - E2E-903

**测试目的**: 验证零成本请求的预算处理

**数据校验**:
- Redis: 预算无变化
- PostgreSQL: CostEstimatedUSD: 0

---

### 12. 性能与压力场景 (Performance & Stress)

#### 场景 12.1: 高并发请求 - E2E-1000

**测试方法**:
- 1000 并发请求
- 持续 5 分钟

**验证内容**:
- 系统稳定运行
- 无数据丢失
- 响应时间可接受

---

#### 场景 12.2: 大批量数据查询 - E2E-1001

**测试方法**:
- UI查询 10000+ 条Trace
- 测试分页性能

---

## 测试结果记录模板

### 测试执行记录

| 场景ID | 场景名称 | 执行日期 | 执行者 | 结果 | 备注 |
|--------|----------|----------|--------|------|------|
| E2E-001 | 非流式基础请求 | | | | |

### 数据校验记录

| 数据位置 | 检查项 | 预期值 | 实际值 | 状态 |
|----------|--------|--------|--------|------|
| PostgreSQL.Trace | ExecutionState | FINALIZED | | |
| Redis | budget remaining | 0.95 | | |
| UI | Trace显示 | 完整 | | |

---

## 附录

### A. 测试数据生成脚本

参考: `test_scripts/e2e_test_runner.py`

### B. 环境变量配置

```bash
REDIS_HOST=localhost
REDIS_PORT=6379
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=sentoris
```

### C. 常见问题排查

1. 连接Redis失败: 检查docker-compose状态
2. 连接PostgreSQL失败: 检查数据库初始化
3. Mock LLM不响应: 检查8081端口
4. UI无法访问: 检查8080端口

