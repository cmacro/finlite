// 前端本地类型（非后端 DTO）。
// 后端 DTO 一律从生成的 `bindings/finlite/internal/services/models` 导入，
// 本文件只放与后端无关的界面类型，避免出现两份 DTO 定义。

/** 左侧导航路由键。 */
export type RouteKey =
  | 'import'
  | 'bank-flow'
  | 'order-flow'
  | 'recon'
  | 'summary'
  | 'voucher'
  | 'balance'
  | 'settings';
