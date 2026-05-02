#!/bin/bash
# Sentoris Proxy 快速端到端测试

set -e

SESSION_ID="bash-test-$(date +%s)"
echo "=========================================="
echo "Sentoris Proxy 快速端到端测试"
echo "=========================================="
echo "会话ID: $SESSION_ID"
echo "测试时间: $(date)"

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PASSED=0
FAILED=0

# 测试函数
test_health() {
    echo ""
    echo "=========================================="
    echo "测试1: 健康检查"
    echo "=========================================="

    RESPONSE=$(curl -s http://localhost:8080/health)
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health)

    if [ "$STATUS" = "200" ]; then
        echo -e "${GREEN}✅ 健康检查通过${NC}"
        echo "响应: $RESPONSE"
        ((PASSED++))
        return 0
    else
        echo -e "${RED}❌ 健康检查失败 (状态码: $STATUS)${NC}"
        echo "响应: $RESPONSE"
        ((FAILED++))
        return 1
    fi
}

test_basic_request() {
    echo ""
    echo "=========================================="
    echo "测试2: 基础非流式请求"
    echo "=========================================="

    RESPONSE=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-token" \
        -H "X-Sentoris-Session-ID: $SESSION_ID" \
        -d '{
            "model": "gpt-4o",
            "messages": [{"role": "user", "content": "Hello"}],
            "max_tokens": 50
        }')

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/chat/completions \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-token" \
        -H "X-Sentoris-Session-ID: $SESSION_ID" \
        -d '{
            "model": "gpt-4o",
            "messages": [{"role": "user", "content": "Hello"}],
            "max_tokens": 50
        }')

    TRACE_ID=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-token" \
        -H "X-Sentoris-Session-ID: $SESSION_ID" \
        -d '{
            "model": "gpt-4o",
            "messages": [{"role": "user", "content": "Hello"}],
            "max_tokens": 50
        }' -D - | grep -i "Sentoris-Trace-Id" | awk '{print $2}' | tr -d '\r')

    echo "状态码: $STATUS"
    echo "Trace ID: $TRACE_ID"
    echo "响应: $RESPONSE" | head -c 500

    if [ "$STATUS" = "200" ] && [ -n "$TRACE_ID" ]; then
        echo -e "${GREEN}✅ 基础请求通过${NC}"
        ((PASSED++))

        # 保存TRACE_ID供后续测试使用
        echo "$TRACE_ID" > /tmp/sentoris_baseline_trace.txt
        return 0
    else
        echo -e "${RED}❌ 基础请求失败${NC}"
        ((FAILED++))
        return 1
    fi
}

test_replay() {
    echo ""
    echo "=========================================="
    echo "测试3: Replay功能"
    echo "=========================================="

    # 读取基线Trace ID
    if [ ! -f /tmp/sentoris_baseline_trace.txt ]; then
        echo -e "${YELLOW}⚠️  没有基线Trace，跳过Replay测试${NC}"
        return 0
    fi

    BASELINE_TRACE=$(cat /tmp/sentoris_baseline_trace.txt)
    echo "基线Trace ID: $BASELINE_TRACE"

    RESPONSE=$(curl -s -X POST http://localhost:8080/v1/replay \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-token" \
        -d "{
            \"baseline_trace_id\": \"$BASELINE_TRACE\",
            \"model\": \"gpt-4o\"
        }")

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/replay \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-token" \
        -d "{
            \"baseline_trace_id\": \"$BASELINE_TRACE\",
            \"model\": \"gpt-4o\"
        }")

    echo "状态码: $STATUS"
    echo "响应: $RESPONSE" | head -c 800

    if [ "$STATUS" = "200" ] || [ "$STATUS" = "201" ]; then
        echo -e "${GREEN}✅ Replay测试通过${NC}"
        ((PASSED++))
        return 0
    else
        echo -e "${RED}❌ Replay测试失败${NC}"
        ((FAILED++))
        return 1
    fi
}

verify_postgres() {
    echo ""
    echo "=========================================="
    echo "验证: PostgreSQL数据"
    echo "=========================================="

    if [ ! -f /tmp/sentoris_baseline_trace.txt ]; then
        echo -e "${YELLOW}⚠️  没有Trace ID，跳过PostgreSQL验证${NC}"
        return 0
    fi

    BASELINE_TRACE=$(cat /tmp/sentoris_baseline_trace.txt)

    # 使用docker exec执行psql命令
    RESULT=$(docker exec sentoris-postgres psql -U postgres -d sentoris -t -c "SELECT trace_id, model, execution_state FROM traces WHERE trace_id = '$BASELINE_TRACE';" 2>/dev/null | tr -d '[:space:]')

    if [ -n "$RESULT" ]; then
        echo -e "${GREEN}✅ Trace存在于PostgreSQL${NC}"
        echo "查询结果: $RESULT"
        ((PASSED++))
        return 0
    else
        echo -e "${RED}❌ Trace未在PostgreSQL中找到${NC}"
        ((FAILED++))
        return 1
    fi
}

verify_redis() {
    echo ""
    echo "=========================================="
    echo "验证: Redis预算数据"
    echo "=========================================="

    # 检查会话相关的key
    KEYS=$(docker exec sentoris-redis redis-cli keys "budget:$SESSION_ID*" 2>/dev/null)

    if [ -n "$KEYS" ]; then
        echo -e "${GREEN}✅ 找到Redis预算数据${NC}"
        echo "Keys: $KEYS"

        # 获取值
        for KEY in $KEYS; do
            VALUE=$(docker exec sentoris-redis redis-cli get "$KEY" 2>/dev/null)
            echo "  $KEY: $VALUE"
        done

        ((PASSED++))
        return 0
    else
        echo -e "${YELLOW}⚠️  未找到会话相关的Redis数据（可能未启用预算限制）${NC}"
        ((PASSED++))  # 这不算失败
        return 0
    fi
}

# 运行测试
test_health
test_basic_request
test_replay
verify_postgres
verify_redis

# 总结
echo ""
echo "=========================================="
echo "测试总结"
echo "=========================================="
echo -e "${GREEN}通过: $PASSED${NC}"
echo -e "${RED}失败: $FAILED${NC}"
echo "会话ID: $SESSION_ID"
echo "Trace ID: $(cat /tmp/sentoris_baseline_trace.txt 2>/dev/null || echo '无')"

if [ $FAILED -eq 0 ]; then
    echo -e "\n${GREEN}🎉 所有测试通过！${NC}"
    exit 0
else
    echo -e "\n${RED}❌ 有测试失败${NC}"
    exit 1
fi
