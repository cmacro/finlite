<!-- 订单流水（F3）：只读提示条 + 统计卡 + 筛选条 + 按结算日期（T+1）归集的密集表格
     （点击行打开右侧详情抽屉看明细与手续费复算）；原始表只读，页面无编辑/删除入口。
     数据经 OrderFlowService.List（Wails 绑定）：一次性返回全部明细，费率复算与合计均由 Go decimal 完成。
     视觉对齐 docs/uidesign/order-flow.html；金额只格式化，不做业务计算。 -->
<script lang="ts">
  import { OrderFlowService } from '../../bindings/finlite/internal/services';
  import type {
    OrderFlow,
    OrderFlowFilter,
    OrderFlowGroup,
    OrderFlowPage,
  } from '../../bindings/finlite/internal/services/models';
  import DataTable from '../lib/DataTable.svelte';
  import Icon from '../lib/Icon.svelte';
  import { buildTSV, copyTSV, type TableColumn } from '../lib/table';
  import {
    feeFlagText,
    formatDateTime,
    formatMoney,
    formatSignedMoney,
    prettyJson,
    weekdayCN,
  } from '../lib/format';
  import type { RouteKey } from '../lib/types';

  interface Props {
    onGoto?: (key: RouteKey) => void;
  }
  let { onGoto }: Props = $props();

  type ToastTone = 'info' | 'ok' | 'warn' | 'danger';

  // 列表数据整体替换、内部不再改写：用 $state.raw 避免把 2000+ 行包成深层响应式代理。
  let page = $state.raw<OrderFlowPage | null>(null);

  // 输入框与「已生效筛选」分离：日期变更/回车/查询按钮才应用（对齐原型交互）。
  let settleInput = $state('');
  let kwInput = $state('');
  let onlyFeeAdj = $state(false);
  let filter = $state<OrderFlowFilter>({ settleDate: '', keyword: '', onlyFeeAdj: '' });

  let busy = $state(false);
  let loadError = $state('');
  let drawerRow = $state<OrderFlow | null>(null);

  const groups = $derived<OrderFlowGroup[]>(page?.groups ?? []);

  // 表格按「结算日期」分组渲染：摊平成行列表 + 分组聚合查表（分组行标题用）。
  const flat = $derived.by(() => {
    const items: OrderFlow[] = [];
    const byDate = new Map<string, OrderFlowGroup>();
    for (const g of page?.groups ?? []) {
      byDate.set(g.settleDate, g);
      for (const o of g.items ?? []) items.push(o);
    }
    return { items, byDate };
  });
  const rows = $derived(flat.items);
  const groupByDate = $derived(flat.byDate);

  const stats = $derived({
    total: page?.total ?? 0,
    orderTotal: page?.orderTotal ?? '0.00',
    feeTotal: page?.feeTotal ?? '0.00',
    netTotal: page?.netTotal ?? '0.00',
    reconDiff: page?.reconDiff ?? '0.00',
    feeDiff: page?.feeDiffCount ?? 0,
    netDiff: page?.netDiffCount ?? 0,
    parseErr: page?.parseErrCount ?? 0,
    anomaly: page?.anomalyCount ?? 0,
  });

  // ---------- 列定义（宽度固定，与分组表头 colgroup 一致；导出取后端原始 decimal 字符串） ----------
  const columns: TableColumn<OrderFlow>[] = [
    { key: 'icbcOrderNo', title: '工行订单号', width: '100px', class: 'mono', hint: (o) => o.icbcOrderNo },
    { key: 'thirdOrderNo', title: '第三方订单号', width: '136px', class: 'mono', cell: thirdCell, hint: (o) => o.thirdOrderNo },
    { key: 'txnTime', title: '交易时间', width: '146px', class: 'tnum' },
    { key: 'settleDate', title: '结算日期', width: '102px', class: 'tnum' },
    { key: 'orderAmount', title: '订单金额', width: '104px', align: 'right', class: 'num', cell: orderCell },
    { key: 'feeAmount', title: '手续费', width: '90px', align: 'right', class: 'num', cell: feeCell },
    { key: 'netAmount', title: '实收', width: '104px', align: 'right', class: 'num', cell: netCell },
    { key: 'payMethod', title: '支付方式', width: '82px', cell: payCell },
    { key: 'debitCreditFlag', title: '借贷记', width: '74px', cell: cardCell },
    {
      key: 'feeFlag',
      title: '费率检查',
      width: '126px',
      cell: feeBadge,
      copy: (o) => feeFlagText(o.feeFlag) || '费率正确',
    },
    { key: 'ops', title: '操作', width: '78px', cell: opsCell, copy: false },
  ];

  const rowKeyOf = (o: OrderFlow): string => o.id;
  const rowClassOf = (o: OrderFlow): string => {
    const tone = rowTone(o.feeFlag);
    return `of-row ${tone === 'warn' ? 'row-warn' : tone === 'err' ? 'row-err' : ''}`;
  };
  const settleDateOf = (o: OrderFlow): string => o.settleDate;

  // ---------- Toast ----------
  let toastSeq = 0;
  let toasts = $state<{ id: number; tone: ToastTone; text: string }[]>([]);

  function toast(tone: ToastTone, text: string): void {
    const id = ++toastSeq;
    toasts = [...toasts, { id, tone, text }];
    setTimeout(() => dismissToast(id), 4200);
  }
  function dismissToast(id: number): void {
    toasts = toasts.filter((t) => t.id !== id);
  }

  // ---------- 数据加载 ----------
  async function load(): Promise<void> {
    busy = true;
    try {
      page = await OrderFlowService.List(filter);
      loadError = '';
    } catch (e) {
      page = null;
      loadError = `加载订单流水失败：${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busy = false;
    }
  }

  async function doQuery(): Promise<void> {
    filter = {
      settleDate: settleInput,
      keyword: kwInput.trim(),
      onlyFeeAdj: onlyFeeAdj ? '1' : '',
    };
    await load();
    if (!loadError) toast('info', `查询完成，命中 ${stats.total} 条订单明细。`);
  }

  function doReset(): void {
    settleInput = '';
    kwInput = '';
    onlyFeeAdj = false;
    filter = { settleDate: '', keyword: '', onlyFeeAdj: '' };
    void load();
    toast('info', '筛选条件已重置，展示全部订单流水。');
  }

  // 顶栏刷新按钮（App.svelte 广播）：重拉订单流水。
  $effect(() => {
    const onRefresh = () => void load();
    window.addEventListener('finlite:refresh', onRefresh);
    return () => window.removeEventListener('finlite:refresh', onRefresh);
  });

  // ---------- 行交互 ----------

  // ---------- 复制到 Excel ----------
  async function copyTable(): Promise<void> {
    if (rows.length === 0) {
      toast('info', '当前没有可复制的数据。');
      return;
    }
    try {
      await copyTSV(buildTSV(columns, rows));
      toast('ok', `已复制 ${rows.length} 行到剪贴板，可直接粘贴到 Excel。`);
    } catch (e) {
      toast('danger', `复制失败：${e instanceof Error ? e.message : String(e)}`);
    }
  }
  function openDrawer(row: OrderFlow): void {
    drawerRow = row;
  }
  function closeDrawer(): void {
    drawerRow = null;
  }
  /** 行内按钮（data-act）：打开行详情。 */
  function onRowAct(act: string, o: OrderFlow): void {
    if (act === 'drawer') openDrawer(o);
  }
  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && drawerRow) closeDrawer();
  }

  // ---------- 展示映射（只做格式化，不做算术） ----------
  type Tone = '' | 'warn' | 'err';

  function rowTone(flag: string): Tone {
    if (flag === 'fee_diff') return 'warn';
    if (flag === 'net_diff' || flag === 'fee_parse_err') return 'err';
    return '';
  }

  /** 金额是否为 0（字符串判断，避免前端做算术）。 */
  function isZeroAmount(v: string | null | undefined): boolean {
    return /^[+-]?0(?:\.0+)?$/.test(String(v ?? '').trim());
  }

  function payTone(pay: string): string {
    const p = (pay || '').toLowerCase();
    if (pay.includes('微信') || p.includes('wechat')) return 'of-pay-wechat';
    if (pay.includes('支付宝') || p.includes('alipay')) return 'of-pay-alipay';
    return '';
  }

  function posBatchNo(settleDate: string): string {
    return `POS-${settleDate.replace(/-/g, '')}`;
  }

  function statusMessage(o: OrderFlow): string {
    switch (o.feeFlag) {
      case 'fee_diff':
        return `实际手续费与按 ${o.feeRate} 复算的预期手续费不符，请核对商户费率或导出字段。`;
      case 'net_diff':
        return '订单金额 − 手续费 与记录的商户实收不一致，请人工复核。';
      case 'fee_parse_err':
        return '手续费字段无法解析，费率复算已跳过，请核对源文件。';
      default:
        return '费率与实收恒等式校验通过，无需人工干预。';
    }
  }

  load();
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet feeBadge(o: OrderFlow)}
  {#if o.feeFlag === 'fee_diff'}
    <span class="of-badge of-badge-warn">{feeFlagText(o.feeFlag)}</span>
  {:else if o.feeFlag === 'net_diff' || o.feeFlag === 'fee_parse_err'}
    <span class="of-badge of-badge-err">{feeFlagText(o.feeFlag)}</span>
  {:else}
    <span class="of-rate-ok">费率正确</span>
  {/if}
{/snippet}

{#snippet thirdCell(o: OrderFlow)}
  {#if o.thirdOrderNo}
    {o.thirdOrderNo}
  {:else}
    <span class="of-none">—</span>
  {/if}
{/snippet}

{#snippet orderCell(o: OrderFlow)}¥{formatMoney(o.orderAmount)}{/snippet}

{#snippet feeCell(o: OrderFlow)}
  {#if o.feeFlag === 'fee_parse_err'}
    <span class="of-fee-fail">解析失败</span>
  {:else}
    ¥{formatMoney(o.feeAmount)}
  {/if}
{/snippet}

{#snippet netCell(o: OrderFlow)}¥{formatMoney(o.netAmount)}{/snippet}

{#snippet payCell(o: OrderFlow)}
  <span class="of-pay {payTone(o.payMethod)}">
    <span class="dot"></span>{o.payMethod || '—'}
  </span>
{/snippet}

{#snippet cardCell(o: OrderFlow)}
  <span class="of-card">{o.debitCreditFlag || '—'}</span>
{/snippet}

{#snippet opsCell()}
  <button class="link-btn" data-act="drawer">查看详情</button>
{/snippet}

<section>
  <!-- ============ 只读提示条 ============ -->
  {#if loadError}
    <div class="of-banner danger" role="alert">
      <span class="ib-ico"><Icon name="alert-triangle" size={18} /></span>
      <div>
        <div class="ib-title">加载失败</div>
        <div class="ib-body">{loadError}</div>
      </div>
    </div>
  {:else}
    <div class="of-banner" role="note">
      <span class="ib-ico"><Icon name="info" size={18} /></span>
      <div>
        <div class="ib-title">收入归属规则：按结算日期（T+1）入账，非交易时间</div>
        <div class="ib-body">
          本表为工行商户后台导出的订单原始明细，交易级只读数据，页面不提供编辑 / 删除。
          金额恒等式 <code>订单金额 − 手续费 = 实收</code>，允许 <b>±0.01</b> 元舍入误差；超出阈值即标记为差异行，需人工复核。
        </div>
      </div>
    </div>
  {/if}

  <!-- ============ 统计卡 ============ -->
  <section class="of-stats">
    <div class="of-stat">
      <div class="of-stat-label">订单笔数</div>
      <div class="of-stat-value">{stats.total}<span class="cur">笔</span></div>
      <div class="of-stat-sub">全部展示 · 未分页</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">订单金额合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.orderTotal)}</div>
      <div class="of-stat-sub">{stats.total} 笔命中 · 全部展示</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">手续费合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.feeTotal)}</div>
      <div class="of-stat-sub">微信 / 支付宝 0.25% · 借记卡 0.5% · 贷记卡 0.6%</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">商户实收合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.netTotal)}</div>
      <div class="of-stat-sub">订单金额 − 手续费</div>
    </div>
    <div class="of-stat warn">
      <div class="of-stat-label">费率异常笔数</div>
      <div class="of-stat-value">{stats.anomaly}<span class="cur">笔</span></div>
      <div class="of-stat-sub">
        费率差异 {stats.feeDiff} · 实收差异 {stats.netDiff} · 解析失败 {stats.parseErr}
      </div>
    </div>
  </section>

  <!-- ============ 筛选条 ============ -->
  <section class="of-filters">
    <div class="of-field">
      <span class="fl">结算日期</span>
      <input
        type="date"
        aria-label="结算日期"
        bind:value={settleInput}
        onchange={() => void doQuery()}
      />
    </div>
    <div class="of-sep"></div>
    <div class="of-field">
      <span class="fl">关键字</span>
      <input
        type="text"
        placeholder="工行订单号 / 第三方订单号"
        aria-label="关键字"
        bind:value={kwInput}
        onkeydown={(e) => {
          if (e.key === 'Enter') void doQuery();
        }}
      />
    </div>
    <div class="of-sep"></div>
    <label class="of-switch">
      <input
        type="checkbox"
        bind:checked={onlyFeeAdj}
        onchange={() => void doQuery()}
      />
      <span class="track"><span class="thumb"></span></span>
      <span class="sw-label">仅看费率差异</span>
    </label>
    <button class="btn btn-primary" onclick={() => void doQuery()} disabled={busy}>
      <Icon name="search" size={15} />
      查询
    </button>
    <button class="btn" onclick={doReset} disabled={busy}>重置</button>
    <div class="of-spacer"></div>
    <button
      class="btn btn-sm"
      onclick={() => void copyTable()}
      disabled={busy || rows.length === 0}
      title="复制当前筛选的全部行（TSV，可直接粘贴到 Excel，金额为数值）"
    >
      <Icon name="file" size={14} />
      复制表格
    </button>
    <div class="of-count">共 <b>{stats.total}</b> 条 · 全部展示</div>
  </section>

  <!-- ============ 表格卡片 ============ -->
  <section class="of-table-card">
    {#if groups.length === 0}
      <div class="of-empty">
        <div class="of-empty-ico"><Icon name="receipt" size={26} /></div>
        <div class="of-empty-title">暂无数据，请先在导入中心导入订单流水</div>
        <div class="of-empty-sub">
          订单流水来自工行商户后台导出的 TSV 文件，导入后按结算日期（T+1）自动归集与勾稽。
        </div>
        <button class="btn btn-primary" onclick={() => onGoto?.('import')}>
          <Icon name="upload" size={15} />
          前往导入中心
        </button>
      </div>
    {:else}
      <DataTable
        rows={rows}
        {columns}
        rowKey={rowKeyOf}
        rowClass={rowClassOf}
        label="订单流水明细"
        tableClass="of-table"
        minWidth="1142px"
        maxHeight="calc(100vh - 430px)"
        groupBy={settleDateOf}
        groupClass="of-group-row"
        onRowClick={openDrawer}
        onAct={onRowAct}
      >
        {#snippet groupHead(settleDate: string)}
          {@const g = groupByDate.get(settleDate)}
          {#if g}
            <div class="of-group-inner">
              <div class="of-group-date">
                <span class="gd-k">结算日期</span>
                <span class="gd-v">{g.settleDate}</span>
                <span class="gd-wd">· {weekdayCN(g.settleDate)}</span>
                <span class="gd-n">· {g.count} 笔</span>
              </div>
              <div class="of-group-pos">
                银行 POS 到账（次日凌晨）
                {#if g.anomalyCount > 0}
                  <span class="of-badge of-badge-warn">待勾稽</span>
                {:else}
                  <span class="of-badge of-badge-ok">已匹配</span>
                {/if}
              </div>
              <div class="of-group-spacer"></div>
              <div class="of-group-subs">
                <span class="of-group-sub">订单金额 <b>¥{formatMoney(g.orderTotal)}</b></span>
                <span class="of-group-sub">手续费 <b>¥{formatMoney(g.feeTotal)}</b></span>
                <span class="of-group-sub">实收 <b>¥{formatMoney(g.netTotal)}</b></span>
                {#if g.parseErrCount > 0}
                  <span class="of-group-note">· {g.parseErrCount} 笔解析失败未计入手续费小计</span>
                {/if}
              </div>
            </div>
          {/if}
        {/snippet}
      </DataTable>

      <!-- ============ 表尾合计 ============ -->
      <div class="of-summary">
        <div class="of-summary-left">当前筛选合计 · 共 <b>{stats.total}</b> 条 · 全部展示</div>
        <div class="of-summary-metrics">
          <div class="of-sm"><span class="sm-k">订单金额</span><span class="sm-v">¥{formatMoney(stats.orderTotal)}</span></div>
          <div class="of-sm"><span class="sm-k">手续费</span><span class="sm-v">¥{formatMoney(stats.feeTotal)}</span></div>
          <div class="of-sm"><span class="sm-k">商户实收</span><span class="sm-v">¥{formatMoney(stats.netTotal)}</span></div>
          {#if stats.total > 0 && !isZeroAmount(stats.reconDiff)}
            <span class="of-sm-chip">勾稽差额 {formatSignedMoney(stats.reconDiff)}</span>
          {/if}
        </div>
      </div>
    {/if}
  </section>
</section>

<!-- ============ 行详情抽屉 ============ -->
{#if drawerRow}
  {@const o = drawerRow}
  {@const tone = rowTone(o.feeFlag)}
  <div class="of-drawer-overlay" role="presentation" onclick={closeDrawer}></div>
  <div class="of-drawer" role="dialog" aria-modal="true" aria-label="订单详情" tabindex="-1">
    <div class="of-drawer-head">
      <div>
        <div class="dh-title">订单详情</div>
        <div class="dh-sub">{o.icbcOrderNo}</div>
      </div>
      <button class="of-drawer-close" aria-label="关闭" onclick={closeDrawer}>
        <Icon name="x" size={16} />
      </button>
    </div>
    <div class="of-drawer-body">
      <div class="of-dstatus {tone === 'warn' ? 'warn' : tone === 'err' ? 'err' : 'ok'}">
        <span class="ico">
          <Icon name={tone === '' ? 'circle-check' : 'alert-triangle'} size={15} />
        </span>
        <span>
          <b>{feeFlagText(o.feeFlag) || '费率正确'}</b> · {statusMessage(o)}
        </span>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">订单标识</div>
        <div class="of-dgrid">
          <div class="of-dfield wide"><span class="dk">工行订单号</span><span class="dv mono">{o.icbcOrderNo}</span></div>
          <div class="of-dfield wide">
            <span class="dk">第三方订单号</span>
            <span class="dv mono {o.thirdOrderNo ? '' : 'empty-v'}">{o.thirdOrderNo || '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">支付方式</span><span class="dv">{o.payMethod || '—'}</span></div>
          <div class="of-dfield"><span class="dk">借贷记</span><span class="dv">{o.debitCreditFlag || '—'}</span></div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">金额与费率</div>
        <div class="of-dgrid">
          <div class="of-dfield"><span class="dk">订单金额</span><span class="dv num">¥{formatMoney(o.orderAmount)}</span></div>
          <div class="of-dfield">
            <span class="dk">手续费</span>
            <span class="dv num {o.feeFlag === 'fee_parse_err' ? 'err' : ''}">
              {o.feeFlag === 'fee_parse_err' ? '解析失败' : `¥${formatMoney(o.feeAmount)}`}
            </span>
          </div>
          <div class="of-dfield"><span class="dk">商户实收</span><span class="dv num">¥{formatMoney(o.netAmount)}</span></div>
          <div class="of-dfield">
            <span class="dk">预期手续费（{o.feeRate}）</span>
            <span class="dv num">{o.feeFlag === 'fee_parse_err' ? '—' : `¥${formatMoney(o.expectedFee)}`}</span>
          </div>
          <div class="of-dfield"><span class="dk">费率检查</span><span class="dv">{feeFlagText(o.feeFlag) || '费率正确'}</span></div>
          <div class="of-dfield">
            <span class="dk">复核状态</span>
            <span class="dv">{o.feeFlag ? '待人工复核' : '无需复核'}</span>
          </div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">时间与收入归属（T+1）</div>
        <div class="of-attr">
          <b>收入归属日 = 结算日期，非交易时间。</b>工行商户订单按 T+1 结算，本笔交易于交易时间发生，次一自然日入账。
          <div class="of-attr-flow">
            <span class="of-af-node">交易时间 {o.txnTime}</span>
            <span class="of-af-arrow">→</span>
            <span class="of-af-node hl">结算日期 {o.settleDate}（收入归属日）</span>
          </div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">数据来源</div>
        <div class="of-dgrid">
          <div class="of-dfield wide">
            <span class="dk">来源文件</span>
            <span class="dv {o.fileName ? '' : 'empty-v'}">{o.fileName || '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">导入批次</span><span class="dv mono">{o.batchId}</span></div>
          <div class="of-dfield"><span class="dk">导入时间</span><span class="dv">{formatDateTime(o.importedAt)}</span></div>
          <div class="of-dfield">
            <span class="dk">原始行号</span>
            <span class="dv {o.sourceRowNo > 0 ? '' : 'empty-v'}">{o.sourceRowNo > 0 ? `第 ${o.sourceRowNo} 行` : '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">数据性质</span><span class="dv">原始数据 · 只读</span></div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">原始 JSON 摘要</div>
        <pre class="of-json">{prettyJson(o.rawJson)}</pre>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">关联勾稽</div>
        <div class="of-recon-list">
          <div class="of-recon-item">
            <span class="ri-ic"><Icon name="wallet" size={14} /></span>
            <div class="of-recon-main">
              <div class="of-recon-k">银行 POS 到账（次日凌晨）</div>
              <div class="of-recon-v">{o.settleDate} · 结算批次 {posBatchNo(o.settleDate)}</div>
            </div>
            {#if o.feeFlag}
              <span class="of-badge of-badge-warn">待勾稽</span>
            {:else}
              <span class="of-badge of-badge-ok">已匹配</span>
            {/if}
          </div>
          <div class="of-recon-item">
            <span class="ri-ic"><Icon name="bar-chart" size={14} /></span>
            <div class="of-recon-main">
              <div class="of-recon-k">数据汇总 · 店面收款归集</div>
              <div class="of-recon-v">按结算日期并入 {o.settleDate} 日汇总</div>
            </div>
            <span class="of-badge of-badge-ok">已归集</span>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- ============ Toast ============ -->
<div class="of-toasts">
  {#each toasts as t (t.id)}
    <div class="of-toast {t.tone}">
      <span class="of-toast-bar"></span>
      <div class="of-toast-body">{t.text}</div>
      <button class="of-toast-close" aria-label="关闭" onclick={() => dismissToast(t.id)}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/each}
</div>
