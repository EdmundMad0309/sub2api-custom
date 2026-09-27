# sub2api 魔改版（基于 ranxi2001/sub2api v2.8.18）

本仓库是上游 [ranxi2001/sub2api](https://github.com/ranxi2001/sub2api) **v2.8.18** 的个人补丁合集，**纯源码仓库**：

- 不含镜像、不含构建产物，不做自动构建；
- 不含任何服务器地址、密钥或个人配置（所有敏感值都在你自己的 `.env` 与后台设置里）；
- 使用前请先备份数据库。

---

## 一、怎么用（自己构建部署）

1. 克隆仓库：

   ```bash
   git clone https://github.com/EdmundMad0309/sub2api-custom.git
   cd sub2api-custom
   ```

2. 构建镜像（多阶段构建，与官方一致，需要 Docker）：

   ```bash
   docker build -t sub2api:custom .
   ```

3. 按上游 `deploy/` 目录里的说明准备 `.env`、PostgreSQL、Redis，并把 `docker-compose.yml` 里 `sub2api` 服务的 `image:` 改成 `sub2api:custom`；

4. 启动：

   ```bash
   docker compose up -d
   ```

也可以不用 Docker，直接源码构建：

```bash
# 前端
cd frontend && pnpm install && pnpm run build
# 后端（把前端产物嵌进二进制）
cd ../backend && CGO_ENABLED=0 go build -tags embed -o sub2api ./cmd/server
```

### 交给 AI 的指令模板

> 这是我的 sub2api 源码仓库：https://github.com/EdmundMad0309/sub2api-custom
>
> 帮我在这台机器上部署：
> 1. `git clone` 这个仓库；
> 2. `docker build -t sub2api:custom .`；
> 3. 按仓库 `deploy/` 里的说明配置 `.env`、数据库和 Redis；
> 4. 把 compose 里的 sub2api 镜像指向 `sub2api:custom`；
> 5. `docker compose up -d`，然后检查健康状态与日志。
>
> 其他说明见仓库里的 README-CUSTOM.md。

---

## 二、与上游 v2.8.18 的差异（本地补丁）

- **质量运维多动作**：仅记录 / 六系分流（答错摘 6 系、通过恢复）/ Excel 模式（答错开 Excel+BPS，通过不关；BPS 403 时摘该账号 6 系）。
- **凭据守护**（智能运维子页面）：令牌巡检 / 失效自动重登 / 错误态自愈；重登凭据每行 `邮箱----密码----2FA密钥`。
- **Excel/BPS**：BPS 403 时可在同一请求内回退标准端点（账号开关 `openai_excel_bps_fallback_on_403`，由 Excel 模式自动开启）。
- **网关兼容**：reasoning 模型采样参数剥离、`function_call` namespace 保留（OAuth/Codex）、响应模型名掩码、Codex 节点观测、Bedrock `output_config` 处理、账号级模型 denylist 等。
- 判题「只比对两个值」等改动上游 v2.8.15 已合并，本仓库不再重复。
