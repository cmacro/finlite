---
title: GTK3 构建环境固化
description: 本机 GTK3 环境下 go vet/go build 需 -tags=gtk3，Makefile/AGENTS.md 固化配置，go build 门禁改为根包
---

# 20260922 gtk3 build tags

## Changed

- Makefile：`vet` 扩到 `go vet -tags=gtk3 ./...`（此前只检业务包）；新增 `gobuild`（`go build -tags=gtk3 .`）；`verify` 门禁加入 gobuild；`build` 更名 `package`（wails3 生产打包），bindings 任务固定 `-ts -i` 参数。
- AGENTS.md Completion Criteria：更新为 `go vet -tags=gtk3 ./...` + `go build -tags=gtk3 .`，并记录原因。

## Docs

- 环境事实：本机装的是 GTK3（`gtk+-3.0` 3.24 / `webkit2gtk-4.1` 2.50），无 GTK4 头文件；Wails v3 `linux_cgo` 默认按 GTK4 编译，需 `-tags=gtk3` 切换实现。此前的 `go build ./...` 失败均为该原因，非业务代码问题。

## Fixed

- `go build ./...` 与根包 `go build .` 区分：`build/{android,ios}` 下的 `package main` 模板在 linux 下无 `main` 函数，`./...` 会误报 "function main is undeclared"；门禁改用根包构建。

## Scope / Impact

- 仅构建/门禁配置；业务代码与前端未改动。

## Verification

- `make verify`（fmt + vet -tags=gtk3 ./... + test + go build -tags=gtk3 . + 前端 check）全过。
- 匿名化 grep：无原值命中。