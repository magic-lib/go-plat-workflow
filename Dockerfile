# 构建上下文应为本目录（go-plat-workflow/），需启用 BuildKit（docker 23+ 默认启用）
#   docker build --build-arg GIT_COMMIT_ID="$(git rev-parse HEAD)" -t go-plat-workflow:latest -f Dockerfile .
ARG GOLANG_IMAGE=golang:1.24.6
ARG ALPINE_IMAGE=alpine:3.21.3

FROM ${GOLANG_IMAGE} AS builder

# git 用于 GOPROXY 缺失时 fallback clone 内部仓库
RUN apt-get update && apt-get install -y --no-install-recommends git make && rm -rf /var/lib/apt/lists/*

WORKDIR /src

ENV GOPROXY="https://goproxy.io,direct"
ENV GOSUMDB="off"
ENV GOFLAGS="-mod=mod"
ENV CGO_ENABLED=0
ENV GO111MODULE=on

# 优化顺序：优先处理依赖缓存（参考 credit-server）
# 依赖层：只拷 go.mod/go.sum，依赖不变时整层直接命中缓存。
# cache mount：Go 模块缓存(/go/pkg/mod)和编译缓存(/root/.cache/go-build)留在宿主机 Docker 里，
#   跨构建复用；id 统一用 gomod/gobuild，和其他 Go 服务共用同一份缓存。
# 原写法第一步就 COPY 整个目录（含 .git），任何提交都会让后面所有层失效，且没有 cache mount，
#   每次都要重新下载全部模块、从零编译。
COPY go.mod go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    for i in 1 2 3; do GODEBUG=http2client=0 go mod download -x && exit 0; echo "go mod download failed (attempt $i), retrying"; sleep 10; done; exit 1

# 复制源码（.git 已在 .dockerignore 排除，commit id 改由构建参数传入）
COPY . .

# 每次构建都把 magic-lib 内部库更新到 master 最新（go mod tidy 依赖源码，只能放在 COPY . . 之后）。
# 必须同样挂 id=gomod：cache mount 的内容不会留在镜像层里，不挂的话这一步看到的 /go/pkg/mod 是空的，
#   又会把全部模块重新下载一遍；挂上后只需拉取有变化的内部库。
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    make gen-update-package

# 编译静态二进制（-o 输出到 /src/server）
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /src/server ./workflow/web/cmd/

# ============================================================
# 运行阶段
# ============================================================
FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 appuser

WORKDIR /app

# 数据/运行目录
RUN mkdir -p /app/data && chown -R appuser:appuser /app

COPY --from=builder --chown=appuser:appuser /src/workflow/etc/app.yaml.demo /app/config/app.yaml
COPY --from=builder --chown=appuser:appuser /src/server /app/workflow

# 当前 commit id（CI 里由 --build-arg GIT_COMMIT_ID="$(git rev-parse HEAD)" 传入）。
# ARG 放在最后：它每次提交都会变，放前面会让 apk 等层的缓存失效
ARG GIT_COMMIT_ID
ENV GIT_COMMIT_ID=${GIT_COMMIT_ID}
RUN echo "${GIT_COMMIT_ID}" > /app/git_commit_id && chown appuser:appuser /app/git_commit_id

USER appuser

EXPOSE 8686

# 可通过环境变量覆盖：
#   DB_DSN / LISTEN_ADDR / CONFIG_PATH / CONFIG_SECRET_KEY
# 若用 CONFIG_PATH 指定配置文件，可改为 CMD ["-addr", ":8686", "-config", "/app/workflow/etc/app.yaml"]
ENTRYPOINT ["/app/workflow"]
CMD ["-addr", ":8686", "-f", "config/app.yaml"]
