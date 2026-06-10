## Onboarding 新人文档 — 开发环境搭建

最后更新：2026年4月5日
维护人：Jiahao

本文档面向新加入 Memax 团队的开发者，指导如何从零搭建本地开发环境。

### 前置要求

- macOS 或 Linux（Windows 用户建议使用 WSL2）
- Node.js >= 20（推荐用 fnm 管理版本）
- Go >= 1.22
- Docker Desktop 或 OrbStack（macOS 推荐 OrbStack，更省资源）
- pnpm >= 9（通过 `corepack enable` 安装）

### 第一步：克隆仓库并安装依赖

```bash
git clone https://github.com/MemaxLabs/memax-internal.git
cd memax-internal
pnpm install
```

如果 `pnpm install` 报错 `EACCES`，检查是否用了 sudo 安装的 Node.js。建议用 fnm 重新安装。

### 第二步：启动本地基础设施

```bash
# 启动 PostgreSQL + Redis + MinIO
docker compose up -d

# 检查服务状态
docker compose ps
```

正常情况下应该看到三个容器都是 `running` 状态。

**常见问题：**
- 端口冲突：默认用 5432（PostgreSQL）、6379（Redis）、9000（MinIO）。如果被占用，修改 `docker-compose.yml` 中的端口映射。
- 首次启动慢：PostgreSQL 需要初始化 pgvector 扩展，大概要 30 秒。

### 第三步：配置环境变量

```bash
cp .env.example .env
```

必须填写的变量：
- `DATABASE_URL` — 默认 `postgres://postgres:postgres@localhost:5432/memax`
- `REDIS_URL` — 默认 `redis://localhost:6379`
- `VOYAGE_API_KEY` — 从 https://www.voyageai.com 获取（免费层足够开发使用）

可选变量（不填也能跑）：
- `COHERE_API_KEY` — rerank 功能需要，没有的话 recall 会跳过 rerank
- `ANTHROPIC_API_KEY` — dream engine 和分类需要，没有的话这些功能会禁用
- `R2_*` — MinIO 在本地模拟 R2，默认配置已在 `.env.example` 中

### 第四步：启动开发服务

```bash
# 方式一：全部启动（推荐）
pnpm dev

# 方式二：分别启动
pnpm --filter @memaxlabs/server dev   # Go API server (localhost:8080)
pnpm --filter @memaxlabs/web dev      # Next.js web app (localhost:3000)
```

数据库 migration 会在 server 启动时自动执行。如果 migration 失败（dirty state），参考下面的恢复方法。

### 数据库问题恢复

如果遇到 migration dirty 状态：

```bash
# 查看当前 migration 版本
psql $DATABASE_URL -c "SELECT * FROM schema_migrations;"

# 如果 dirty=true，手动修复
psql $DATABASE_URL -c "UPDATE schema_migrations SET dirty=false;"

# 或者最简单的方法：重置数据库
docker compose down -v && docker compose up -d
```

### 第五步：验证环境

1. 打开 http://localhost:3000 应该看到 Memax web app 登录页
2. 用 GitHub OAuth 登录（需要在 GitHub 注册一个 OAuth App，redirect URI 设为 `http://localhost:3000/api/auth/callback/github`）
3. 在终端运行 `memax recall "test"` 验证 CLI 连通性

### 日常开发提示

- `pnpm format && pnpm lint` 每次提交前必须运行
- Go 代码修改后 server 会自动热重载（用了 air）
- Next.js 的 Fast Refresh 有时会失效，刷新浏览器即可
- 如果 Redis 出问题，可以直接 `redis-cli FLUSHALL` 清缓存
