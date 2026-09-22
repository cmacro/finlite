# finlite Makefile：常用开发命令统一入口（与 Taskfile.yml 互补，
# Taskfile 只负责 wails3 打包/运行矩阵）。
#
#   make fmt        Go 格式化检查
#   make vet        Go 静态检查（含 wails 层）
#   make test       Go 单测
#   make gobuild    编译门禁（根包）
#   make check      前端类型检查
#   make verify     fmt + vet + test + gobuild + check（一键门禁）
#   make print-env  打印当前生效的关键变量（调试 .env 用）
#   make bindings   重新生成 Wails TS 绑定
#   make dev        Wails 开发模式（含后端热重载）
#   make package    生产构建（wails3 打包）
#
# ---------- .env 本地环境 ----------
# 本地环境差异（GTK 版本、代理等）写进 .env（不提交，模板见 .env.example）。
# make 会加载其中 KEY=VALUE 并 export 给所有子命令（含 wails3 dev 的内部构建）。
# 语法为 make 变量赋值：不要加 export / 引号 / 行尾空格；注释用 #。

-include .env
export

# 环境说明：本机装的是 GTK3（gtk+-3.0 + webkit2gtk-4.1），无 GTK4 头文件，
# 所有 go build / go vet 必须带 -tags=gtk3（Wails linux_cgo 按 tag 选择实现），
# 因此 GO_TAGS 默认为 -tags=gtk3，.env 中可覆盖。
# 另外 go build ./... 会误把 build/{android,ios} 的 package main 模板算进去，
# 编译门禁用根包 `go build .`。

.PHONY: fmt vet test gobuild check verify print-env bindings dev package

GO_TAGS ?= -tags=gtk3

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

print-env:
	@echo "GO_TAGS   = $(GO_TAGS)"
	@echo "GOFLAGS   = $${GOFLAGS:-<未设>}"

verify: fmt vet test gobuild check

bindings:
	wails3 generate bindings -ts -i

dev:
	wails3 dev -config ./build/config.yml

package:
	wails3 build
