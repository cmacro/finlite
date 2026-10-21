// 通用数据表（lib/DataTable.svelte）的列定义类型与导出助手。
// 页面只声明列（含单元格 snippet）与数据行，表格骨架、事件委托、展开行、粘性表头由组件负责。
import type { Snippet } from 'svelte';
import { Clipboard } from '@wailsio/runtime';

/**
 * 一列的声明。`key` 既是默认单元格取值字段，也是默认导出到 Excel 的字段。
 */
export interface TableColumn<T> {
  /** 列 key：默认单元格取 row[key]，默认导出取 row[key] */
  key: string;
  /** 表头文案 */
  title: string;
  /** colgroup 宽度（固定布局，跨批次/筛选列宽恒定，便于财务目视比对） */
  width: string;
  /** 数字列右对齐 */
  align?: 'right';
  /** td 附加 class */
  class?: string;
  /** th 附加 class */
  headClass?: string;
  /** 自定义单元格渲染；缺省按 row[key] 原样输出 */
  cell?: Snippet<[T]>;
  /** 鼠标悬停提示（长文本被省略号截断时给出全文） */
  hint?: (row: T) => string;
  /** 复制到 Excel 的原始值（不带千分位）；false = 该列不参与复制 */
  copy?: ((row: T) => string | number | null | undefined) | false;
}

/** TSV 单元格：去掉制表符 / 换行，避免粘贴到 Excel 时串列。 */
export function tsvCell(v: unknown): string {
  if (v === null || v === undefined) return '';
  if (typeof v === 'number') return Number.isFinite(v) ? String(v) : '';
  return String(v).replace(/[\t\r\n]+/g, ' ');
}

/**
 * 列定义 + 数据行 → TSV 文本：含表头，CRLF 换行，金额用后端原始 decimal 字符串。
 * 直接粘到 Excel 即为分列、金额为数值（不带千分位，避免文本化）。
 */
export function buildTSV<T>(columns: TableColumn<T>[], rows: T[]): string {
  const cols = columns.filter((c) => c.copy !== false);
  const lines: string[] = [cols.map((c) => tsvCell(c.title)).join('\t')];
  for (const row of rows) {
    const rec = row as unknown as Record<string, unknown>;
    lines.push(cols.map((c) => tsvCell(c.copy ? c.copy(row) : rec[c.key])).join('\t'));
  }
  return lines.join('\r\n');
}

/** 写系统剪贴板（Wails 运行时）；失败抛错，由调用方转成中文提示。 */
export async function copyTSV(text: string): Promise<void> {
  await Clipboard.SetText(text);
}
