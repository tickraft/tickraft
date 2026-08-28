# 配置

> 本中文文档仅供参考，请以英文文档为准。
> Chinese translation is for reference only; the English documentation is authoritative.

Tickraft 通过单个 YAML 文件进行配置。`start` 命令在启动时加载该文件一次；每个子系统（API 服务器、scheduler、executor、telemetry、告警引擎）都从同一文件中读取自己所需的段落。

完整的带注释示例位于 [`configs/config.example.yaml`](../../configs/config.example.yaml)。复制一份并根据你的环境进行编辑：

```bash
cp configs/config.example.yaml config.yaml
```

## 环境变量插值

敏感值 —— 数据库 DSN、JWT 密钥、管理员密码 —— 应当从环境注入，而不是写入磁盘。配置加载器支持两种插值形式：

| 形式              | 行为                                                                |
|-------------------|---------------------------------------------------------------------|
| `${VAR}`          | 展开为 `VAR` 的值。引用未设置且没有默认值的变量会导致加载错误。     |
| `${VAR:-default}` | 展开为 `VAR` 的值；当 `VAR` 未设置时展开为 `default`。              |

示例：

```yaml
database:
  dsn: ${TICKRAFT_DB_DSN}
auth:
  jwt_secret: ${TICKRAFT_JWT_SECRET:-change-me-in-production}
  admin_password: ${TICKRAFT_ADMIN_PASSWORD}
```

## 启动前校验

启动服务器前先校验配置文件 —— 校验器会执行插值、应用默认值，并进行字段级检查：

```bash
./bin/tickraft config validate -c config.yaml
```

## 配置段落

### `server` —— HTTP API 服务器

运行时以单端口模式运行：REST API、SPA 与遥测上报端点共享 `server.addr`。不会绑定额外端口。

| 字段                  | 类型     | 默认值    | 说明                                                               |
|-----------------------|----------|-----------|--------------------------------------------------------------------|
| `addr`                | string   | `:6153`   | API、SPA 和健康探测的 HTTP 监听地址。                              |
| `enable_cors`         | bool     | `true`    | 是否启用 CORS 中间件。                                             |
| `enable_access_log`   | bool     | `true`    | 是否启用访问日志中间件。                                           |
| `max_header_bytes`    | int      | `1048576` | 请求头的最大字节数。                                               |
| `read_timeout`        | duration | `0s`      | 读取整个请求的最长耗时。`0s` = 不超时；生产环境建议显式配置（如 `10s`）。 |
| `write_timeout`       | duration | `0s`      | 写入超时前的最长耗时。`0s` = 不超时；生产环境建议显式配置（如 `30s`）。   |
| `maintenance_interval` | duration | `5m`      | 后台维护扫描之间的间隔（例如清理过期 token 黑名单条目）。          |

### `worker` —— worker 运行时

worker 是一种统一的部署模式。它始终在进程内同时启动 scheduler、executor 和 telemetry；无需任何角色参数，也不会绑定额外端口。

| 字段            | 类型     | 默认值 | 说明                                                              |
|-----------------|----------|--------|-------------------------------------------------------------------|
| `concurrence`   | int      | `0`    | 并发执行任务的最大数量。`0` = 自动取 `GOMAXPROCS*2`。             |
| `probe_timeout` | duration | `5s`   | prober executor 的默认超时时间。                                  |

### `prism` —— 告警引擎

告警引擎在进程内运行，不监听额外端口。

| 字段             | 类型     | 默认值 | 说明                                                              |
|------------------|----------|--------|-------------------------------------------------------------------|
| `eval_interval`  | duration | `30s`  | 告警规则求值之间的间隔。                                          |
| `concurrence`    | int      | `8`    | 发送通知的 goroutine 池大小。`0` = 同步发送。                     |

### `database` —— 数据库连接

