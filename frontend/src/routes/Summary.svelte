<!-- 数据汇总（F5）：CRUD + 从对账预填 + 审核/驳回；未审核不可生成凭证。
     数据经 SummaryService.List / Save / Audit / GenerateFromRecon（Wails 绑定）。 -->
<script lang="ts">
  import { SummaryService } from '../../bindings/finlite/internal/services';
  import type { Summary, SaveSummaryReq } from '../../bindings/finlite/internal/services/models';
  import { formatMoney, summaryStatusText } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  const PAGE_SIZE = 50;
  const OPERATOR = 'local';

  let items = $state<Summary[]>([]);
  let total = $state(0);
  let page = $state(1);
  let dateFrom = $state('');
  let dateTo = $state('');
  let statusFilter = $state('');

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  // 审核 / 驳回确认框
  let auditOpen = $state(false);
  let pendingAudit = $state<{ id: string; approve: boolean } | null>(null);

  // 预填确认框
  let prefillOpen = $state(false);
  let prefillDate = $state('');

  // 编辑抽屉
  let editing = $state<SaveSummaryReq | null>(null);
  let saveOpen = $state(false);

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  function blankForm(): SaveSummaryReq {
    return {
      id: '', date: '', receiptNo: '', category: '收入', project: '营业收入', reason: '',
      incomeAmount: '0.00', expenseAmount: '0.00', feeActual: '0.00', balance: '0.00',
      remark: '', operator: OPERATOR,
    };
  }

  async function refresh(): Promise<void> {
    busy = true;
    try {
      const res = await SummaryService.List({ dateFrom, dateTo, status: statusFilter, page, pageSize: PAGE_SIZE });
      items = res.items ?? [];
      total = res.total;
      page = res.page;
    } catch (e) {
      items = [];
      total = 0;
      notice = `加载汇总失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
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

  function askAudit(s: Summary, approve: boolean): void {
    pendingAudit = { id: s.id, approve };
    auditOpen = true;
  }

  async function doAudit(): Promise<void> {
    const req = pendingAudit;
    auditOpen = false;
    pendingAudit = null;
    if (!req) return;
    busy = true;
    try {
      await SummaryService.Audit(req.id, req.approve, OPERATOR, '');
      notice = req.approve ? '已审核通过，可生成凭证。' : '已驳回为草稿。';
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `${req.approve ? '审核' : '驳回'}失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  function askPrefill(): void {
    if (!prefillDate) {
      notice = '请先选择要预填的对账日期';
      noticeClass = 'text-warn';
      return;
    }
    prefillOpen = true;
  }

  async function doPrefill(): Promise<void> {
    prefillOpen = false;
    busy = true;
    try {
      await SummaryService.GenerateFromRecon(prefillDate, OPERATOR);
      notice = `已按 ${prefillDate} 对账结果预填草稿，请补录事由/备注后审核。`;
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `预填失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  function openNew(): void {
    editing = blankForm();
    saveOpen = true;
  }

  function openEdit(s: Summary): void {
    editing = {
      id: s.id, date: s.date, receiptNo: s.receiptNo, category: s.category, project: s.project,
      reason: s.reason, incomeAmount: s.incomeAmount, expenseAmount: s.expenseAmount,
      feeActual: s.feeActual, balance: s.balance, remark: s.remark, operator: OPERATOR,
    };
    saveOpen = true;
  }

  async function doSave(): Promise<void> {
    if (!editing) return;
    busy = true;
    try {
      await SummaryService.Save(editing);
      saveOpen = false;
      editing = null;
      notice = '已保存。';
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `保存失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  const auditMessage = $derived(
    pendingAudit?.approve ? '审核通过该汇总吗？审核后方可生成凭证。' : '驳回该汇总为草稿吗？'
  );

  // 顶栏刷新按钮（App.svelte 广播）：重拉汇总列表。
  $effect(() => {
    const handler = () => void refresh();
    window.addEventListener('finlite:refresh', handler);
    return () => window.removeEventListener('finlite:refresh', handler);
  });

  refresh();
</script>

<section>
  <div class="toolbar">
    <input class="input" type="date" bind:value={dateFrom} onchange={doSearch} />
    <span class="text-sm text-muted">至</span>
    <input class="input" type="date" bind:value={dateTo} onchange={doSearch} />
    <select class="input" bind:value={statusFilter} onchange={doSearch}>
      <option value="">全部状态</option>
      <option value="draft">草稿</option>
      <option value="reviewed">已审核</option>
      <option value="vouchered">已生成凭证</option>
    </select>
    <button class="btn" onclick={doSearch} disabled={busy}>查询</button>
    <input class="input" type="date" bind:value={prefillDate} />
    <button class="btn" onclick={askPrefill} disabled={busy}>从对账预填</button>
    <button class="btn" onclick={openNew} disabled={busy}>新增汇总</button>
    <span class="text-sm text-muted">共 {total} 条 · 第 {page} / {pageCount} 页</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>日期</th><th>收据号</th><th>一级</th><th>项目</th><th>事由</th>
        <th class="num">实际收入</th><th class="num">支出</th><th class="num">手续费</th>
        <th>状态</th><th>操作</th>
      </tr>
    </thead>
    <tbody>
      {#each items as s (s.id)}
        <tr>
          <td class="whitespace-nowrap">{s.date}</td>
          <td>{s.receiptNo}</td>
          <td>{s.category}</td>
          <td>{s.project}</td>
          <td>{s.reason}</td>
          <td class="num">{formatMoney(s.incomeAmount)}</td>
          <td class="num">{formatMoney(s.expenseAmount)}</td>
          <td class="num">
            {formatMoney(s.feeActual)}{#if s.feeManualAdjusted}<span class="ml-1 font-semibold text-warn">费</span>{/if}
          </td>
          <td>{summaryStatusText(s.status)}</td>
          <td class="whitespace-nowrap">
            {#if s.status === 'draft'}
              <button class="btn btn-sm" onclick={() => askAudit(s, true)} disabled={busy}>审核</button>
              <button class="btn btn-sm" onclick={() => openEdit(s)} disabled={busy}>编辑</button>
            {:else if s.status === 'reviewed'}
              <button class="btn btn-sm btn-danger" onclick={() => askAudit(s, false)} disabled={busy}>驳回</button>
            {:else}
              <span class="text-sm text-muted">已制单</span>
            {/if}
          </td>
        </tr>
      {:else}
        <tr><td colspan="10" class="empty">暂无汇总，请从对账预填或手工新增</td></tr>
      {/each}
    </tbody>
  </table>
  <div class="mt-2 flex items-center gap-2">
    <button class="btn btn-sm" onclick={() => goPage(page - 1)} disabled={busy || page <= 1}>上一页</button>
    <button class="btn btn-sm" onclick={() => goPage(page + 1)} disabled={busy || page >= pageCount}>下一页</button>
  </div>

  <Confirm open={auditOpen} message={auditMessage} onconfirm={doAudit} oncancel={() => (auditOpen = false)} />
  <Confirm
    open={prefillOpen}
    message={`按 ${prefillDate} 的三方勾稽结果生成汇总草稿吗？事由/备注需人工补录。`}
    onconfirm={doPrefill}
    oncancel={() => (prefillOpen = false)}
  />

  {#if saveOpen && editing}
    <div class="modal-overlay" role="presentation" onclick={() => (saveOpen = false)}>
      <div
        class="modal-box modal-box-wide"
        role="dialog"
        aria-modal="true"
        aria-label="汇总编辑"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') saveOpen = false; }}
        tabindex="-1"
      >
        <h3 class="mb-3 text-base font-semibold">{editing.id ? '编辑汇总' : '新增汇总'}</h3>
        <div class="grid grid-cols-2 gap-2 text-sm">
          <label class="flex flex-col gap-1">日期
            <input class="input" type="date" bind:value={editing.date} />
          </label>
          <label class="flex flex-col gap-1">收据号
            <input class="input" bind:value={editing.receiptNo} placeholder="收入行填写，如 0000502" />
          </label>
          <label class="flex flex-col gap-1">一级
            <select class="input" bind:value={editing.category}>
              <option value="收入">收入</option>
              <option value="支出">支出</option>
            </select>
          </label>
          <label class="flex flex-col gap-1">项目
            <input class="input" bind:value={editing.project} placeholder="营业收入 / 产品成本 / 护理成本 / 手续费 / 水电费" />
          </label>
          <label class="flex flex-col gap-1 col-span-2">事由
            <input class="input" bind:value={editing.reason} placeholder="如 王浩然产康充值" />
          </label>
          <label class="flex flex-col gap-1 col-span-2">备注
            <input class="input" bind:value={editing.remark} placeholder="对方单位 / 清算户等" />
          </label>
          <label class="flex flex-col gap-1">实际收入
            <input class="input num-input" bind:value={editing.incomeAmount} />
          </label>
          <label class="flex flex-col gap-1">支出
            <input class="input num-input" bind:value={editing.expenseAmount} />
          </label>
          <label class="flex flex-col gap-1">手续费
            <input class="input num-input" bind:value={editing.feeActual} />
          </label>
          <label class="flex flex-col gap-1">余额
            <input class="input num-input" bind:value={editing.balance} />
          </label>
        </div>
        <div class="modal-actions mt-4">
          <button class="btn" onclick={() => (saveOpen = false)}>取消</button>
          <button class="btn" onclick={doSave} disabled={busy}>保存</button>
        </div>
      </div>
    </div>
  {/if}
</section>
