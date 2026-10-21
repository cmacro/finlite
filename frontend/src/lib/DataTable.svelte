<!-- 高性能数据表（财务主控件）：固定列宽 + 粘性表头 + 分组行 + 整表复制到 Excel。
     设计取舍：**不做虚拟滚动**——全量行始终是真实 DOM，框选 / Ctrl+A 后可原样粘进 Excel；
     性能靠三件事：① 固定 table-layout（列宽由 colgroup 决定，浏览器单趟布局）；
     ② 每行共用一个点击处理函数、按 data-act 分发（不建 N 行闭包）；
     ③ 行详情一律走右侧抽屉，表内不插入展开行（插行会触发整张长表重排）。
     页面只声明 columns（含 cell snippet）与 rows；列宽/表头/分组行由组件统一渲染。 -->
<script lang="ts" generics="T">
  import type { Snippet } from 'svelte';
  import type { TableColumn } from './table';

  interface Props {
    /** 数据行（后端已排好序；分组列由 groupBy 指定，需连续） */
    rows: T[];
    /** 列定义（按展示顺序） */
    columns: TableColumn<T>[];
    /** 行唯一键 */
    rowKey: (row: T) => string;
    /** 表格无障碍名称 */
    label?: string;
    /** 表格最小宽度（固定布局下等于列宽合计） */
    minWidth?: string;
    /** 滚动区最大高度 */
    maxHeight?: string;
    /** 表格附加 class（页面用自有样式时传入，如 bf-table / of-table） */
    tableClass?: string;
    /** 行附加 class（如 bf-row bf-row-mismatch） */
    rowClass?: (row: T) => string;
    /** 分组键：与上一行不同时插入分组行（数据须按该键连续排列） */
    groupBy?: (row: T) => string;
    /** 分组行内容（参数为分组键，聚合数据由父页面按 key 查表） */
    groupHead?: Snippet<[string]>;
    /** 分组行附加 class */
    groupClass?: string;
    /** 点击数据行（带 data-act 的按钮不触发） */
    onRowClick?: (row: T) => void;
    /** 点击行内带 data-act 的元素；act 取该属性值 */
    onAct?: (act: string, row: T) => void;
    /** 表格工具条（标题 / 图例 / 复制按钮等） */
    toolbar?: Snippet;
  }

  let {
    rows,
    columns,
    rowKey,
    label = '数据表',
    minWidth = '1120px',
    maxHeight = 'calc(100vh - 424px)',
    tableClass = '',
    rowClass,
    groupBy,
    groupHead,
    groupClass = '',
    onRowClick,
    onAct,
    toolbar,
  }: Props = $props();

  const colCount = $derived(columns.length);

  /** 默认单元格：按 key 取原值（不做格式化，格式化由列 cell 负责） */
  function cellText(row: T, key: string): string {
    const v = (row as unknown as Record<string, unknown>)[key];
    return v === null || v === undefined ? '' : String(v);
  }

  // 所有行共用同一个处理函数（不是每行一个闭包）：命中带 data-act 的元素即视为行内动作，
  // 否则按行点击处理（打开右侧详情抽屉）。
  function onClick(e: MouseEvent): void {
    const tr = e.currentTarget as HTMLElement | null;
    if (!tr) return;
    const row = rows[Number(tr.dataset.dtIndex)];
    if (row === undefined) return;
    const target = e.target as HTMLElement | null;
    const actEl = target?.closest<HTMLElement>('[data-act]') ?? null;
    if (actEl?.dataset.act) {
      onAct?.(actEl.dataset.act, row);
      return;
    }
    onRowClick?.(row);
  }
</script>

{#if toolbar}{@render toolbar()}{/if}

<div class="dt-scroll" style:max-height={maxHeight}>
  <table class="dt-table {tableClass}" style:min-width={minWidth} aria-label={label}>
    <colgroup>
      {#each columns as col (col.key)}
        <col style:width={col.width} />
      {/each}
    </colgroup>
    <thead>
      <tr>
        {#each columns as col (col.key)}
          <th class="{col.align === 'right' ? 'num' : ''} {col.headClass ?? ''}">{col.title}</th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each rows as row, i (rowKey(row))}
        {#if groupBy && groupHead}
          {@const g = groupBy(row)}
          {#if i === 0 || groupBy(rows[i - 1]) !== g}
            <tr class="dt-group {groupClass}">
              <td colspan={colCount}>{@render groupHead(g)}</td>
            </tr>
          {/if}
        {/if}
        <tr class="dt-row {rowClass?.(row) ?? ''}" data-dt-index={i} onclick={onClick}>
          {#each columns as col (col.key)}
            <td
              class="{col.align === 'right' ? 'num' : ''} {col.class ?? ''}"
              title={col.hint?.(row)}
            >
              {#if col.cell}{@render col.cell(row)}{:else}{cellText(row, col.key)}{/if}
            </td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>
