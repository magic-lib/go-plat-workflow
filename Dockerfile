# 构建上下文应为本目录（go-plat-workflow/）
#   docker build -t go-plat-workflow:latest -f Dockerfile .
ARG GOLANG_IMAGE=golang:1.24.3-alpine
ARG ALPINE_IMAGE=alpine:3.21.3

FROM ${GOLANG_IMAGE} AS builder

WORKDIR /src

# 先把依赖描述复制进来（利用 Docker 层缓存）
COPY go.mod go.sum ./

ENV GOPROXY="https://goproxy.cn,direct"
ENV GOSUMDB="off"
ENV CGO_ENABLED=0
ENV GO111MODULE=on

# 下载依赖（Go 模块缓存）
RUN GODEBUG=http2client=0 go mod download -x

# 复制业务源码
COPY workflow/ ./workflow/

# 编译静态二进制（-o 输出到 /src/server）
RUN go build -ldflags="-s -w" -o /src/server ./workflow/web/cmd/

# ============================================================
# 运行阶段
# ============================================================
FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=builder /src/server /app/server

# 配置文件（startupcfg 配置，db_dsn / listen_addr 等）
# Load() 默认相对 exe 目录查找 workflow/etc/app.yaml，故放在此处
COPY workflow/etc/app.yaml.demo /app/workflow/etc/app.yaml

# 数据/运行目录
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

# 可通过环境变量覆盖：
#   DB_DSN / LISTEN_ADDR / CONFIG_PATH / CONFIG_SECRET_KEY
# 若用 CONFIG_PATH 指定配置文件，可改为 CMD ["-addr", ":8080", "-config", "/app/workflow/etc/app.yaml"]
ENTRYPOINT ["/app/server"]
CMD ["-addr", ":8080"]
