// 金额 / 日期 / 状态展示 helpers。纯函数，无业务计算（计算在 Go Service）。
// 金额统一后端 decimal 字符串，前端只做展示格式化。

/** 金额展示：保留 2 位小数，千分位分隔。输入为后端 decimal 字符串。 */
export function formatMoney(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return '0.00';
  const s = typeof v === 'number' ? (Number.isFinite(v) ? v.toFixed(2) : '') : v.trim();
  if (!s) return '0.00';

  let i = 0;
  let neg = false;
  const first = s.charCodeAt(0);
  if (first === 45) {
    // '-'
    neg = true;
    i = 1;
  } else if (first === 43) {
    // '+'
    i = 1;
  }
  const dot = s.indexOf('.', i);
  const intRaw = dot < 0 ? s.slice(i) : s.slice(i, dot);
  const fracRaw = dot < 0 ? '' : s.slice(dot + 1);
  if (intRaw === '' || !/^\d+$/.test(intRaw) || (fracRaw !== '' && !/^\d+$/.test(fracRaw))) {
    return '0.00';
  }

  // 字符串进位取 2 位小数，避免经过 float64（金额只做展示，但绝不落到浮点）。
  let digits = intRaw + (fracRaw + '00').slice(0, 2);
  if (fracRaw.length > 2 && fracRaw.charCodeAt(2) >= 53) {
    let k = digits.length - 1;
    while (k >= 0 && digits.charCodeAt(k) === 57) {
      digits = `${digits.slice(0, k)}0${digits.slice(k + 1)}`;
      k -= 1;
    }
    digits =
      k >= 0
        ? `${digits.slice(0, k)}${String.fromCharCode(digits.charCodeAt(k) + 1)}${digits.slice(k + 1)}`
        : `1${digits}`;
  }

  const intPart = digits.slice(0, -2).replace(/^0+(?=\d)/, '');
  const fracPart = digits.slice(-2);
  const grouped = intPart.length > 3 ? intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ',') : intPart;
  return `${neg ? '-' : ''}${grouped}.${fracPart}`;
}

/** 带符号金额展示：保留后端差额的 +/- 号，其余同 formatMoney。 */
export function formatSignedMoney(v: string | null | undefined): string {
  if (v === null || v === undefined || v === '') return '0.00';
  const t = String(v).trim();
  const neg = t.startsWith('-');
  const pos = t.startsWith('+');
  const body = formatMoney(t.replace(/^[+-]/, ''));
  return (neg ? '-' : pos ? '+' : '') + body;
}

/** 时间戳展示：RFC3339 → `YYYY-MM-DD HH:mm:ss`（本地时区）；非 RFC3339 原样返回。 */
export function formatDateTime(v: string | null | undefined): string {
  if (!v) return '—';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
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

/**
 * 银行流水行展示状态：已确认 / 不平 / 无上行余额（跳过）/ 正常。
 * 只做展示映射；校验本身在 Go 导入时完成（首行无上行余额 = 跳过）。
 */
export function bankRowState(f: { displayStatus: string; prevBalance: string }): string {
  if (f.displayStatus === 'confirmed') return 'confirmed';
  if (f.displayStatus === 'mismatch') return 'mismatch';
  if (!f.prevBalance) return 'skip';
  return 'ok';
}

/** 原始 JSON 字符串 → 两空格缩进的展示文本（解析失败原样返回）。 */
export function prettyJson(raw: string): string {
  if (!raw) return '{}';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
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

/** 业务日期（YYYY-MM-DD）→ 中文星期，如 `2026-07-01` → `周三`；非法日期返回空串。 */
export function weekdayCN(date: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return '';
  const d = new Date(`${date}T00:00:00`);
  if (Number.isNaN(d.getTime())) return '';
  return ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][d.getDay()] ?? '';
}

/** 字节数 → 人类可读（B/KB/MB/GB，1 位小数）；备份与原始文件体积展示用。 */
export function formatBytes(v: number | null | undefined): string {
  const n = Number(v ?? 0);
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let x = n;
  while (x >= 1024 && i < units.length - 1) {
    x /= 1024;
    i += 1;
  }
  return i === 0 ? `${Math.round(x)} B` : `${x.toFixed(1)} ${units[i]}`;
}

/** 原始文件校验问题类型 → 中文。 */
export function evidenceIssueText(kind: string): string {
  switch (kind) {
    case 'missing': return '文件缺失';
    case 'modified': return '内容被改动';
    case 'unregistered': return '未登记文件';
    default: return kind;
  }
}
