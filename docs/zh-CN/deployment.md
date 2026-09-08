# 部署

> 本中文文档仅供参考，请以英文文档为准。
> Chinese translation is for reference only; the English documentation is authoritative.

## 构建

### 从源码构建

```bash
go build -o bin/tickraft ./cmd/tickraft
```

或使用 Makefile：

```bash
make build
```

二进制文件输出到 `bin/tickraft`。

### 交叉编译

```bash
make cross-build
```

支持的目标平台：`linux/amd64`、`linux/arm64`、`windows/amd64`、`darwin/amd64`、`darwin/arm64`。

## 运行

开源版仅以独立模式运行 —— 单个进程在同一个 HTTP 端口上提供 API、SPA、scheduler、executor、telemetry 以及告警引擎。

```bash
./bin/tickraft start --config config.yaml
```

当省略 `--config` 时，会使用内置的默认配置（SQLite、一个开发用的 JWT 密钥）。该配置仅适用于本地试验。默认监听地址为 `:6153`，可通过配置文件中的 `server.addr` 修改。

### 准备配置文件

复制示例文件并根据你的环境进行编辑：

```bash
cp configs/config.example.yaml config.yaml
```

配置文件采用 YAML 格式，并支持环境变量插值，以便将敏感信息（数据库 DSN、JWT 密钥、管理员密码）从环境注入，而不是写入磁盘。每一个字段的说明请参见 [配置](./configuration.md)。

### 校验配置

启动服务器前先校验配置文件 —— 这一步会检查环境变量插值、应用默认值，并校验每一个字段：

```bash
./bin/tickraft config validate -c config.yaml
```

## Docker

### 构建镜像

```bash
make docker
```

### 运行容器

运行刚构建的本地镜像（与 [README](../../README.md) 快速上手一致）：

```bash
docker run -d \
  --name tickraft \
  -p 6153:6153 \
  -v tickraft-data:/app/data \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -e TICKRAFT_JWT_SECRET="your-secret-key" \
  -e TICKRAFT_ADMIN_PASSWORD="your-admin-password" \
  tickraft-ce:latest start --config /app/config.yaml
```

发布构建由发布流水线推送至 `ghcr.io/tickraft/tickraft-ce`；将该镜像标签替换为发布镜像即可运行发布版而非本地构建。

### docker-compose

```yaml
version: '3'
services:
  tickraft:
    image: tickraft-ce:latest
    command: ["start", "--config", "/app/config.yaml"]
    ports:
      - "6153:6153"
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml
    environment:
      - TICKRAFT_JWT_SECRET=your-secret-key
      - TICKRAFT_ADMIN_PASSWORD=your-admin-password
      - TICKRAFT_DB_DSN=sqlite:///app/data/tickraft.db
    restart: unless-stopped
```

## 单端口路由

一个 Hertz 引擎在 `server.addr` 上提供所有协议的服务。路由按路径前缀进行划分，因此不会出现端口冲突：

| 路径前缀      | 服务                | 鉴权方式                | 说明                                       |
|---------------|---------------------|-------------------------|--------------------------------------------|
| `/api/v1/*`   | JSON API            | JWT 中间件              | 任务、资产、告警、系统等。                  |
| `POST /api/v1/telemetry` | 遥测数据接入       | Asset-Key 请求头        | 统一遥测上报端点。                          |
| `/healthz`    | 健康探测            | 无（已加入白名单）      | 依赖不可用时返回 503。                      |
| `/`           | SPA 静态资源        | 无                      | Vue 3 前端。                                |

全局中间件（RequestID → AccessLog → Recovery → CORS → TrustedProxy）在每条路径上都会执行；路由组中间件则按前缀分别挂载。

## 摄取 Prometheus exposition 格式

`POST /api/v1/telemetry` 同时接受 Prometheus 文本 exposition 格式（Content-Type `text/plain; version=0.0.4; charset=utf-8`），任何能产出 `.prom` 文件的程序都无需 JSON 包装即可上报指标。鉴权与身份沿用与 JSON 上报相同的模型：

- **签名**（推荐）：`X-Tickraft-Signature` 携带原始请求体的十六进制 HMAC-SHA256，使用全局密钥或按监控点密钥签名；按点密钥匹配时报告同时绑定到该点所属资产。
- **查询串携带资产身份**：`?asset_id=<id>` 或 `?asset_key=<key>&tenant_id=<id>`，在无签名凭据时使用。

序列会拍平进指标映射：标签折叠进键名（`http_requests_total{code="200",method="get"}`），直方图/摘要展开为 `_sum`、`_count`、`_bucket{le="..."}` 与 `{quantile="..."}` 序列。每次推送按一个事件计入每日配额，单次上限 64 KiB。

```bash
curl -X POST "http://localhost:6153/api/v1/telemetry?asset_id=1" \
  -H 'Content-Type: text/plain; version=0.0.4; charset=utf-8' \
  -H "X-Tickraft-Signature: $(openssl dgst -sha256 -hmac "$SECRET" -hex < metrics.prom | cut -d' ' -f2)" \
  --data-binary @metrics.prom
```

### node_exporter textfile 桥接

一行 cron 即可转发 node_exporter textfile 采集目录中的全部文件：

```cron
* * * * * cat /var/lib/node_exporter/textfile_collector/*.prom > /tmp/all.prom && curl -s -X POST "http://tickraft:6153/api/v1/telemetry?asset_id=1" -H 'Content-Type: text/plain; version=0.0.4' -H "X-Tickraft-Signature: $(openssl dgst -sha256 -hmac \"$SECRET\" -hex < /tmp/all.prom | cut -d' ' -f2)" --data-binary @/tmp/all.prom
```

## 系统要求

- **操作系统**：Linux、macOS 或 Windows。
- **架构**：amd64 或 arm64。
- **内存**：空闲时 ≥ 30 MB。
- **磁盘**：建议 ≥ 1 GB（取决于日志和遥测数据量）。
- **数据库**：内嵌 SQLite —— 无需外部数据库服务器。

## 开发模式

### 前端开发服务器

```bash
cd web
pnpm install
pnpm run dev
```

Vite 开发服务器运行在 `http://localhost:5173`，并启用了 mock 数据，因此无需运行后端即可完整交互 UI。

### 后端开发

```bash
go run ./cmd/tickraft start
```

不带 `--config` 时会使用内置默认配置，便于本地快速启动。

## 相关文档

- [配置](./configuration.md) —— 详解每一个配置字段。
- [快速开始](./getting-started.md) —— 五分钟内从零到第一个任务。
- [架构](./architecture.md) —— 分层架构与三模块设计。
- [OpenAPI 规范](../api/openapi.yaml) —— REST API 路径与 schema。
