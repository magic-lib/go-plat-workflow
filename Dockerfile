# 构建上下文应为本目录（go-plat-workflow/）
#   docker build -t go-plat-workflow:latest -f Dockerfile .
ARG GOLANG_IMAGE=golang:1.24.6
ARG ALPINE_IMAGE=alpine:3.21.3

FROM ${GOLANG_IMAGE} AS builder

# git 用于：1) GOPROXY 缺失时 fallback clone 内部仓库；2) 获取当前 commit id
RUN apt-get update && apt-get install -y --no-install-recommends git make && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY .. .
COPY .git/ ./.git/

ENV GOPROXY="https://goproxy.io,direct"
ENV GOSUMDB="off"
ENV GOFLAGS="-mod=mod"
ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN make gen-update-package

# 下载依赖（Go 模块缓存）
RUN GODEBUG=http2client=0 go mod download -x

# 复制业务源码
COPY workflow/ ./workflow/

# 生成 git_commit_id 文件（内容为当前 HEAD commit）
RUN git -C /src rev-parse HEAD > /src/git_commit_id

# 编译静态二进制（-o 输出到 /src/server）
RUN go build -ldflags="-s -w" -o /src/server ./workflow/web/cmd/

# ============================================================
# 运行阶段
# ============================================================
FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=builder /src/workflow/etc/app.yaml.demo /app/config/app.yaml
COPY --from=builder /src/server /app/workflow
# 当前 commit id（构建时由 git rev-parse HEAD 生成）
COPY --from=builder /src/git_commit_id /app/git_commit_id

# 数据/运行目录
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

EXPOSE 8686

# 可通过环境变量覆盖：
#   DB_DSN / LISTEN_ADDR / CONFIG_PATH / CONFIG_SECRET_KEY
# 若用 CONFIG_PATH 指定配置文件，可改为 CMD ["-addr", ":8686", "-config", "/app/workflow/etc/app.yaml"]
ENTRYPOINT ["/app/workflow"]
CMD ["-addr", ":8686", "-f", "config/app.yaml"]
