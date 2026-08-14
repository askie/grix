# QEMU 在 darwin/arm64 上模拟 linux/amd64 时，Go 1.26 的 modindex mmap
# （cmd/go/internal/modindex）会 SIGSEGV（go build 0.3s 内崩溃）。
# 所有服务统一在原生架构（BUILDPLATFORM）上运行 go build，通过
# GOOS/GOARCH 交叉编译出目标二进制，完全规避 qemu 模拟。
# 参考：https://github.com/multiarch/compiler
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder-native

WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ARG COMMIT=unknown
ARG COMMITTIME=unknown
ARG HTTP_PROXY=""
ARG HTTPS_PROXY=""
ARG NO_PROXY=""

ENV GOPROXY=${GOPROXY}
ENV HTTP_PROXY=${HTTP_PROXY}
ENV HTTPS_PROXY=${HTTPS_PROXY}
ENV NO_PROXY=${NO_PROXY}

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked go mod download

COPY . .

# 版本注入：BuildTime 用 commit 时间（COMMITTIME）而非墙钟时间，保证同一 Tag 重复构建产物可复现。
ENV LDFLAGS="-s -w -X github.com/askie/grix/backend/internal/version.Version=${VERSION} -X github.com/askie/grix/backend/internal/version.Commit=${COMMIT} -X github.com/askie/grix/backend/internal/version.BuildTime=${COMMITTIME}"

FROM builder-native AS build-api
ARG TARGETOS TARGETARCH
RUN test -f ./internal/webapp/dist/index.html
RUN test -f ./internal/adminweb/dist/index.html
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/api ./cmd/api

FROM builder-native AS build-ws
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/ws ./cmd/ws

FROM builder-native AS build-llm
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/llm ./cmd/llm

FROM builder-native AS build-push
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/push ./cmd/push

FROM builder-native AS build-migrate
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/migrate ./cmd/migrate

FROM builder-native AS build-voicebridge
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/voicebridge ./cmd/voicebridge

FROM builder-native AS build-gateway
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/gateway ./cmd/gateway

FROM builder-native AS build-pay
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="${LDFLAGS}" -o /out/pay ./cmd/pay

FROM alpine:3.21 AS runtime-base

ARG HTTP_PROXY=""
ARG HTTPS_PROXY=""
ARG ALPINE_MIRROR="dl-cdn.alpinelinux.org"
RUN export HTTP_PROXY="${HTTP_PROXY}" HTTPS_PROXY="${HTTPS_PROXY}" && \
    sed -i "s/dl-cdn.alpinelinux.org/${ALPINE_MIRROR}/g" /etc/apk/repositories && \
    apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY config.yaml /app/config/config.yaml
COPY migration /app/migration
COPY scripts/docker-entrypoint.sh /app/docker-entrypoint.sh

RUN chmod +x /app/docker-entrypoint.sh

FROM runtime-base AS api
COPY --from=build-api /out/api /app/bin/api
ENV AIBOT_SERVICE=api
EXPOSE 27180
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS ws
COPY --from=build-ws /out/ws /app/bin/ws
ENV AIBOT_SERVICE=ws
EXPOSE 27189
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS llm
COPY --from=build-llm /out/llm /app/bin/llm
ENV AIBOT_SERVICE=llm
EXPOSE 27182
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS push
COPY --from=build-push /out/push /app/bin/push
ENV AIBOT_SERVICE=push
EXPOSE 27183
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS gateway
COPY --from=build-gateway /out/gateway /app/bin/gateway
ENV AIBOT_SERVICE=gateway
EXPOSE 27184
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS pay
COPY --from=build-pay /out/pay /app/bin/pay
ENV AIBOT_SERVICE=pay
EXPOSE 27185
ENTRYPOINT ["/app/docker-entrypoint.sh"]

FROM runtime-base AS migrate
COPY --from=build-migrate /out/migrate /app/bin/migrate
ENTRYPOINT ["/app/bin/migrate"]
CMD ["/app/config/config.yaml"]

# voicebridge Go stub（NATS 控制通道监控，实际音频桥接由 Python 进程处理）
FROM runtime-base AS voicebridge
COPY --from=build-voicebridge /out/voicebridge /app/bin/voicebridge
ENV AIBOT_SERVICE=voicebridge
ENTRYPOINT ["/app/bin/voicebridge"]
CMD ["/app/config/config.yaml"]
