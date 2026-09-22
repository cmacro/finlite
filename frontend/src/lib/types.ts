// 后端 DTO 的 TS 镜像（与 internal/services/models.go 同步）。
// TODO: 运行 `wails3 generate bindings` 后，各页面改从
// `../../bindings/finlite/internal/services` 导入同名 Service 调用，
// 本文件仅保留类型定义供本地 state 标注。

export interface ImportResult {
  batchId: string;
  /** bank_flow / order_flow（见 import_batches.source_type） */
  sourceType: string;
  fileName: string;
  rowCount: number;
  successCount: number;
  skipCount: number;
  balanceWarning: string;
}

export interface BankFlow {
  id: string;
  batchId: string;
  fileName: string;
  rowNo: number;
  txnTime: string;
  counterparty: string;
  summary: string;
  remark: string;
  inAmount: string;
  outAmount: string;
  balance: string;
  balanceStatus: string;
  balanceConfirmedAt: string;
  displayStatus: string;
  createdAt: string;
}

export interface OrderFlow {
  id: string;
  batchId: string;
  icbcOrderNo: string;
  thirdOrderNo: string;
  txnTime: string;
  settleDate: string;
  orderAmount: string;
  feeAmount: string;
  netAmount: string;
  payMethod: string;
  debitCreditFlag: string;
  feeFlag: string;
  createdAt: string;
}

export interface ReconResult {
  date: string;
  orderNet: string;
  bankPOSIn: string;
  summaryActual: string;
  matched: boolean;
  diff: string;
}

export interface ReconDiff {
  date: string;
  field: string;
  expect: string;
  actual: string;
  detail: string;
}

export interface Summary {
  id: string;
  date: string;
  receiptNo: string;
  category: string;
  project: string;
  reason: string;
  incomeAmount: string;
  expenseAmount: string;
  feeAuto: string;
  feeActual: string;
  feeManualAdjusted: number;
  balance: string;
  remark: string;
  source: string;
  status: string;
  voucherId: string;
  createdAt: string;
  updatedAt: string;
}

export interface VoucherEntry {
  id: string;
  voucherId: string;
  accountCode: string;
  accountName: string;
  debit: string;
  credit: string;
  remark: string;
  sortNo: number;
}

export interface Voucher {
  id: string;
  voucherDate: string;
  voucherNo: string;
  summaryText: string;
  status: string;
  sourceSummaryId: string;
  entries: VoucherEntry[];
  createdAt: string;
}

export interface BalanceRow {
  accountCode: string;
  accountName: string;
  begin: string;
  debit: string;
  credit: string;
  end: string;
}

export type RouteKey =
  | 'import'
  | 'bank-flow'
  | 'order-flow'
  | 'recon'
  | 'summary'
  | 'voucher'
  | 'balance'
  | 'settings';
