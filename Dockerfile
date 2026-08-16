# Build frontend
FROM node:24-alpine AS web-builder
RUN apk add --no-cache libc6-compat
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /app/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
COPY web/packages/core/package.json ./packages/core/
COPY web/packages/features/package.json ./packages/features/
COPY web/app/package.json ./app/
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# Build Go binary
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /app/web/app/dist ./web/app/dist
RUN rm -rf internal/web/dist && mkdir -p internal/web/dist && cp -r web/app/dist/* internal/web/dist/
RUN CGO_ENABLED=0 go build -o /tickraft ./cmd/tickraft

# Runtime
FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /tickraft .
COPY configs/config.example.yaml /app/config.yaml
# Expose the single Server API port. The runtime runs in
# single-port mode: the API, SPA, and webhook listener are all served
# from this one port.
EXPOSE 6153
ENTRYPOINT ["./tickraft"]
CMD ["start", "--config", "/app/config.yaml"]
