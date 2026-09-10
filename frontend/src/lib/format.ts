// 金额 / 日期 / 状态展示 helpers。纯函数，无业务计算（计算在 Go Service）。
// 金额统一后端 decimal 字符串，前端只做展示格式化。

/** 金额展示：保留 2 位小数，千分位分隔。输入为后端 decimal 字符串。 */
export function formatMoney(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return '0.00';
  const n = typeof v === 'number' ? v : Number(v);
  if (Number.isNaN(n)) return '0.00';
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

/** 余额校验状态 → 中文。 */
export function statusText(s: string): string {
  switch (s) {
    case 'ok': return '正常';
    case 'mismatch': return '不平';
    case 'skip': return '跳过';
    case 'confirmed': return '已确认';
    default: return s || '-';
  }
}

/** 余额校验状态 → Tailwind 样式类（mismatch 红 / ok+confirmed 绿 / skip 灰，色值见 styles/tokens.css）。 */
export function statusClass(s: string): string {
  switch (s) {
    case 'mismatch': return 'font-semibold text-danger';
    case 'ok':
    case 'confirmed': return 'text-ok';
    case 'skip': return 'text-muted';
    default: return '';
  }
}

/** 汇总状态 → 中文。 */
export function summaryStatusText(s: string): string {
  switch (s) {
    case 'draft': return '草稿';
    case 'reviewed': return '已审核';
    case 'vouchered': return '已生成凭证';
    default: return s || '-';
  }
}

/** 凭证状态 → 中文。 */
export function voucherStatusText(s: string): string {
  switch (s) {
    case 'draft': return '草稿';
    case 'audited': return '已审核';
    default: return s || '-';
  }
}

/** 费率标识 → 中文。 */
export function feeFlagText(f: string): string {
  switch (f) {
    case 'fee_diff': return '费率差异';
    case 'net_diff': return '实收差异';
    case 'fee_parse_err': return '金额解析失败';
    default: return '';
  }
}
