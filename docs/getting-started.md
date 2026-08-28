# Getting started

This guide takes you from a clean checkout to a running Tickraft instance with your first scheduled task in about five minutes.

## Prerequisites

- **Go** 1.26 or newer (to build the backend).
- **Node.js** 22 or newer and **pnpm** 9 or newer (to compile the embedded SPA; only needed for `make build`).
- A SQLite-capable filesystem — SQLite is embedded, so no external database server is required.

## Step 1 — build the binary

```bash
git clone https://github.com/tickraft/tickraft.git
cd tickraft
make build
```

The binary is written to `bin/tickraft`. `make build` first compiles the SPA with pnpm and embeds it into the binary, so the UI is served by the same process as the API.

> Backend-only build: `go build -o bin/tickraft ./cmd/tickraft` skips the frontend toolchain, but the embedded SPA is then empty — the browser UI will not load, and the instance is API-only.

## Step 2 — prepare the configuration

Copy the example configuration and edit it:

```bash
cp configs/config.example.yaml config.yaml
```

At minimum, set these three environment variables so the config file can interpolate them:

```bash
export TICKRAFT_DB_DSN="sqlite:///tmp/tickraft.db"
export TICKRAFT_JWT_SECRET="$(openssl rand -hex 32)"
export TICKRAFT_ADMIN_PASSWORD="admin"
```

> The `TICKRAFT_ADMIN_PASSWORD` is the password for the built-in `admin` user. You will be asked to change it on first login.

Validate the configuration before starting:

```bash
./bin/tickraft config validate -c config.yaml
```

## Step 3 — start the server

```bash
./bin/tickraft start --config config.yaml
```

The server listens on `http://localhost:6153` by default. Open it in a browser.

## Step 4 — log in

1. Navigate to `http://localhost:6153`.
2. Sign in with username `admin` and the password you set in `TICKRAFT_ADMIN_PASSWORD`.
3. On first login you are forced to change your password — pick a strong one and confirm.

![Login screen](./screenshots/login.png)

## Step 5 — register an asset

Before a probe or a listener can report data, register the target as an asset.

1. Open **Telemetry → Assets** in the sidebar.
2. Click **Create**, fill in a name and an asset key (e.g. `web-1`), and save.
3. Note the asset key — you will use it when reporting telemetry.

![Asset list](./screenshots/telemetry-asset-list.png)

## Step 6 — create your first task

Create an HTTP probe task that checks a URL every minute.

1. Open **Scheduler → Tasks** and click **Create**.
2. Choose the `http` executor.
3. Set the target URL (e.g. `https://example.com`).
4. Pick the `interval` schedule type and set the interval to `60s`.
5. Save and enable the task.

The task fires immediately and then every 60 seconds. Open the task detail to watch execution records arrive in real time.

![Task list](./screenshots/scheduler-task-list.png)
![Task detail](./screenshots/scheduler-task-detail.png)

## Step 7 — review execution logs

Open **Scheduler → Logs** to see every execution — status, duration, and output — for all tasks.

![Execution logs](./screenshots/scheduler-log-list.png)

## Step 8 — explore the dashboard

The dashboard rolls up asset status, task success rates, and recent alerts into one view.

![Dashboard](./screenshots/dashboard.png)

## Next steps

- **[User guide](./user-guide.md)** — a walkthrough of every screen in the web UI.
- **[Configuration](./configuration.md)** — every configuration field explained.
- **[Architecture](./architecture.md)** — how the scheduler, executor, and telemetry cooperate.
- **[Extension guide](./extension-guide.md)** — add custom executors, listeners, and channels.

## Troubleshooting

**Port 6153 is already in use.** Check the listener with `lsof -i :6153` and either stop the conflicting process or change `server.addr` in `config.yaml`.

**`config validate` reports an unset variable.** Every `${VAR}` without a `:-default` must be present in the environment. Export the variable or add a default.

**First login fails.** Confirm that `TICKRAFT_ADMIN_PASSWORD` was exported in the shell that started the server. When the password env var is empty, the server generates a random one and logs it once at startup — check the startup log.

**The UI cannot reach the API.** The SPA is served from the same port as the API, so there is no CORS issue in production. In development mode (`pnpm run dev` on port 5173), the Vite proxy forwards `/api` to the backend — make sure the backend is running on the port configured in `vite.config.ts`.
