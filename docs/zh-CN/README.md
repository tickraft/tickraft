# Tickraft 中文文档

> 本目录为中文参考翻译，英文文档为权威来源。如中英文存在歧义，以英文版为准。

欢迎阅读 Tickraft 中文文档。开源版以单一自包含二进制文件形式发布，内置 REST API、Vue 3 单页应用、调度引擎、执行引擎、采集引擎与告警引擎。

## 文档索引

| 文档 | 描述 |
|------|------|
| [快速入门](./getting-started.md) | 五分钟内从零到第一个调度任务。 |
| [用户指南](./user-guide.md) | 带截图的 Web UI 各页面完整 walkthrough。 |
| [配置说明](./configuration.md) | 每个配置字段的详解，含环境变量插值。 |
| [架构设计](./architecture.md) | 分层架构、三模块设计与事件总线。 |
| [部署指南](./deployment.md) | 二进制、Docker 与开发环境部署。 |
| [扩展指南](./extension-guide.md) | 如何通过 SPI 添加自定义 executor、listener、channel 与 API 插件。 |
| [模块边界](./module-boundary.md) | 保持 scheduler、executor 与 collector 解耦的规则。 |
| [OpenAPI 规范](../api/openapi.yaml) | REST API 路径、请求/响应模式与错误码。 |

## 语言版本

- [English](../README.md) — 权威来源。
- **简体中文**（参考翻译）— 当前页面。

> 英文文档为权威来源。中文翻译仅为便利提供；如有歧义，以英文版为准。

## 许可证

Tickraft 采用 AGPLv3 与 Tickraft Commercial License 双重许可。详见 [LICENSE](../../LICENSE)。
