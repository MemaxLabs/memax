## 团队周会笔记 — 4月第一周

日期：2026年4月7日（周一）
参会：Jiahao（主持）、Ziyang、Sarah、Mira

### 本周进度

**Ziyang：**
- 完成了 recall endpoint 的混合检索重构，pgvector + pg_trgm 双路检索已上线 staging
- Cohere rerank 集成基本完成，但遇到了延迟问题（P95 达到 800ms，目标是 500ms 以内）
- 开始做 MCP OAuth consent screen 的重新设计，参考了 GitHub OAuth 的交互模式

**Jiahao：**
- River 队列的 worker 部署已迁移到独立 Fly.io 机器，解决了之前内存不足的问题
- Dream engine 的第一版 prompt 完成，正在用小规模语料测试效果
- 审核了 Sarah 的用户反馈整理，确认了搜索准确性是用户最关心的问题

**Sarah：**
- 完成了3月用户反馈的整理和分类，共收集了 47 条反馈
- 搜索准确性相关的反馈占比最高（34%），其次是响应速度（21%）
- 开始做 onboarding funnel 的数据分析，PostHog 埋点已就位

**Mira：**
- docs-site（memax.dev）的 Fumadocs 迁移进度约 70%
- API reference 的 Scalar 集成遇到了样式冲突，需要自定义主题
- 计划下周完成 CLI 命令文档的自动生成

### 阻塞问题

1. Cohere rerank API 在高并发下响应时间不稳定，需要考虑降级策略或本地 reranker
2. Fly.io staging 环境的 TLS 证书续期失败，已提交 support ticket
3. Voyage AI 的 embedding 批处理 API 偶尔返回 429（rate limit），需要实现退避重试

### 决策

- **搜索延迟目标调整**：将 P95 目标从 500ms 放宽到 600ms，因为 rerank 带来的准确性提升值得额外 100ms
- **Dream engine 上线节奏**：先在 Ziyang 的个人 hub 上灰度测试两周，再推广到 team hub
- **中文搜索优化**：确认 Voyage AI 的 multilingual 模型在中文查询上的表现可以接受，暂不需要额外的中文分词处理

### 下周计划

- Ziyang：完成 consent screen 重构，开始 API rate limiting 实现
- Jiahao：Dream engine 灰度上线，处理 Cohere 降级策略
- Sarah：完成 onboarding funnel 分析报告，开始定价策略调研
- Mira：完成 docs-site 迁移，修复 Scalar 样式问题
