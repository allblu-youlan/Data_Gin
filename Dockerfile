# syntax=docker/dockerfile:1
FROM golang:1.24-bullseye AS builder

WORKDIR /app

# The official Debian-based Go image already includes GCC and glibc headers.
# Avoid an unnecessary APT refresh so builds do not depend on mirror index sync.

COPY go.mod go.sum ./
# 使用 BuildKit 缓存模块与编译产物，依赖未变化时不重复下载和全量编译。
RUN --mount=type=cache,target=/go/pkg/mod \
    GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -o main .

FROM oraclelinux:9-slim

# godror compiles without Oracle Client headers, but requires libclntsh at
# runtime. Oracle Linux keeps the official Instant Client and its native
# dependencies in one compatible glibc-based image.
RUN mkdir -p /etc/yum/vars && \
    microdnf install -y oracle-instantclient-release-el9 && \
    microdnf install -y oracle-instantclient19.32-basiclite ca-certificates tzdata wget && \
    microdnf clean all

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/etc ./etc
COPY --from=builder /app/ssl ./ssl

RUN mkdir -p /app/storage/logs /app/storage/cache

EXPOSE 8501

ENV TZ=Asia/Shanghai

CMD ["./main", "server"]
