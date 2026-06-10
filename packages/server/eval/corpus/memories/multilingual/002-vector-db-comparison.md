## 技术选型：向量数据库对比

最后更新：2026年3月18日

### 背景

Memax 需要一个向量数据库来存储和检索 memory embeddings。核心需求：

1. 支持高维向量（1024 维，Voyage AI 输出）
2. 支持混合检索（向量相似度 + 全文搜索）
3. 运维成本可控，适合早期创业团队
4. 延迟在 100ms 以内（不含 rerank）

### 方案对比

#### pgvector（PostgreSQL 扩展）

**优点：**
- 与现有 PostgreSQL 基础设施复用，无需额外运维
- 支持 HNSW 和 IVFFlat 索引，HNSW 在 10 万级数据量下性能优秀
- 天然支持事务和 RLS（Row-Level Security），安全模型简单
- 可以在同一个查询中组合向量搜索和 SQL 条件过滤（owner_id, hub_id）
- Neon 原生支持，无需自建

**缺点：**
- 大规模（百万级以上）性能不如专用方案
- 缺少动态分片，扩展性受限于单节点
- HNSW 索引构建速度较慢

#### Pinecone

**优点：**
- 全托管，零运维
- 亚毫秒级查询延迟
- 支持 namespace 和 metadata 过滤

**缺点：**
- 成本高昂（$70/mo 起步，按 pod 计费）
- 无法与 PostgreSQL 事务组合，需要双写一致性方案
- 增加了系统复杂度（需要额外维护同步逻辑）
- 供应商锁定风险

#### Milvus

**优点：**
- 开源，可自建
- 支持多种索引类型，性能强劲
- 社区活跃，文档丰富

**缺点：**
- 运维复杂度高（需要 etcd、MinIO、Pulsar 等组件）
- 早期团队缺少运维人力
- 与 PostgreSQL 分离，同样有双写一致性问题

### 决策：选择 pgvector

**核心理由：**

1. **运维简洁性**：复用现有 Neon PostgreSQL，不引入新的基础设施组件
2. **安全模型**：owner_id 过滤和未来的 RLS 策略可以直接在数据库层面执行，不依赖应用层
3. **成本**：Neon 的免费层足够早期使用，Pro 计划（$19/mo）可以支撑到 10 万用户
4. **混合检索**：配合 pg_trgm 可以在同一查询中实现向量 + 关键词的混合检索

**风险缓解：**
- 如果 pgvector 在百万级数据量下性能不足，可以迁移到 Pinecone 或引入 pgvectorscale
- embedding 存储接口已经抽象化，迁移成本可控

**基准测试结果（10 万条 1024 维向量）：**
- pgvector HNSW：P50 = 12ms，P95 = 38ms，Recall@10 = 0.95
- Pinecone：P50 = 5ms，P95 = 15ms，Recall@10 = 0.98
- Milvus IVF_SQ8：P50 = 8ms，P95 = 22ms，Recall@10 = 0.93

pgvector 的延迟在可接受范围内，recall 也足够好。

决策人：Ziyang，2026年3月15日
