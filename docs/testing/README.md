# Sentoris Proxy 端到端测试指南

## 快速开始

### 1. 启动测试环境

```bash
# 进入项目目录
cd /Users/williamlee/github/spec-trae/sentoris-proxy

# 使用docker-compose启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps
```

预期服务:
- ✅ `sentoris-redis` (端口 6379)
- ✅ `sentoris-postgres` (端口 5432)
- ✅ `sentoris-proxy` (端口 8080)
- ✅ `mock-llm` (端口 8081, 如果配置了)

### 2. 安装Python依赖

```bash
# 进入测试脚本目录
cd test_scripts

# 创建虚拟环境（推荐）
python3 -m venv venv
source venv/bin/activate  # Linux/Mac
# 或
.\venv\Scripts\activate  # Windows

# 安装依赖
pip install aiohttp psycopg2-binary redis
```

### 3. 运行测试

```bash
# 查看帮助
python e2e_test_runner.py --help

# 列出所有可用场景
python e2e_test_runner.py --list

# 运行基础场景
python e2e_test_runner.py --basic

# 运行指定场景
python e2e_test_runner.py --scenarios E2E-001,E2E-002,E2E-600

# 运行所有场景
python e2e_test_runner.py --all
```

---

## 测试文档索引

| 文档 | 说明 |
|------|------|
| [E2E_TEST_SCENARIOS.md](./E2E_TEST_SCENARIOS.md) | 完整的测试场景列表，包含100+个测试用例 |
| [TEST_INPUTS_OUTPUTS.md](./TEST_INPUTS_OUTPUTS.md) | 详细的输入输出示例 |
| [README.md](./README.md) | 本文档，测试运行指南 |

---

## 测试场景分类

### 基础测试 (Basic Tests)
| 场景ID | 说明 |
|--------|------|
| E2E-001 | 非流式基础请求 |
| E2E-002 | 流式基础请求 |
| E2E-003 | 多轮对话会话 |
| E2E-700 | UI Trace列表视图 |
| E2E-800 | PostgreSQL数据完整性 |
| E2E-801 | Redis预算一致性 |

### 预算治理 (Budget Governance)
| 场景ID | 说明 |
|--------|------|
| E2E-100 | 预算硬停止 |
| E2E-101 | 预算降级策略 |
| E2E-102 | 预算软告警 |
| E2E-103 | 并发预算原子性 |

### 隐私脱敏 (Privacy)
| 场景ID | 说明 |
|--------|------|
| E2E-200 | Raw级别隐私 |
| E2E-201 | Masked级别隐私 |
| E2E-202 | Hash Only级别隐私 |
| E2E-203 | JSONPath字段级脱敏 |

### Replay + Diff (关键功能)
| 场景ID | 说明 |
|--------|------|
| E2E-600 | 基础Replay功能 |
| E2E-601 | 模型替换Replay |
| E2E-602 | 高风险差异检测 |
| E2E-603 | 中风险差异检测 |
| E2E-604 | 低风险差异检测 |
| E2E-605 | 焦点字段对比 |
| E2E-606 | 字段级风险分析 |

---

## 手动测试步骤

如果自动化测试无法运行，可以按以下步骤进行手动测试：

### 测试1: 基础请求

**使用curl**:
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-token" \
  -H "X-Sentoris-Session-ID: manual-test-001" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello, how are you?"}],
    "max_tokens": 100
  }'
```

**验证响应**:
- HTTP 200状态码
- 响应包含 `choices` 和 `usage` 字段
- 响应头包含 `Sentoris-Trace-Id`

**验证PostgreSQL**:
```bash
# 连接数据库
psql -h localhost -U postgres -d sentoris

# 查询Trace
SELECT trace_id, model, execution_state, created_at 
FROM traces 
WHERE session_id = 'manual-test-001'
ORDER BY created_at DESC 
LIMIT 5;
```

### 测试2: 预算治理

```bash
# 1. 设置Redis预算
redis-cli SET budget:manual-test-100 0.005

# 2. 发送请求
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-token" \
  -H "X-Sentoris-Session-ID: manual-test-100" \
  -H "Sentoris-Budget-Limit: 0.005" \
  -H "Sentoris-Budget-Strategy: hard_stop" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Generate a very long text..."}],
    "max_tokens": 2000
  }'

# 3. 验证Redis预算
redis-cli MGET budget:manual-test-100 budget:used:manual-test-100
```

### 测试3: Replay + Diff

```bash
# 1. 先发送一个请求获取基线Trace ID（见测试1）

