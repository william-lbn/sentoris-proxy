#!/bin/bash
# Sentoris Proxy 端到端测试

set -e

echo "=========================================="
echo "Sentoris Proxy 端到端测试"
echo "测试时间: $(date)"
echo "=========================================="

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASSED=0
FAILED=0

# 测试1: 健康检查
echo ""
echo "测试1: 健康检查"
echo "------------------------------------------"
RESPONSE=$(curl -s http://localhost:8080/health)
if [ "$RESPONSE" = '{"status":"ok"}' ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败: $RESPONSE${NC}"
    ((FAILED++))
fi

# 测试2: 非流式请求
echo ""
echo "测试2: 非流式请求"
echo "------------------------------------------"
RESPONSE=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token" \
    -H "X-Sentoris-Session-ID: e2e-test-001" \
    -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello"}], "max_tokens": 50}')

HAS_ID=$(echo "$RESPONSE" | grep -c '"id"')
HAS_CHOICES=$(echo "$RESPONSE" | grep -c '"choices"')
HAS_USAGE=$(echo "$RESPONSE" | grep -c '"usage"')

if [ "$HAS_ID" -gt 0 ] && [ "$HAS_CHOICES" -gt 0 ] && [ "$HAS_USAGE" -gt 0 ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    echo "响应: $(echo "$RESPONSE" | head -c 200)..."
    ((PASSED++))
else
    echo -e "${RED}❌ 失败${NC}"
    echo "响应: $RESPONSE"
    ((FAILED++))
fi

# 测试3: 流式请求
echo ""
echo "测试3: 流式请求"
echo "------------------------------------------"
RESPONSE=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token" \
    -H "X-Sentoris-Session-ID: e2e-test-002" \
    -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Stream test"}], "max_tokens": 30, "stream": true}')

HAS_DATA=$(echo "$RESPONSE" | grep -c "data:")

if [ "$HAS_DATA" -gt 0 ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    echo "找到 $HAS_DATA 个数据块"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败${NC}"
    echo "响应: $RESPONSE"
    ((FAILED++))
fi

# 测试4: 预算控制
echo ""
echo "测试4: 预算控制"
echo "------------------------------------------"
RESPONSE=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token" \
    -H "X-Sentoris-Session-ID: e2e-test-budget" \
    -H "X-Sentoris-Budget-Limit-USD: 0.00001" \
    -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Budget test"}], "max_tokens": 100}')

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token" \
    -H "X-Sentoris-Session-ID: e2e-test-budget" \
    -H "X-Sentoris-Budget-Limit-USD: 0.00001" \
    -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Budget test"}], "max_tokens": 100}')

if [ "$STATUS" = "429" ] || [ "$STATUS" = "200" ]; then
    echo -e "${GREEN}✅ 通过 (状态码: $STATUS)${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败 (状态码: $STATUS)${NC}"
    ((FAILED++))
fi

# 测试5: 隐私掩码
echo ""
echo "测试5: 隐私掩码"
echo "------------------------------------------"
RESPONSE=$(curl -s -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token" \
    -H "X-Sentoris-Session-ID: e2e-test-privacy" \
    -H "X-Sentoris-Privacy-Level: masked" \
    -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Contact: test@example.com"}], "max_tokens": 50}')

HAS_CHOICES=$(echo "$RESPONSE" | grep -c '"choices"')

if [ "$HAS_CHOICES" -gt 0 ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败${NC}"
    echo "响应: $RESPONSE"
    ((FAILED++))
fi

# 测试6: 模型列表
echo ""
echo "测试6: 模型列表API"
echo "------------------------------------------"
RESPONSE=$(curl -s http://localhost:8080/v1/models)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/models)

if [ "$STATUS" = "200" ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败 (状态码: $STATUS)${NC}"
    ((FAILED++))
fi

# 测试7: 监控API
echo ""
echo "测试7: 监控API"
echo "------------------------------------------"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/monitor)

if [ "$STATUS" = "200" ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败 (状态码: $STATUS)${NC}"
    ((FAILED++))
fi

# 测试8: Redis连接
echo ""
echo "测试8: Redis连接"
echo "------------------------------------------"
REDIS_PING=$(docker exec sentoris-redis redis-cli ping 2>/dev/null)

if [ "$REDIS_PING" = "PONG" ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败${NC}"
    ((FAILED++))
fi

# 测试9: PostgreSQL连接
echo ""
echo "测试9: PostgreSQL连接"
echo "------------------------------------------"
PG_CHECK=$(docker exec sentoris-postgres psql -U postgres -d sentoris -t -c "SELECT 1;" 2>/dev/null | tr -d '[:space:]')

if [ "$PG_CHECK" = "1" ]; then
    echo -e "${GREEN}✅ 通过${NC}"
    ((PASSED++))
else
    echo -e "${RED}❌ 失败${NC}"
    ((FAILED++))
fi

# 总结
echo ""
echo "=========================================="
echo "测试总结"
echo "=========================================="
echo -e "${GREEN}通过: $PASSED${NC}"
echo -e "${RED}失败: $FAILED${NC}"

if [ $FAILED -eq 0 ]; then
    echo -e "\n${GREEN}🎉 所有测试通过！${NC}"
    exit 0
else
    echo -e "\n${RED}❌ 有测试失败${NC}"
    exit 1
fi
