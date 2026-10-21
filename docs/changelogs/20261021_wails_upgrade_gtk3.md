---
title: 升级 Wails 到 v3.0.0-beta.34 与本机 GTK3 构建约束
description: wails/v3 从 beta.16 升到 beta.34，Go 工具链切到 1.26.0，wails3 CLI 与 bindings 同步；记录 beta.34 默认走 GTK4、本机 GTK 4.8 缺 GtkFileDialog，故 task build/dev 必须带 EXTRA_TAGS=gtk3
---

# 升级 Wails 到 v3.0.0-beta.34（2026-10-21）

## Changed

- `go.mod` / `go.sum`：`github.com/wailsapp/wails/v3` `v3.0.0-beta.16` → **`v3.0.0-beta.34`**；
  `go` 指令随之为 `1.26.0`，本机 `GOTOOLCHAIN=auto` 自动切到 go1.26.0 编译（原为 go1.25.3）。
- 本机 `wails3` CLI：`v3.0.0-beta.17` → **`v3.0.0-beta.34`**（与库版本对齐；
  `GOBIN=/usr/local/go/bin` 无写权限，安装需 `GOBIN=$HOME/go/bin go install ...@v3.0.0-beta.34`）。
- 用 beta.34 的 CLI 重新生成 `frontend/bindings`：**无差异**（生成结果与旧版一致，未手改）。

## 关键约束（升级后必看）

beta.34 的 Linux 默认实现走 **GTK4** 路径，而 Debian 12 的 GTK4 是 **4.8**，缺它用到的
`GtkFileDialog`（GTK 4.10+），默认构建会直接编译失败：

```
./linux_cgo.h:177:1: error: unknown type name 'GtkFileDialog'; did you mean 'GtkNativeDialog'?
ERROR task: Failed to run task "build": exit status 1
```

因此本机必须显式带 `gtk3` tag（已写入 `AGENTS.md` 完成标准）：

```sh
go build -tags=gtk3 .                      # 编译门禁（原有）
wails3 task build EXTRA_TAGS=gtk3          # 生产构建
EXTRA_TAGS=gtk3 wails3 dev                 # 开发模式（config.yml 的 executes 会用 EXTRA_TAGS）
```

## 说明

- `-tags=gtk3` 选择 GTK3 + webkit2gtk-4.1 实现（`pkg/application/linux_cgo_gtk3.go`），与本机已装的
  webkit2gtk-4.1 匹配；GTK4 路径需要 GTK ≥ 4.10 与 webkitgtk-6.0。
- beta.34 的主循环退出仍是 `status != 0 → exit code: N`，会经 `main.go` 的 `log.Fatal` 变成退出码 1；
  窗口被外部销毁（如 WSLg 会话断开）时 `wails3 dev` 会报 `Failed to run task "run"`，属预期链路，未在本提交改动。
- 注意不要同时跑两个实例（两个 `wails3 dev` 会抢 9245 端口；两个 app 会并发写同一个 `finlite.db`，
  进程内单写锁不跨进程）。单实例保护（`application.SingleInstance`）尚未启用。

## 验证

- `wails3 task build EXTRA_TAGS=gtk3`：通过；`go version -m bin/finlite` 显示
  `wails/v3 v3.0.0-beta.34` + `go1.26.0`，启动后窗口正常创建、资源正常加载
- `EXTRA_TAGS=gtk3 wails3 dev`：通过（`go build -tags gtk3`，vite 正常启动）
- `gofmt -l .`、`go vet -tags=gtk3 ./...`、`go build -tags=gtk3 .`：通过
- `npm run check --prefix frontend`：0 errors / 0 warnings
