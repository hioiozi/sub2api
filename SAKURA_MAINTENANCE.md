# 樱花版维护与发布

官方基线：`3a6fd1c9db07203ca308aaba69e502bc1f35b307`（0.2.15）。二开保留官方 Git 历史；官方仓库作为 `upstream`，个人发布仓库作为 `origin`。不要把生产数据库、CF Token、密钥文件或用户上传内容提交 Git。

## 改动边界

- 前端：独立樱花颜色覆盖；个人仪表盘封面；访问防护；模型检测。官方运维图表、调度、余额扣费和系统设置继续使用官方实现。
- 后端：独立的 `dashboard_cover_*`、`access_protection_*`、`model_detection_*` 文件；用户路由只增加入口；审计日志仅为封面正文与令牌轮转正文增加排除项。
- 无新增数据库迁移。运行数据使用 `/app/data/dashboard-covers`、`/app/data/access-protection`、`/app/data/model-detection`，必须持久挂载。目前只支持单应用实例，文件锁不是跨进程锁。

## 模型检测

分组来自当前用户有效、未过期、有剩余额度且未设置 IP 限制的密钥，每组显示一次。模型列表读取同一密钥的 `/v1/models`；换组清空选择，迟到响应丢弃。下拉支持搜索、手填模型 ID；手填不承诺上游支持，也不会修改分组配置。共享作品展示历史请求 ID，与当前分组候选列表独立。若官方配置模型映射，实际上游模型以官方请求日志为准。

任务使用用户密钥请求本机官方网关，认证、调度及扣费由官方执行。余额需大于零；每用户同时一项、全站四项，八分钟超时，不自动重试计费请求。保留最近二十条，登录用户共享；作者或管理员可删除。糖果问题保留原始回答；HTML 在禁止脚本及外部资源的沙箱中显示。

## 访问防护

累计计额 = 官方 `total_recharged` + 已使用的正数 `admin_balance` 记录。余额兑换码已包含于官方累计值，不再次相加。消费、负向调整不减累计值；当前每满 100 个系统余额单位取得一个名额，不作人民币汇率换算。官方管理员加款记录若曾写入失败或被删除，该部分历史无法从当前余额反推。

只对指定中国地区阻断规则追加 IP 排除，不跳过其他 WAF/DDoS 规则。站点最多 200 条记录，表达式最多 4000 字节。规则被外部修改后暂停写入，需要管理员核对。配置必须与生产规则对应，不能照搬 staging 的规则 ID、基线表达式或状态文件。

服务端配置：`CLOUDFLARE_ZONE_ID`、`CLOUDFLARE_RULESET_ID`、`CLOUDFLARE_RULE_ID`、`CLOUDFLARE_BASE_EXPRESSION`、`CLOUDFLARE_IP_TOKEN_FILE`、`CLOUDFLARE_IP_TOKEN_KEY_FILE`。首次部署单独准备密文及 AES 密钥，密钥只读挂载；后续管理员在访问防护页面轮转 Token。Token 验证指定规则读写权限后以 AES-GCM 加密保存，前端不回显。`ACCESS_TRUSTED_PROXY_CIDRS` 只能列受信任、会清洗 `X-Real-IP` 的直接代理地址。

## 跟随官方更新

```sh
git fetch upstream
git switch -c codex/update-official
git merge upstream/main
```

优先审查路由、中间件、导航、i18n、Dashboard、余额累计与密钥 DTO 的冲突。推荐完整合并官方安全修复，再协调 UI；仅挑后端提交可能遗漏前后端契约及数据库迁移。

`sakura.css` 是颜色覆盖，部分选择器有 Vue scoped hash。官方更新后它不会自动适配新 hash。先从干净官方版本构建得到主 CSS，记录 SHA256；执行 `node deploy/sakura/generate-palette.cjs 官方CSS路径 SHA256`，再核对浅色、深色、下拉、仪表盘与运维状态色。生成器只允许颜色属性，不改尺寸布局。

`sakura-controls.css` 使用官方 Select 的稳定类名，覆盖异步组件样式的浅色、深色、焦点与选中颜色；不修改官方 Select 实现。官方组件类名变化时需要同步核对。

前端使用官方 Dockerfile 固定的 pnpm 9 与原锁文件，避免 pnpm 新大版本改写锁文件：

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm run build
pnpm exec vitest run src/views/user/__tests__/ModelDetectionView.spec.ts src/components/user/dashboard/__tests__/coverDrag.spec.ts src/components/user/dashboard/__tests__/coverPreview.spec.ts src/utils/__tests__/modelDetection.spec.ts
cd ../backend
go test ./internal/handler ./internal/server/routes ./internal/service
```

## 德国生产发布准备

现有 Compose：`/data/sub2api-deploy/docker-compose.yml`；应用数据：`/data/sub2api-deploy/data` → `/app/data`。保留现有数据库、Redis、JWT、网络及卷配置，只替换应用镜像并增加扩展所需环境变量与秘密挂载。先核对实际 Compose 服务名、运行 UID 与目录写权限，固定新镜像 digest；记录当前镜像 digest，作为回滚目标。

本次准备阶段只部署香港测试站，德国尚未切换。正式切换前仍需完成生产专属 CF 配置、核对代理真实 IP 链路、管理员计额与迁移计划。先核实无正在计费的模型检测任务，再切换应用；健康检查、登录、用户权限、余额/密钥页面和网关连通性通过后开放。

准备脚本：`python deploy/sakura/prepare-production.py Linux二进制路径 --host sub2new`。仅上传二进制并构建官方镜像派生版本，生成固定镜像 ID 的 `image.yml`、原镜像 digest 的 `rollback.yml` 与 `rollback.sh`，验证 Compose 配置和脚本语法，确认生产容器 ID 未变；不会执行重启。输出目录是 `/data/sub2api-deploy/releases/sakura-二进制哈希前12位`。正式发布另行合入已核对的生产 CF 环境/秘密挂载；该镜像覆盖文件本身不含 CF 配置。

回滚使用原应用镜像及原 Compose 配置，保留数据库和 `/app/data`。不要使用 `down -v`，不要删除持久目录。若将来合并带数据库迁移的官方更新，先单独评估降级兼容性；应用镜像回滚不会撤回已经生效的 CF IP 例外，需要在规则侧单独处理。
