---
title: Makefile 支持 .env 本地环境
description: make 加载 .env（不提交，模板 .env.example）并导出给子命令，GO_TAGS 可按本机 GTK 版本覆盖
---

# 20260922 dotenv for make

## Added

- Makefile 支持 `.env`：`-include .env` + `export`，本地环境差异（GTK 版本等）写进 `.env`，make 每次执行自动加载并导出给所有子命令（含 `wails3 dev` 的内部构建）。
- `.env.example` 模板：`GO_TAGS=-tags=gtk3`（GTK3 本机）与可选 `GOFLAGS`，附语法说明（make 赋值、不加引号、不提交）。
- `make print-env`：打印当前生效的关键变量，调试 `.env` 用。
- 本地已生成实际 `.env`（仅本机，gitignore 掉）。

## Changed

- `GO_TAGS` 由硬编码改为 `?=`：默认仍 `-tags=gtk3`，可被 `.env` 或命令行 `make xxx GO_TAGS=...` 覆盖。

## Fixed

- 上一次提交误将根目录 21.4M 构建产物 `finlite` 二进制纳入版本库（`git add -A` 所致），本次 `git rm --cached` 移除并在 `.gitignore` 加 `/finlite`。

## Scope / Impact

- 仅构建/环境配置；业务代码与前端未改动。

## Verification

- `.env` 加载/覆盖/缺省三态验证：`make print-env` 分别输出 gtk3 / 覆盖值 / 默认 gtk3。
- `make verify`（fmt + vet -tags=gtk3 + test + gobuild + 前端 check）全过。
- 匿名化 grep：无原值命中；`.env` 已 gitignore，不入库。