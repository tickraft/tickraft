# Tickraft documentation

Welcome to the Tickraft documentation. The open-source edition ships as a single self-contained binary that bundles the REST API, the Vue 3 SPA, the scheduling engine, the execution engine, the collection engine, and the alerting engine.

## Documentation index

| Document | Description |
|----------|-------------|
| [Getting started](./getting-started.md) | From zero to your first scheduled task in five minutes. |
| [User guide](./user-guide.md) | A walkthrough of every screen in the web UI, with screenshots. |
| [Configuration](./configuration.md) | Every configuration field explained, with environment-variable interpolation. |
| [Architecture](./architecture.md) | Layered architecture, the three-module design, and the event bus. |
| [Deployment](./deployment.md) | Binary, Docker, and development deployment. |
| [Extension guide](./extension-guide.md) | How to add custom executors, listeners, channels, and API plugins via SPI. |
| [Module boundaries](./module-boundary.md) | The rules that keep the scheduler, executor, and collector decoupled. |
| [OpenAPI specification](./api/openapi.yaml) | REST API paths, request/response schemas, and error codes. |

## Languages

- **English** (authoritative) — you are here.
- [简体中文](./zh-CN/README.md) — reference translation.

> The English documentation is the authoritative source. The Chinese translation is provided for convenience only; in case of any discrepancy, the English version prevails.

## License

Tickraft is dual-licensed under AGPLv3 and the Tickraft Commercial License. See [LICENSE](../LICENSE) for details.
