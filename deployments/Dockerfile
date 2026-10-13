# 阶段 1：前端构建
FROM node:22-alpine AS webbuild
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 阶段 2：后端构建（纯 Go，免 cgo）
FROM golang:1.26-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /app/web/dist ./internal/server/webdist
RUN CGO_ENABLED=0 go build -o /fupan ./cmd/fupan

# 阶段 3：运行镜像（单二进制 + 前端 embed）
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=gobuild /fupan /app/fupan
ENV FUPAN_DATA_DIR=/data
ENV FUPAN_MILVUS_ADDR=milvus:19530
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/fupan"]
