<!-- 银行流水（F2）：批次信息栏 + 统计卡 + 密集表格（三色余额状态）+ 行内余额衔接 + 右侧详情抽屉。
     数据经 BankFlowService.List / Batches / ConfirmBatch / UnconfirmBatch / ExportTSV（Wails 绑定）。
     原始表只读：本页无任何编辑入口；确认仅写 balance_confirmed_at，可撤销（二次确认）。
     视觉对齐 docs/uidesign/bank-flow.html；金额展示只格式化，不在此做算术。 -->
<script lang="ts">
  import { Dialogs } from '@wailsio/runtime';
  import { BankFlowService } from '../../bindings/finlite/internal/services';
  import type {
    BankFlow,
    BankFlowBatch,
    BankFlowFilter,
    BankFlowPage,
  } from '../../bindings/finlite/internal/services/models';
  import Icon from '../lib/Icon.svelte';
  import Confirm from '../lib/Confirm.svelte';
  import {
    bankRowState,
    formatDateTime,
    formatMoney,
    formatSignedMoney,
    prettyJson,
  } from '../lib/format';
  import type { RouteKey } from '../lib/types';

  interface Props {
    onGoto?: (key: RouteKey) => void;
  }
  let { onGoto }: Props = $props();

  type AlertTone = 'info' | 'ok' | 'warn' | 'danger';
  type ToastTone = AlertTone;

  let page = $state<BankFlowPage | null>(null);
  let batches = $state<BankFlowBatch[]>([]);
  let filter = $state<BankFlowFilter>({ batchId: '', dateFrom: '', dateTo: '', keyword: '' });

  // 输入框与「已生效筛选」分离：查询按钮才应用日期/关键字（对齐原型交互）。
  let fromInput = $state('');
  let toInput = $state('');
  let kwInput = $state('');

  let busy = $state(false);
  let alert = $state<{ tone: AlertTone; msg: string } | null>(null);
  let expanded = $state<Record<string, boolean>>({});
  let checked = $state<Record<string, boolean>>({});
  let drawerRow = $state<BankFlow | null>(null);
  let dialogMode = $state<'' | 'confirm' | 'unconfirm'>('');

  let initialized = false;

  const items = $derived(page?.items ?? []);
  const selectedBatch = $derived(batches.find((b) => b.batchId === filter.batchId) ?? null);
  const batchConfirmed = $derived(!!selectedBatch?.confirmedAt);
  const allMismatch = $derived(batches.reduce((n, b) => n + b.mismatchCount, 0));
  const allConfirmedMismatch = $derived(batches.reduce((n, b) => n + b.confirmedCount, 0));
  const allTotal = $derived(batches.reduce((n, b) => n + b.successCount, 0));

  const stats = $derived({
    total: page?.total ?? 0,
    totalIn: page?.totalIn ?? '0.00',
    totalOut: page?.totalOut ?? '0.00',
    inCount: page?.inCount ?? 0,
    outCount: page?.outCount ?? 0,
    mismatch: page?.mismatchCount ?? 0,
    confirmed: page?.confirmedCount ?? 0,
    skip: page?.skipCount ?? 0,
  });

  const dialogMessage = $derived(
    dialogMode === 'confirm'
      ? `确认本批次余额连续性吗？确认后 ${selectedBatch?.mismatchCount ?? 0} 行不平的红底将消除，之后可撤销。\n批次：${selectedBatch?.fileName ?? ''}`
      : `撤销本批次的余额确认吗？不平行的行将恢复红底显示。\n批次：${selectedBatch?.fileName ?? ''}`
  );

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
      page = await BankFlowService.List(filter);
    } catch (e) {
      page = null;
      alert = {
        tone: 'danger',
        msg: `加载银行流水失败：${e instanceof Error ? e.message : String(e)}`,
      };
    } finally {
      busy = false;
    }
  }

  async function loadBatches(): Promise<void> {
    try {
      batches = (await BankFlowService.Batches()) ?? [];
      // 侧栏徽标：全部批次的余额不平笔数（不随筛选变化）。
      window.dispatchEvent(
        new CustomEvent('finlite:bankflow-mismatch', { detail: allMismatch })
      );
    } catch {
      batches = [];
    }
  }

  async function boot(): Promise<void> {
    await loadBatches();
    if (!initialized) {
      initialized = true;
      if (batches.length > 0) {
        filter = { ...filter, batchId: batches[0].batchId };
      }
    }
    await load();
    if (!alert && batches.length > 0) {
      const mis = selectedBatch ? selectedBatch.mismatchCount : stats.mismatch;
      alert =
        mis > 0
          ? {
              tone: 'info',
              msg: `本页为网银次日导出的原始流水（只读）。当前视图含 ${mis} 笔余额不平，请确认后继续。`,
            }
          : {
              tone: 'ok',
              msg: '本页为网银次日导出的原始流水（只读）。当前视图余额衔接全部正常。',
            };
    }
  }

  function applyBatch(id: string): void {
    filter = { ...filter, batchId: id };
    expanded = {};
    void load();
  }

  async function doQuery(): Promise<void> {
    filter = {
      batchId: filter.batchId,
      dateFrom: fromInput,
      dateTo: toInput,
      keyword: kwInput.trim(),
    };
    await load();
    toast('info', `查询完成，命中 ${stats.total} 条记录。`);
  }

  function doReset(): void {
    fromInput = '';
    toInput = '';
    kwInput = '';
    filter = { batchId: filter.batchId, dateFrom: '', dateTo: '', keyword: '' };
    void load();
  }

  // ---------- 确认 / 撤销 ----------
  function askConfirm(): void {
    if (!selectedBatch || batchConfirmed) return;
    dialogMode = 'confirm';
  }
  function askUnconfirm(): void {
    if (!selectedBatch || !batchConfirmed) return;
    dialogMode = 'unconfirm';
  }

  async function doDialogConfirm(): Promise<void> {
    const mode = dialogMode;
    dialogMode = '';
    const batch = selectedBatch;
    if (!mode || !batch) return;
    const mis = mode === 'confirm' ? batch.mismatchCount : batch.confirmedCount;
    busy = true;
    try {
      if (mode === 'confirm') {
        const n = await BankFlowService.ConfirmBatch(batch.batchId, 'local');
        await loadBatches();
        await load();
        alert = { tone: 'ok', msg: `已确认批次余额（${mis} 行红底消除），可撤销。` };
        toast('ok', `${batch.fileName} 已确认，${n} 行余额已人工确认。`);
      } else {
        const n = await BankFlowService.UnconfirmBatch(batch.batchId);
        await loadBatches();
        await load();
        alert = { tone: 'warn', msg: `已撤销本批次余额确认，${mis} 行不平恢复红底显示。` };
        toast('warn', `${batch.fileName} 已撤销确认（${n} 行）。`);
      }
    } catch (e) {
      const msg = `${mode === 'confirm' ? '确认' : '撤销确认'}失败：${e instanceof Error ? e.message : String(e)}`;
      alert = { tone: 'danger', msg };
      toast('danger', msg);
    } finally {
      busy = false;
    }
  }

  // ---------- 导出 TSV ----------
  async function exportTsv(): Promise<void> {
    busy = true;
    try {
      const path = await Dialogs.SaveFile({
        Title: '导出银行流水 TSV',
        Filename: selectedBatch
          ? `${selectedBatch.fileName.replace(/\.tsv$/i, '')}_导出.tsv`
          : '银行流水_全部批次.tsv',
        Filters: [{ DisplayName: 'TSV 文件', Pattern: '*.tsv' }],
      });
      if (!path) return;
      const n = await BankFlowService.ExportTSV(path, filter);
      const msg = `已按当前筛选导出 ${n} 条记录：${path}`;
      alert = { tone: 'ok', msg };
      toast('ok', msg);
    } catch (e) {
      const msg = `导出失败：${e instanceof Error ? e.message : String(e)}`;
      alert = { tone: 'danger', msg };
      toast('danger', msg);
    } finally {
      busy = false;
    }
  }

  // ---------- 行交互 ----------
  function toggleExpand(id: string): void {
    expanded = { ...expanded, [id]: !expanded[id] };
  }

  function toggleChecked(id: string): void {
    checked = { ...checked, [id]: !checked[id] };
    toast('ok', checked[id] ? '已标记该行为「已核对」（本地标注）。' : '已取消「已核对」标记。');
  }

  function openDrawer(row: BankFlow): void {
    drawerRow = row;
  }
  function closeDrawer(): void {
    drawerRow = null;
  }

  function batchRowsOf(row: BankFlow): BankFlow[] {
    return items.filter((x) => x.batchId === row.batchId);
  }
  function neighborsOf(row: BankFlow): { prev: BankFlow | null; next: BankFlow | null } {
    const rows = batchRowsOf(row);
    const i = rows.findIndex((x) => x.id === row.id);
    return {
      prev: i > 0 ? rows[i - 1] : null,
      next: i >= 0 && i < rows.length - 1 ? rows[i + 1] : null,
    };
  }

  function locateNeighbor(row: BankFlow): void {
    const { prev, next } = neighborsOf(row);
    const target = prev ?? next;
    if (!target) {
      toast('info', '该行没有相邻行可定位。');
      return;
    }
    const el = document.querySelector<HTMLElement>(`[data-row-id="${target.id}"]`);
    if (el) {
      el.scrollIntoView({ block: 'center' });
      el.classList.remove('bf-flash');
      void el.offsetWidth;
      el.classList.add('bf-flash');
    }
    toast('info', `已定位到相邻行：${target.txnTime} · 余额 ${formatMoney(target.balance)}`);
  }

  /** 金额是否非零（只做字符串判断，不做算术）。 */
  function nonzero(v: string): boolean {
    return !/^[+-]?0(?:\.0+)?$/.test(v.trim());
  }

  function isExpanded(id: string): boolean {
    return !!expanded[id];
  }
  function isChecked(id: string): boolean {
    return !!checked[id];
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && drawerRow) closeDrawer();
  }

  // 顶栏刷新 / 导出 TSV（App.svelte 广播）。
  $effect(() => {
    const onRefresh = () => {
      void loadBatches();
      void load();
    };
    const onExport = () => {
      void exportTsv();
    };
    window.addEventListener('finlite:refresh', onRefresh);
    window.addEventListener('finlite:export-bankflow', onExport);
    return () => {
      window.removeEventListener('finlite:refresh', onRefresh);
      window.removeEventListener('finlite:export-bankflow', onExport);
    };
  });

  boot();
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet stateBadge(state: string)}
  {#if state === 'confirmed'}
    <span class="bf-badge bf-badge-conf"><Icon name="check" size={11} />已确认</span>
  {:else if state === 'mismatch'}
    <span class="bf-badge bf-badge-err"><Icon name="alert-triangle" size={11} />不平</span>
  {:else if state === 'skip'}
    <span class="bf-badge bf-badge-skip">跳过</span>
  {:else}
    <span class="bf-badge bf-badge-ok"><Icon name="check" size={11} />正常</span>
  {/if}
{/snippet}

<section>
  <!-- ============ 提示条 ============ -->
  {#if alert}
    <div class="bf-alert bf-alert-{alert.tone}">
      <span class="ico">
        <Icon
          name={alert.tone === 'ok' ? 'circle-check' : alert.tone === 'danger' ? 'x' : alert.tone === 'warn' ? 'alert-triangle' : 'info'}
          size={16}
        />
      </span>
      <span class="txt">{alert.msg}</span>
      <button class="close" aria-label="关闭" onclick={() => (alert = null)}>
        <Icon name="x" size={15} />
      </button>
    </div>
  {/if}

  <!-- ============ 批次信息栏 ============ -->
  <section class="bf-batch">
    <div class="bf-batch-main">
      <div class="bf-batch-file">
        <span class="ico"><Icon name="file" size={17} /></span>
        {#if selectedBatch}
          <span>{selectedBatch.fileName}</span>
        {:else}
          <span class="alllabel">全部批次</span>
          <span class="text-muted">· {batches.length} 个文件</span>
        {/if}
      </div>
      <div class="bf-batch-meta">
        {#if selectedBatch}
          <span class="bf-kv">导入时间 <b>{formatDateTime(selectedBatch.importedAt)}</b></span>
          <span class="bf-kv">导入行数 <b>{selectedBatch.rowCount} 行</b></span>
          <span class="bf-kv">成功 <b>{selectedBatch.successCount} 行</b></span>
          <span class="bf-kv {selectedBatch.skipCount > 0 ? 'fail' : ''}">失败 <b>{selectedBatch.skipCount} 行</b></span>
          <span class="bf-kv">
            余额不平
            <b class={selectedBatch.mismatchCount > 0 ? 'is-err' : ''}>{selectedBatch.mismatchCount} 笔</b>
          </span>
        {:else}
          <span class="bf-kv">银行流水批次 <b>{batches.length} 个</b></span>
          <span class="bf-kv">合计记录 <b>{stats.total} 行</b></span>
          <span class="bf-kv">
            余额不平
            <b class={allMismatch > 0 ? 'is-err' : ''}>{allMismatch} 笔</b>
          </span>
        {/if}
      </div>
      <div class="bf-chips">
        {#if selectedBatch && batchConfirmed}
          <span class="bf-chip bf-chip-ok">
            <span class="bf-chip-dot"></span>已确认 · {formatDateTime(selectedBatch.confirmedAt)}
          </span>
          {#if selectedBatch.confirmedCount > 0}
            <span class="bf-chip">人工确认 {selectedBatch.confirmedCount} 行（原不平）</span>
          {/if}
        {:else if selectedBatch}
          <span class="bf-chip bf-chip-warn"><span class="bf-chip-dot"></span>未确认</span>
          {#if selectedBatch.mismatchCount > 0}
            <span class="bf-chip bf-chip-err">
              <span class="bf-chip-dot"></span>存在 {selectedBatch.mismatchCount} 行余额不平
            </span>
          {/if}
        {:else}
          <span class="bf-chip">汇总视图（不可确认）</span>
          {#if allMismatch > 0}
            <span class="bf-chip bf-chip-err">
              <span class="bf-chip-dot"></span>存在 {allMismatch} 行余额不平
            </span>
          {/if}
          {#if allConfirmedMismatch > 0}
            <span class="bf-chip">人工确认 {allConfirmedMismatch} 行（原不平）</span>
          {/if}
        {/if}
      </div>
    </div>
    <div class="bf-batch-actions">
      <button
        class="btn btn-primary"
        onclick={askConfirm}
        disabled={busy || !selectedBatch || batchConfirmed}
        title={!selectedBatch ? '请先选择具体批次' : batchConfirmed ? '本批次已确认' : '确认本批次余额连续性'}
      >
        <Icon name="check" size={15} />
        确认本批余额
      </button>
      <button
        class="btn btn-danger"
        onclick={askUnconfirm}
        disabled={busy || !selectedBatch || !batchConfirmed}
        title={!selectedBatch ? '请先选择具体批次' : batchConfirmed ? '撤销本批次确认' : '本批次尚未确认'}
      >
        <Icon name="refresh" size={15} />
        撤销确认
      </button>
    </div>
  </section>

  <!-- ============ 统计卡 ============ -->
  <section class="bf-stats">
    <div class="bf-stat">
      <div class="bf-stat-head"><Icon name="list" size={14} />本批记录数</div>
      <div class="bf-stat-val">{stats.total}<span class="unit">行</span></div>
      <div class="bf-stat-sub">完整展示全部明细 · 首行跳过 {stats.skip} 行</div>
    </div>
    <div class="bf-stat bf-stat-in">
      <div class="bf-stat-head"><Icon name="arrow-up" size={14} />转入合计</div>
      <div class="bf-stat-val">{formatMoney(stats.totalIn)}</div>
      <div class="bf-stat-sub">{stats.inCount} 笔转入</div>
    </div>
    <div class="bf-stat bf-stat-out">
      <div class="bf-stat-head"><Icon name="arrow-down" size={14} />转出合计</div>
      <div class="bf-stat-val">{formatMoney(stats.totalOut)}</div>
      <div class="bf-stat-sub">{stats.outCount} 笔转出</div>
    </div>
    <div class="bf-stat {stats.mismatch > 0 ? 'is-warn' : stats.confirmed > 0 ? 'is-ok' : ''}">
      <div class="bf-stat-head"><Icon name="alert-triangle" size={14} />余额不平笔数</div>
      <div class="bf-stat-val">{stats.mismatch}<span class="unit">笔</span></div>
      <div class="bf-stat-sub">
        {stats.mismatch > 0
          ? '需确认或核对，红底行'
          : stats.confirmed > 0
            ? `已人工确认 ${stats.confirmed} 行`
            : '余额衔接全部正常'}
      </div>
    </div>
  </section>

  <!-- ============ 筛选条 ============ -->
  <section class="bf-filters">
    <div class="bf-field">
      <label for="bf-batch">批次</label>
      <select id="bf-batch" bind:value={filter.batchId} onchange={() => applyBatch(filter.batchId)}>
        <option value="">全部批次（{allTotal} 行）</option>
        {#each batches as b (b.batchId)}
          <option value={b.batchId}>{b.fileName}（{b.successCount} 行）</option>
        {/each}
      </select>
    </div>
    <div class="bf-field">
      <label for="bf-from">交易日期区间</label>
      <div class="bf-daterange">
        <input id="bf-from" type="date" bind:value={fromInput} aria-label="起始日期" />
        <span>至</span>
        <input type="date" bind:value={toInput} aria-label="结束日期" />
      </div>
    </div>
    <div class="bf-field">
      <label for="bf-kw">关键字</label>
      <input
        id="bf-kw"
        type="text"
        placeholder="对方单位 / 摘要 / 附言"
        bind:value={kwInput}
        onkeydown={(e) => {
          if (e.key === 'Enter') void doQuery();
        }}
      />
    </div>
    <button class="btn" onclick={() => void doQuery()} disabled={busy}>
      <Icon name="search" size={15} />
      查询
    </button>
    <button class="btn btn-ghost" onclick={doReset} disabled={busy}>重置</button>
    <div class="bf-filters-right">
      <span class="bf-count-line">共 <b>{stats.total}</b> 条 · 全部展示</span>
    </div>
  </section>

  <!-- ============ 表格 ============ -->
  <section class="bf-table-card">
    <div class="bf-table-toolbar">
      <span class="bf-table-title">流水明细</span>
      <span class="bf-chip">{selectedBatch ? selectedBatch.fileName : '全部批次'}</span>
      <div class="bf-legend">
        <span class="bf-legend-item"><span class="bf-legend-key bf-legend-ok"></span>正常</span>
        <span class="bf-legend-item"><span class="bf-legend-key bf-legend-err"></span>不平</span>
        <span class="bf-legend-item"><span class="bf-legend-key bf-legend-skip"></span>跳过</span>
        <span class="bf-legend-item"><span class="bf-legend-key bf-legend-conf"></span>已确认</span>
      </div>
    </div>

    {#if items.length === 0}
      <div class="bf-empty">
        <div class="bf-empty-ico"><Icon name="file" size={26} /></div>
        <h3>暂无数据</h3>
        <p>暂无数据，请先在导入中心导入银行流水。</p>
        <button class="btn btn-primary" onclick={() => onGoto?.('import')}>
          <Icon name="upload" size={15} />
          前往导入中心
        </button>
      </div>
    {:else}
      <div class="bf-scroll">
        <table class="bf-table">
          <thead>
            <tr>
              <th class="w-9"></th>
              <th>交易时间</th>
              <th>对方账号</th>
              <th>对方单位</th>
              <th>摘要 / 用途</th>
              <th>附言</th>
              <th class="num">转入</th>
              <th class="num">转出</th>
              <th class="num">余额</th>
              <th>校验状态</th>
            </tr>
          </thead>
          <tbody>
            {#each items as f (f.id)}
              {@const st = bankRowState(f)}
              <tr
                data-row-id={f.id}
                class="bf-row bf-row-{st}"
                onclick={() => openDrawer(f)}
              >
                <td class="pr-0">
                  <button
                    class="bf-expand-btn {isExpanded(f.id) ? 'open' : ''}"
                    aria-label="展开余额衔接"
                    onclick={(e) => {
                      e.stopPropagation();
                      toggleExpand(f.id);
                    }}
                  >
                    <Icon name="chevron-right" size={14} />
                  </button>
                </td>
                <td class="bf-time">{f.txnTime}</td>
                <td class="bf-acct">{f.counterpartyAccount || '—'}</td>
                <td>
                  <div class="bf-cell-cp">
                    <span class="bf-cp-name">{f.counterparty}</span>
                    {#if f.isPos}<span class="bf-tag-pos">POS 清算</span>{/if}
                    {#if isChecked(f.id)}<span class="bf-checked-tag">已核对</span>{/if}
                  </div>
                </td>
                <td>{f.summary}</td>
                <td>
                  {#if f.remark}
                    {#if f.isPos}
                      <span class="bf-memo-pos">{f.remark}</span>
                    {:else}
                      {f.remark}
                    {/if}
                  {:else}
                    <span class="bf-memo-empty">—</span>
                  {/if}
                </td>
                <td class="bf-num">{nonzero(f.inAmount) ? formatMoney(f.inAmount) : ''}</td>
                <td class="bf-num">{nonzero(f.outAmount) ? formatMoney(f.outAmount) : ''}</td>
                <td class="bf-num bal">{formatMoney(f.balance)}</td>
                <td>{@render stateBadge(st)}</td>
              </tr>
              {#if isExpanded(f.id)}
                <tr class="bf-detail-row">
                  <td colspan="10">
                    <div class="bf-eq-wrap">
                      <div class="bf-eq-title"><Icon name="list" size={13} />余额衔接校验</div>
                      {#if !f.prevBalance}
                        <div class="bf-eq-line">
                          <span class="bf-eq-verdict skip">本行为批次首行，无上行余额可校验。</span>
                        </div>
                      {:else}
                        <div class="bf-eq-line">
                          <span class="bf-eq-term"><span class="lbl">上行余额</span>{formatMoney(f.prevBalance)}</span>
                          <span class="bf-eq-op">+</span>
                          <span class="bf-eq-term"><span class="lbl">转入</span>{formatMoney(f.inAmount)}</span>
                          <span class="bf-eq-op">−</span>
                          <span class="bf-eq-term"><span class="lbl">转出</span>{formatMoney(f.outAmount)}</span>
                          <span class="bf-eq-eq">=</span>
                          <span class="bf-eq-term"><span class="lbl">应到余额</span>{formatMoney(f.expectedBalance)}</span>
                        </div>
                        {#if st === 'mismatch'}
                          <div class="bf-eq-verdict err">
                            余额衔接断开：本行余额 {formatMoney(f.balance)}，差额 {formatSignedMoney(f.balanceDiff)}
                          </div>
                        {:else if st === 'confirmed'}
                          <div class="bf-eq-verdict ok">
                            人工确认前余额衔接断开：差额 {formatSignedMoney(f.balanceDiff)}，本批次已确认。
                          </div>
                        {:else}
                          <div class="bf-eq-verdict ok">
                            衔接正常：本行余额 {formatMoney(f.balance)} = 应到余额。
                          </div>
                        {/if}
                      {/if}
                      <div class="bf-eq-actions">
                        <button
                          class="btn btn-sm"
                          onclick={(e) => {
                            e.stopPropagation();
                            locateNeighbor(f);
                          }}>定位相邻行</button
                        >
                        <button
                          class="btn btn-sm {isChecked(f.id) ? 'btn-primary' : ''}"
                          onclick={(e) => {
                            e.stopPropagation();
                            toggleChecked(f.id);
                          }}>{isChecked(f.id) ? '已标记核对' : '标记已核对'}</button
                        >
                        <span class="divider"></span>
                        <button
                          class="link-btn"
                          onclick={(e) => {
                            e.stopPropagation();
                            openDrawer(f);
                          }}>查看完整详情 →</button
                        >
                      </div>
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
</section>

<!-- ============ 行详情抽屉 ============ -->
{#if drawerRow}
  {@const f = drawerRow}
  {@const st = bankRowState(f)}
  {@const nb = neighborsOf(f)}
  {@const srcBatch = batches.find((b) => b.batchId === f.batchId) ?? null}
  <div class="bf-drawer-overlay" role="presentation" onclick={closeDrawer}></div>
  <div class="bf-drawer" role="dialog" aria-modal="true" aria-label="行详情" tabindex="-1">
    <div class="bf-drawer-head">
      <Icon name="list" size={17} />
      <h2>行详情</h2>
      <button class="bf-drawer-close" aria-label="关闭" onclick={closeDrawer}>
        <Icon name="x" size={16} />
      </button>
    </div>
    <div class="bf-drawer-body">
      <div class="bf-dsection">
        <h3>核心字段</h3>
        <div class="bf-dlist">
          <div class="bf-drow"><span class="bf-dk">交易时间</span><span class="bf-dv">{f.txnTime}</span></div>
          <div class="bf-drow">
            <span class="bf-dk">对方账号</span>
            <span class="bf-dv font-mono">{f.counterpartyAccount || '—'}</span>
          </div>
          <div class="bf-drow">
            <span class="bf-dk">对方单位</span>
            <span class="bf-dv">
              {f.counterparty}
              {#if f.isPos}<span class="bf-tag-pos">POS 清算</span>{/if}
            </span>
          </div>
          <div class="bf-drow"><span class="bf-dk">摘要 / 用途</span><span class="bf-dv">{f.summary}</span></div>
          <div class="bf-drow">
            <span class="bf-dk">附言</span>
            <span class="bf-dv">
              {#if f.remark}
                {#if f.isPos}<span class="bf-memo-pos">{f.remark}</span>{:else}{f.remark}{/if}
              {:else}
                <span class="bf-dnone">无</span>
              {/if}
            </span>
          </div>
          {#if nonzero(f.inAmount)}
            <div class="bf-drow">
              <span class="bf-dk">转入金额</span>
              <span class="bf-dv amt-in mono-num">+ {formatMoney(f.inAmount)}</span>
            </div>
          {:else}
            <div class="bf-drow">
              <span class="bf-dk">转出金额</span>
              <span class="bf-dv amt-out mono-num">− {formatMoney(f.outAmount)}</span>
            </div>
          {/if}
          <div class="bf-drow">
            <span class="bf-dk">余额（交易后）</span>
            <span class="bf-dv mono-num font-semibold">{formatMoney(f.balance)}</span>
          </div>
          <div class="bf-drow"><span class="bf-dk">校验状态</span><span class="bf-dv">{@render stateBadge(st)}</span></div>
        </div>
      </div>

      <div class="bf-dsection">
        <h3>来源</h3>
        <div class="bf-dlist">
          <div class="bf-drow"><span class="bf-dk">来源文件</span><span class="bf-dv">{f.fileName}</span></div>
          <div class="bf-drow"><span class="bf-dk">批次</span><span class="bf-dv font-mono text-xs">{f.batchId}</span></div>
          <div class="bf-drow"><span class="bf-dk">原始行号</span><span class="bf-dv">第 {f.rowNo} 行</span></div>
          <div class="bf-drow">
            <span class="bf-dk">导入时间</span>
            <span class="bf-dv">{formatDateTime(srcBatch?.importedAt ?? f.createdAt)}</span>
          </div>
        </div>
      </div>

      <div class="bf-dsection">
        <h3>原始 JSON 摘要</h3>
        <pre class="bf-json">{prettyJson(f.rawJson)}</pre>
      </div>

      <div class="bf-dsection">
        <h3>余额衔接上下文</h3>
        <div class="bf-ctx">
          <div class="bf-ctx-row">
            <span class="bf-ctx-mark">上一行</span>
            <span class="bf-ctx-desc">{nb.prev ? `${nb.prev.summary} · ${nb.prev.counterparty}` : '无'}</span>
            <span class="bf-ctx-bal">{nb.prev ? formatMoney(nb.prev.balance) : '—'}</span>
          </div>
          <div class="bf-ctx-row current">
            <span class="bf-ctx-mark">当前行</span>
            <span class="bf-ctx-desc">{f.summary} · {f.counterparty}</span>
            <span class="bf-ctx-bal">{formatMoney(f.balance)}</span>
          </div>
          <div class="bf-ctx-row">
            <span class="bf-ctx-mark">下一行</span>
            <span class="bf-ctx-desc">{nb.next ? `${nb.next.summary} · ${nb.next.counterparty}` : '无'}</span>
            <span class="bf-ctx-bal">{nb.next ? formatMoney(nb.next.balance) : '—'}</span>
          </div>
        </div>
        {#if !f.prevBalance}
          <div class="bf-eq-verdict skip mt-2">首行无上行余额。</div>
        {:else if f.balanceDiff && f.balanceDiff !== '0.00'}
          <div class="bf-eq-verdict err mt-2">差额 {formatSignedMoney(f.balanceDiff)}</div>
        {:else}
          <div class="bf-eq-verdict ok mt-2">衔接正常</div>
        {/if}
      </div>

      <div class="bf-dsection">
        <h3>关联凭证</h3>
        {#if f.voucherNo}
          <button class="bf-voucher-link" onclick={() => onGoto?.('voucher')}>
            <Icon name="file" size={15} />
            {f.voucherNo}
          </button>
        {:else}
          <div class="bf-dnone">该流水行未关联凭证。</div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<!-- ============ 确认框 ============ -->
<Confirm
  open={dialogMode !== ''}
  title={dialogMode === 'confirm' ? '确认本批次余额' : '撤销批次余额确认'}
  confirmLabel={dialogMode === 'confirm' ? '确认本批余额' : '撤销确认'}
  variant={dialogMode === 'confirm' ? 'info' : 'danger'}
  message={dialogMessage}
  onconfirm={doDialogConfirm}
  oncancel={() => (dialogMode = '')}
/>

<!-- ============ Toast ============ -->
<div class="bf-toasts">
  {#each toasts as t (t.id)}
    <div class="bf-toast {t.tone}">
      <span class="bf-toast-bar"></span>
      <div class="bf-toast-body">{t.text}</div>
      <button class="bf-toast-close" aria-label="关闭" onclick={() => dismissToast(t.id)}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/each}
</div>
