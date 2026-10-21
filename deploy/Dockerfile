FROM node:22-bookworm-slim AS web-builder

WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.3-bookworm AS app-builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web-builder /src/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/typecho-next ./cmd/server

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && groupadd --gid 10001 typecho \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin typecho \
    && mkdir -p /app/web /var/lib/typecho \
    && chown -R typecho:typecho /app /var/lib/typecho \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=app-builder --chown=10001:10001 /out/typecho-next /app/typecho-next
COPY --from=web-builder --chown=10001:10001 /src/web/dist /app/web/dist

ENV APP_LISTEN_HOST=0.0.0.0 \
    APP_PORT=8081 \
    APP_DATA_DIR=/var/lib/typecho \
    APP_WEB_DIR=/app/web/dist

USER 10001:10001
EXPOSE 8081
VOLUME ["/var/lib/typecho"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD curl --fail --silent http://127.0.0.1:8081/healthz || exit 1
ENTRYPOINT ["/app/typecho-next"]
