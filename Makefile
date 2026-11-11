.PHONY: web build dev test clean

# 前端构建 → 拷贝进 embed 目录 → 编译单二进制
web:
	cd web && npm run build
	rm -rf internal/server/webdist
	cp -r web/dist internal/server/webdist

build: web
	CGO_ENABLED=0 go build -o fupan ./cmd/fupan

# 仅后端（前端已构建过）
backend:
	CGO_ENABLED=0 go build -o fupan ./cmd/fupan

# 本地开发：显式 NoAuth + loopback；前端 vite 可另开终端运行
dev:
	FUPAN_NO_AUTH=1 FUPAN_ADDR=127.0.0.1:8080 go run ./cmd/fupan -dev

test:
	go test ./...

clean:
	rm -rf internal/server/webdist fupan