开源版仅支持 SQLite。驱动根据 DSN 的 scheme 推导：`sqlite://` 或 `sqlite3://`（出于向后兼容，裸文件路径如 `tickraft.db` 也可被接受）。连接池调优通过 DSN 查询参数完成（例如 `sqlite:///path?_max_open_conns=10&_max_idle_conns=5`），而不是通过结构化字段。

| 字段  | 类型   | 默认值 | 说明                                   |
|-------|--------|--------|----------------------------------------|
| `dsn` | string | —      | 数据源名称。请使用环境变量插值。       |

```yaml
database:
  dsn: "sqlite:///app/data/tickraft.db"
```

### `auth` —— JWT 与内置 admin 用户

| 字段             | 类型     | 默认值  | 说明                                                                |
|------------------|----------|---------|---------------------------------------------------------------------|
| `jwt_secret`     | string   | —       | 用于签发 JWT token 的密钥。**必须设置。** 请使用环境变量插值。      |
| `access_ttl`     | duration | `2h`    | 签发的 JWT access token 的有效期。                                  |
| `refresh_ttl`    | duration | `168h`  | 签发的 JWT refresh token 的有效期。                                 |
| `admin_username` | string   | `admin` | 内置 admin 用户名。                                                 |
| `admin_password` | string   | —       | 内置 admin 密码。为空时，启动时会生成一个随机密码并记录一次日志。请使用环境变量插值。 |

### `logger` —— 日志

| 字段             | 类型   | 默认值  | 说明                                                                |
|------------------|--------|---------|---------------------------------------------------------------------|
| `level`          | string | `info`  | 日志级别：`debug`、`info`、`warn` 或 `error`。                      |
| `mode`           | string | `debug` | 日志模式：`debug`（开发）或 `release`（生产）。                     |
| `retention_days` | int    | `30`    | 日志文件保留天数，超过后轮转删除。                                  |

### `i18n` —— 国际化

内核内置了 `zh-Hans`（默认）和 `en-US` 的语言包。

| 字段                 | 类型     | 默认值              | 说明                                                                |
|----------------------|----------|---------------------|---------------------------------------------------------------------|
| `default_locale`     | string   | `zh-Hans`           | 当未找到精确匹配时使用的回退语言。必须是合法的 BCP 47 标签。        |
| `supported_locales`  | []string | `[zh-Hans, en-US]`  | 通过 `GET /api/v1/i18n/locales` 暴露的语言列表。当下游语言包注册后可扩展。 |

## 单端口路由

由于所有服务共享 `server.addr`，路由按路径前缀进行划分：

| 路径                           | 服务               | 鉴权方式                          |
|--------------------------------|--------------------|-----------------------------------|
| `POST /api/v1/auth/login`      | 登录               | 无（公开）                        |
| `POST /api/v1/auth/refresh`    | 刷新令牌           | 无（公开）                        |
| `GET /api/v1/i18n/locales`     | 语言列表           | 无（公开）                        |
| `POST /api/v1/telemetry`       | 遥测数据接入       | `X-Tickraft-Asset-Key`            |
| `/api/v1/*`（其余全部）        | JSON API           | JWT 中间件                        |
| `GET /ws`                      | WebSocket 推送     | 查询参数令牌（handler 内校验）    |
| `GET /healthz`、`GET /readyz`  | 健康探测           | 无（已加入白名单）                |
| `/`                            | SPA 静态资源       | 无                                |

## 开源版配额

开源版实施软配额，以保证单进程的资源占用可预期。可通过重新编译源码来解除这些限制。

| 资源                | 配额    |
|---------------------|---------|
| 监控资产            | 20      |
| prober              | 20      |
| 调度任务            | 20      |
| 自愈动作            | 5       |
| HTTP 探测间隔       | 60 s    |
| 遥测事件/天         | 100 000 |

## 相关文档

- [部署](./deployment.md) —— 二进制、Docker 及开发部署。
- [快速开始](./getting-started.md) —— 五分钟内从零到第一个任务。
- [架构](./architecture.md) —— 分层架构与三模块设计。
- [示例文件](../../configs/config.example.yaml) —— 完整带注释的参考配置。