# 2. 使用基线Trace ID进行Replay
curl -X POST http://localhost:8080/v1/replay \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-token" \
  -d '{
    "baseline_trace_id": "<your-baseline-trace-id>",
    "model": "deepseek-chat",
    "focus_fields": ["$.choices[0].message.content"]
  }'
```

### 测试4: UI访问

打开浏览器访问:
- UI首页: http://localhost:8080/ui
- Trace列表: http://localhost:8080/ui/traces
- 健康检查: http://localhost:8080/health

---

## 数据验证检查清单

### 每次测试后检查

#### ✅ PostgreSQL 验证
- [ ] Trace记录存在
- [ ] ExecutionState正确 (INIT → CONSTRAINT_EVAL → EXECUTING → VALIDATION → FINALIZED/FAILED)
- [ ] Model字段正确
- [ ] Input/Output数据完整
- [ ] Session ID正确
- [ ] Audit signature存在

#### ✅ Redis 验证
- [ ] 预算键正确创建
- [ ] 预算数值合理
- [ ] `budget >= reserved + used` 关系成立

#### ✅ UI 验证
- [ ] UI可访问
- [ ] Trace列表显示正常
- [ ] Trace详情页显示完整
- [ ] 数据与PostgreSQL一致

---

## 常见问题排查

### 问题1: 无法连接到Proxy

```bash
# 检查容器状态
docker-compose ps

# 查看Proxy日志
docker-compose logs sentoris-proxy

# 检查端口是否被占用
lsof -i :8080  # Mac
netstat -ano | findstr :8080  # Windows
```

### 问题2: PostgreSQL连接失败

```bash
# 检查PostgreSQL容器
docker-compose logs postgres

# 尝试直接连接
psql -h localhost -U postgres -d sentoris -c "SELECT 1"

# 检查数据库初始化
ls -la postgres-data/  # 查看数据卷
```

### 问题3: Redis连接失败

```bash
# 检查Redis容器
docker-compose logs redis

# 尝试ping
redis-cli ping

# 检查Redis数据
redis-cli keys "budget:*"
```

### 问题4: Mock LLM不工作

```bash
# 如果需要Mock LLM，单独启动
cd ../sentoris-llm-mock
docker-compose up -d

# 检查Mock LLM是否可访问
curl http://localhost:8081/health
```

### 问题5: 测试脚本依赖问题

```bash
# 重新安装依赖
pip install --upgrade pip
pip install aiohttp psycopg2-binary redis python-dotenv

# 或使用requirements.txt
cat > requirements.txt << EOF
aiohttp>=3.9.0
psycopg2-binary>=2.9.0
redis>=5.0.0
python-dotenv>=1.0.0
EOF
pip install -r requirements.txt
```

---

## 性能测试

### 基础性能测试

```bash
# 使用ab工具进行简单的负载测试
ab -n 100 -c 10 -p payload.json -T application/json \
  -H "Authorization: Bearer test-token" \
  http://localhost:8080/v1/chat/completions
```

payload.json:
```json
{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 50
}
```

### 长时间稳定性测试

```bash
# 运行自动化测试循环
while true; do
  python e2e_test_runner.py --basic
  sleep 60
done
```

---

## 测试结果报告

### 自动化测试结果

运行测试后，结果将保存到 `e2e_test_results.json`:

```json
[
  {
    "scenario_id": "E2E-001",
    "scenario_name": "E2E-001: 非流式基础请求",
    "status": "passed",
    "duration": 1.23,
    "details": {
      "trace_id": "trace-xxx",
      "postgres_check": true
    }
  }
]
```

### 手动测试报告模板

```markdown
# 手动测试报告

**测试日期**: 2024-05-02  
**测试人员**: Your Name  
**环境版本**: v1.0.0

## 测试结果摘要

| 场景 | 状态 | 备注 |
|------|------|------|
| E2E-001 | ✅ Passed | |
| E2E-100 | ✅ Passed | |
| E2E-600 | ❌ Failed | 错误信息... |

## 问题记录

### 问题1
- 场景: E2E-600
- 描述: ...
- 复现步骤: ...
- 日志: ...
```

---

## 下一步

- 阅读 [协议文档](../../sentoris-spec/spec/) 了解详细规范
- 查看 [AI.md](../../sentoris-spec/AI.md) 了解指导原则
- 查看 [ARCHITECTURE.md](../ARCHITECTURE.md) 了解系统架构

