FROM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25 AS builder

COPY . /src
WORKDIR /src

RUN CGO_ENABLED=0 GOPROXY=https://goproxy.cn make build

FROM debian:stable-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    netbase \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /src/bin/vane /app/vane
COPY --from=web /src/web/dist /app/web/dist
EXPOSE 8080
VOLUME ["/data/conf", "/app/logs"]
CMD ["./vane", "serve", "--conf", "/data/conf/config.yaml"]
