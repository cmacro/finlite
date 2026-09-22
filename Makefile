# finlite Makefile：常用开发命令统一入口（与 Taskfile.yml 互补，
# Taskfile 只负责 wails3 打包/运行矩阵）。
#
#   make fmt        Go 格式化检查
#   make vet        Go 静态检查（含 wails 层）
#   make test       Go 单测
#   make check      前端类型检查
#   make verify     fmt + vet + test + check + gobuild（一键门禁）
#   make bindings   重新生成 Wails TS 绑定
#   make dev        Wails 开发模式（含后端热重载）
#   make package    生产构建（wails3 打包）
#
# 环境说明：本机装的是 GTK3（gtk+-3.0 + webkit2gtk-4.1），无 GTK4 头文件，
# 所有 go build / go vet 必须带 -tags=gtk3（Wails linux_cgo 按 tag 选择实现）。
# 另外 go build ./... 会误把 build/{android,ios} 的 package main 模板算进去，
# 编译门禁用根包 `go build .`。

.PHONY: fmt vet test check verify bindings dev build package

GO_TAGS := -tags=gtk3

fmt:
	gofmt -l internal main.go

vet:
	go vet $(GO_TAGS) ./...

test:
	go test ./internal/store/... ./internal/decimal/... ./internal/services/...

gobuild:
	go build $(GO_TAGS) .

check:
	npm run check --prefix frontend

verify: fmt vet test gobuild check

bindings:
	wails3 generate bindings -ts -i

dev:
	wails3 dev -config ./build/config.yml

package:
	wails3 build
