---
title: UI 原型目录与预览入口
description: docs/uidesign 原型文件清单、OpenDesign 统一项目 finlite-uidesign 的预览地址规则与新增流程
---

# UI 原型目录

所有高保真原型集中在 **OpenDesign 项目 `finlite-uidesign`**，文件位于项目根目录，按文件名直接预览。

## 预览地址

```
http://127.0.0.1:7456/api/projects/finlite-uidesign/raw/{文件名}
```

总览入口：<http://127.0.0.1:7456/api/projects/finlite-uidesign/raw/index.html>

本地副本可直接用浏览器打开（不依赖 OpenDesign 服务）。

## 文件清单

| 文件 | 说明 |
|---|---|
| `index.html` | 原型总览（卡片导航） |
| `import-center.html` | F1 导入中心 |
| `bank-flow.html` | F2 银行流水（三色余额校验） |
| `order-flow.html` | F3 订单流水（费率复算，不分页） |
| `collection.html` | F9 店面收款（附件/审核/生成汇总） |
| `recon-center-prototype.html` | F4 对账中心原型 |
| `reconciliation.html` | 对账中心早期版本 |
| `import-center-ant.html` | 导入中心 · Ant 风格 |
| `import-center-material.html` | 导入中心 · Material 风格 |
| `import-center-linear.html` | 导入中心 · Linear 风格 |
| `import-center-stripe.html` | 导入中心 · Stripe 风格 |
| `DESIGN.md` | 统一设计规范 |
| `tokens.css` | 设计 token（机器可读） |

## 侧栏基准

所有页面的左侧导航以 `order-flow.html` 为唯一基准：品牌区（F + Finlite + 单账套）、5 组 9 项导航、底部状态条，仅当前高亮项不同。新增页面必须复用同结构，不得自创导航分组或样式。

## 新增原型流程

1. 在 `finlite-uidesign` 项目内生成 / 写入新 HTML（放项目根，文件名即 URL 路径）。
2. 生成前先读 `DESIGN.md` + `tokens.css`，遵循统一风格。
3. 同步一份到 `docs/uidesign/`。
4. 更新本 README 与 `index.html` 的入口列表。

## 快捷查询

```bash
./scripts/od-preview.sh            # 列出全部可预览 URL
./scripts/od-preview.sh bank       # 只列文件名/项目含 bank 的
./scripts/od-preview.sh '' --check # 附带 HTTP 状态码
```
