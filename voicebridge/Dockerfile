FROM python:3.11-slim

WORKDIR /app

# 安装依赖(默认走阿里云 PyPI/apt 镜像,加速 CN 构建;镜像均走 HTTPS + 正常证书校验,
# 不再使用 --trusted-host 跳过 TLS 验证;
# 海外构建可经 --build-arg PIP_INDEX_URL=https://pypi.org/simple
# --build-arg APT_MIRROR=deb.debian.org 切官方源)
ARG PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple
ARG APT_MIRROR=mirrors.aliyun.com
# pip 下载默认走 HTTP 代理（通过 docker --build-arg 传入，降低国际网络超时重试）
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
# 版本注入：voicebridge 是 Python 进程，不吃 ldflags，改走 build-arg → 运行时环境变量。
ARG VERSION=dev
ARG COMMIT=unknown
ARG COMMITTIME=unknown
ENV VOICEBRIDGE_VERSION=${VERSION}
ENV VOICEBRIDGE_COMMIT=${COMMIT}
ENV VOICEBRIDGE_BUILD_TIME=${COMMITTIME}
COPY requirements.txt .
# osc-data (volcengine SDK 依赖) 需要 Rust 编译；装完后清理以压缩镜像
# pip 走 buildkit 缓存（--mount=type=cache,target=/root/.cache/pip），
# 避免每次构建重新下载全部 Python wheels（否则 CN 区每次约 7min）。
# 清理脚本也发生在 RUN 内部，不影响缓存生存期。
RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked \
    sed -i "s/deb.debian.org/${APT_MIRROR}/g" /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
    sed -i "s/deb.debian.org/${APT_MIRROR}/g" /etc/apt/sources.list && \
    apt-get update && apt-get install -y --no-install-recommends gcc build-essential cargo && \
    pip install -i "$PIP_INDEX_URL" -r requirements.txt && \
    apt-get purge -y gcc build-essential cargo && apt-get autoremove -y && apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# 复制所有应用代码（*.py）
COPY *.py .

# 非 root 运行
RUN useradd --system --uid 10001 --no-create-home voicebridge
USER voicebridge

# 运行时环境变量（通过 k8s Secret 注入）
ENV LIVEKIT_URL=""
ENV LIVEKIT_API_KEY=""
ENV LIVEKIT_API_SECRET=""
ENV NATS_URL="nats://nats:4222"

CMD ["python", "main.py"]
