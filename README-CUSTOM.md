# sub2api 魔改版（基于 ranxi2001/sub2api v2.8.18）

本仓库是上游 [ranxi2001/sub2api](https://github.com/ranxi2001/sub2api) **v2.8.18** 的个人补丁合集，
用于你自己的部署；与上游保持同样的配置方式，只增加/调整了下列能力（详见文末）。

> 使用前请自行备份数据库（`docker exec <postgres容器> pg_dump -U sub2api -d sub2api | gzip > backup.sql.gz`）。

---

## 一、一键更新（推荐：用自动构建的镜像）

本仓库 `main` 分支每次推送都会自动构建镜像并推送到 GitHub 容器仓库（GHCR）：

```
ghcr.io/edmundmad0309/sub2api-custom:latest
```

**更新步骤（Docker Compose 部署）：**

1. 拉新镜像：
   ```bash
   docker pull ghcr.io/edmundmad0309/sub2api-custom:latest
   ```
2. 把 `docker-compose.yml` 里 `sub2api` 服务的 `image:` 改成上面这行（原来可能是 `sub2api:xxx` 或 `ghcr.io/ranxi2001/sub2api:xxx`）。
3. 重建并重启：
   ```bash
   docker compose up -d --no-deps sub2api
   ```
4. 查看状态：
   ```bash
   docker compose ps
   docker logs --tail 50 <sub2api容器名>
   ```

### 交给 AI 的指令模板

> 帮我更新 sub2api：把 docker-compose.yml 里 sub2api 的镜像改为
> `ghcr.io/edmundmad0309/sub2api-custom:latest`，先备份数据库，再 `docker compose up -d --no-deps sub2api`，
> 完成后检查容器健康状态与日志。参考仓库里的 `README-CUSTOM.md`。

---

## 二、全新部署

1. 克隆本仓库：
   ```bash
   git clone https://github.com/EdmundMad0309/sub2api-custom.git
   cd sub2api-custom
   ```
2. 按上游 `deploy/` 目录下的说明准备 `.env`、数据库与 Redis；
3. 把 compose 里 `sub2api` 的镜像换成 `ghcr.io/edmundmad0309/sub2api-custom:latest`（或本仓库源码自建，见下）；
4. `docker compose up -d`。

---

## 三、从源码自行构建

```bash
# 前端 + 后端（Docker 多阶段构建，与官方一致）
docker build -t sub2api:custom .

# 或分开构建：
# 前端：cd frontend && pnpm install && pnpm run build
# 后端：cd backend && CGO_ENABLED=0 go build -tags embed -o sub2api ./cmd/server
```

---

## 四、与上游 v2.8.18 的差异（本地补丁）

- **质量运维多动作**：仅记录 / 六系分流（答错摘 6 系、通过恢复）/ Excel 模式（答错开 Excel+BPS，通过不关；BPS 403 时摘该账号 6 系）。
- **凭据守护**（智能运维子页面）：令牌巡检 / 失效自动重登 / 错误态自愈；重登凭据每行 `邮箱----密码----2FA密钥`。
- **Excel/BPS**：BPS 403 时可在同一请求内回退标准端点（账号开关 `openai_excel_bps_fallback_on_403`，由 Excel 模式自动开启）。
- **网关兼容**：reasoning 模型采样参数剥离、`function_call` namespace 保留（OAuth/Codex）、响应模型名掩码、Codex 节点观测、Bedrock `output_config` 处理、账号级模型 denylist 等。
- 判题「只比对两个值」等改动上游 v2.8.15 已合并，本仓库不再重复。

> 本仓库不含任何服务器地址、密钥或个人配置；所有敏感值都在你自己的 `.env` 与后台设置里。
