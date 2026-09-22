<!-- 银行流水（F2）：三色余额状态 + 批次确认/撤销（需确认框）。
     数据经 BankFlowService.List / ConfirmBatch / UnconfirmBatch（Wails 绑定）。
     原始表只读：本页无任何编辑入口；确认仅写 balance_confirmed_at，可撤销。 -->
<script lang="ts">
  import { BankFlowService, ImportService } from '../../bindings/finlite/internal/services';
  import type { BankFlow, BankFlowFilter, ImportResult } from '../../bindings/finlite/internal/services/models';
  import { formatMoney, statusText, statusClass } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  const PAGE_SIZE = 50;

  let flows = $state<BankFlow[]>([]);
  let total = $state(0);
  let page = $state(1);
  let batches = $state<ImportResult[]>([]);

  let batchId = $state('');
  let dateFrom = $state('');
  let dateTo = $state('');
  let keyword = $state('');

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  // 确认对话框模式：confirm=确认本批余额 / unconfirm=撤销确认
  let dialogMode = $state<'' | 'confirm' | 'unconfirm'>('');

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  function filter(): BankFlowFilter {
    return {
      batchId,
      dateFrom,
      dateTo,
      keyword: keyword.trim(),
      page,
      pageSize: PAGE_SIZE,
    };
  }

  async function refresh(): Promise<void> {
    busy = true;
    try {
      const res = await BankFlowService.List(filter());
      flows = res.items ?? [];
      total = res.total;
      page = res.page;
      notice = '';
    } catch (e) {
      flows = [];
      total = 0;
      notice = `加载银行流水失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function loadBatches(): Promise<void> {
    try {
      batches = (await ImportService.ListBatches('bank_flow')) ?? [];
    } catch {
      batches = [];
    }
  }

  function doSearch(): void {
    page = 1;
    refresh();
  }

  function goPage(p: number): void {
    if (p < 1 || p > pageCount || p === page) return;
    page = p;
    refresh();
  }

  /** 批次确认/撤销：仅当筛选了具体批次时可用（确认按 batch_id 全量生效）。 */
  function askConfirm(): void {
    if (!batchId) return;
    dialogMode = 'confirm';
  }
  function askUnconfirm(): void {
    if (!batchId) return;
    dialogMode = 'unconfirm';
  }

  async function doDialogConfirm(): Promise<void> {
    const mode = dialogMode;
    dialogMode = '';
    if (!mode || !batchId) return;
    busy = true;
    try {
      if (mode === 'confirm') {
        const n = await BankFlowService.ConfirmBatch(batchId, 'local');
        notice = `已确认批次余额（${n} 行红底消除），可撤销。`;
        noticeClass = 'text-ok';
      } else {
        const n = await BankFlowService.UnconfirmBatch(batchId);
        notice = `已撤销批次余额确认（${n} 行），不平行恢复红底。`;
        noticeClass = 'text-warn';
      }
      await refresh();
    } catch (e) {
      notice = `${mode === 'confirm' ? '确认' : '撤销确认'}失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  const dialogMessage = $derived(
    dialogMode === 'confirm'
      ? '确认本批次余额连续性吗？确认后不平行的红底将消除，之后可撤销。'
      : '撤销本批次的余额确认吗？不平行的行将恢复红底显示。'
  );

  refresh();
  loadBatches();
</script>

<section>
  <div class="toolbar">
    <select class="input" bind:value={batchId} onchange={doSearch}>
      <option value="">全部批次</option>
      {#each batches as b (b.batchId)}
        <option value={b.batchId}>{b.fileName}（{b.successCount} 行）</option>
      {/each}
    </select>
    <input class="input" type="date" bind:value={dateFrom} onchange={doSearch} />
    <span class="text-sm text-muted">至</span>
    <input class="input" type="date" bind:value={dateTo} onchange={doSearch} />
    <input
      class="input"
      placeholder="对方/摘要/附言关键字"
      bind:value={keyword}
      onkeydown={(e) => { if (e.key === 'Enter') doSearch(); }}
    />
    <button class="btn" onclick={doSearch} disabled={busy}>查询</button>
    <button class="btn" onclick={askConfirm} disabled={busy || !batchId}>确认本批余额</button>
    <button class="btn btn-danger" onclick={askUnconfirm} disabled={busy || !batchId}>撤销确认</button>
    <span class="text-sm text-muted">共 {total} 条 · 第 {page} / {pageCount} 页</span>
  </div>
  {#if !batchId}
    <p class="mb-2 text-sm text-muted">提示：余额确认 / 撤销按批次生效，请先在左侧选择具体批次。</p>
  {/if}
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>交易时间</th><th>对方单位</th><th>摘要</th><th>附言</th>
        <th class="num">转入</th><th class="num">转出</th><th class="num">余额</th><th>校验</th>
      </tr>
    </thead>
    <tbody>
      {#each flows as f (f.id)}
        <tr class={f.displayStatus === 'mismatch' ? 'row-mismatch' : ''}>
          <td class="whitespace-nowrap">{f.txnTime}</td>
          <td>{f.counterparty}</td>
          <td>{f.summary}</td>
          <td>{f.remark}</td>
          <td class="num">{f.inAmount === '0' ? '' : formatMoney(f.inAmount)}</td>
          <td class="num">{f.outAmount === '0' ? '' : formatMoney(f.outAmount)}</td>
          <td class="num">{formatMoney(f.balance)}</td>
          <td><span class={statusClass(f.displayStatus)}>{statusText(f.displayStatus)}</span></td>
        </tr>
      {:else}
        <tr><td colspan="8" class="empty">暂无数据，请先在导入中心导入银行流水</td></tr>
      {/each}
    </tbody>
  </table>
  <div class="mt-2 flex items-center gap-2">
    <button class="btn btn-sm" onclick={() => goPage(page - 1)} disabled={busy || page <= 1}>上一页</button>
    <button class="btn btn-sm" onclick={() => goPage(page + 1)} disabled={busy || page >= pageCount}>下一页</button>
  </div>
  <Confirm
    open={dialogMode !== ''}
    message={dialogMessage}
    onconfirm={doDialogConfirm}
    oncancel={() => (dialogMode = '')}
  />
</section>