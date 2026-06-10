## 混合语言 Debug Notes: API 超时问题

日期：2026年4月9日
调试人：Ziyang（with Claude Code）
状态：已解决

### 问题描述

Staging 环境的 `POST /v1/recall` endpoint 在某些查询下出现超时（>5s），导致 Claude Code hook 调用失败。用户在 Discord 和微信群都报告了这个问题。

### 复现步骤

```bash
# 正常查询（<500ms）
curl -X POST https://staging-api.memax.app/v1/recall \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"query": "deployment process", "limit": 5}'

# 超时查询（>5s，有时候直接 504）
curl -X POST https://staging-api.memax.app/v1/recall \
  -d '{"query": "how to set up the development environment with docker and redis for local testing", "limit": 10}'
```

长查询（超过 15 个 token）触发超时的概率明显更高。

### 根因分析

通过 `pprof` 和日志分析，发现瓶颈在三个地方：

1. **Voyage AI embedding 调用**：长查询的 embedding 生成时间从 50ms 增加到 200ms。这个还好，不是主要问题。

2. **Cohere rerank 调用**：这是主要瓶颈。当 candidate pool 超过 50 条时，Cohere rerank API 的 P95 延迟从 150ms 飙到 2000ms+。原来的代码没有限制 candidate pool 大小：

```go
// 之前的代码（有问题）
candidates := pgvectorResults // 可能有 100+ 条
reranked, err := cohere.Rerank(ctx, query, candidates)

// 修复后
candidates := pgvectorResults
if len(candidates) > 30 {
    candidates = candidates[:30] // 截断到 top 30
}
reranked, err := cohere.Rerank(ctx, query, candidates)
```

3. **pg_trgm 全文搜索**：中文查询触发了 pg_trgm 的全表扫描，因为中文字符的 trigram 匹配效果很差。加了一个 `LIMIT 50` 和超时兜底：

```sql
-- 修复前
SELECT id, similarity(content, $1) AS sim FROM chunks
WHERE similarity(content, $1) > 0.1
ORDER BY sim DESC;

-- 修复后（加了 LIMIT 和 statement_timeout）
SET LOCAL statement_timeout = '200ms';
SELECT id, similarity(content, $1) AS sim FROM chunks
WHERE similarity(content, $1) > 0.1 AND owner_id = $2
ORDER BY sim DESC
LIMIT 50;
```

### 修复结果

修复后的延迟对比：

| 查询类型 | 修复前 P95 | 修复后 P95 |
|----------|-----------|-----------|
| 短查询（<10 token） | 320ms | 280ms |
| 长查询（>15 token） | 4800ms | 520ms |
| 中文查询 | 3200ms | 480ms |

### 遗留问题

- pg_trgm 对中文的支持本质上不好，长期需要考虑用 pgroonga 或者应用层中文分词
- Cohere rerank 的降级策略还没做完——如果 Cohere API 挂了应该直接返回 pgvector 结果
- 需要给 recall endpoint 加一个全链路 timeout（建议 2s），避免任何单个环节的慢查询拖垮整个请求
