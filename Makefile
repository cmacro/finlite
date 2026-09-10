# finlite Makefile：常用开发命令统一入口（与 Taskfile.yml 互补，
# Taskfile 只负责 wails3 打包/运行矩阵）。
#
#   make fmt        Go 格式化检查
#   make vet        Go 静态检查（业务包，不含需 GTK 头文件的 wails 层）
#   make test       Go 单测
#   make check      前端类型检查
#   make verify     fmt + vet + test + check（一键门禁）
#   make bindings   重新生成 Wails TS 绑定（需 GTK 开发头文件）
#   make dev        Wails 开发模式（含后端热重载）
#   make build      生产构建

.PHONY: fmt vet test check verify bindings dev build

fmt:
	gofmt -l internal main.go

vet:
	go vet ./internal/store/... ./internal/decimal/... ./internal/services/...

test:
	go test ./internal/store/... ./internal/decimal/...

check:
	npm run check --prefix frontend

verify: fmt vet test check

bindings:
	wails3 generate bindings

dev:
	wails3 dev -config ./build/config.yml

build:
	wails3 build
