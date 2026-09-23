<!-- 凭证管理（F6）：列表/详情/按 §6.5 模板由已审核汇总一键生成、审核/反审核、打印预览。
     数据经 VoucherService.List / Get / BuildEntries / Create / Audit / UnAudit（Wails 绑定）。 -->
<script lang="ts">
  import { VoucherService, SummaryService } from '../../bindings/finlite/internal/services';
  import type { Voucher, VoucherEntry, Summary } from '../../bindings/finlite/internal/services/models';
  import { formatMoney, voucherStatusText, summaryStatusText } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  const PAGE_SIZE = 50;
  const OPERATOR = 'local';

  let items = $state<Voucher[]>([]);
  let total = $state(0);
  let page = $state(1);
  let dateFrom = $state('');
  let dateTo = $state('');
  let statusFilter = $state('');

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  // 待生成凭证的已审核汇总
  let reviewed = $state<Summary[]>([]);

  // 详情 + 打印预览
  let detail = $state<Voucher | null>(null);
  let detailOpen = $state(false);

  // 审核 / 反审核确认
  let auditOpen = $state(false);
  let pendingAudit = $state<{ id: string; audit: boolean } | null>(null);

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));
  let debitTotal = $derived((detail?.entries ?? []).reduce((a, e) => a + Number(e.debit || 0), 0));
  let creditTotal = $derived((detail?.entries ?? []).reduce((a, e) => a + Number(e.credit || 0), 0));

  async function refresh(): Promise<void> {
    busy = true;
    try {
      const res = await VoucherService.List({ dateFrom, dateTo, status: statusFilter, page, pageSize: PAGE_SIZE });
      items = res.items ?? [];
      total = res.total;
      page = res.page;
    } catch (e) {
      items = [];
      total = 0;
      notice = `加载凭证失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function loadReviewed(): Promise<void> {
    try {
      const res = await SummaryService.List({ dateFrom: '', dateTo: '', status: 'reviewed', page: 1, pageSize: 100 });
      reviewed = res.items ?? [];
    } catch {
      reviewed = [];
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

  /** 一键生成：走 §6.5 模板生成分录，后端仍校验借贷平衡。 */
  async function generate(s: Summary): Promise<void> {
    busy = true;
    try {
      const entries: VoucherEntry[] = (await VoucherService.BuildEntries(s.id)) ?? [];
      if (entries.length === 0) {
        throw new Error('模板未生成任何分录，请检查该汇总的一级/项目是否匹配 §6.5 五类模板');
      }
      const v = await VoucherService.Create({ summaryId: s.id, entries, operator: OPERATOR });
      notice = `已生成凭证 ${v.voucherNo}（借贷平衡校验通过）。`;
      noticeClass = 'text-ok';
      await refresh();
      await loadReviewed();
    } catch (e) {
      notice = `生成凭证失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function openDetail(v: Voucher): Promise<void> {
    busy = true;
    try {
      detail = await VoucherService.Get(v.id);
      detailOpen = true;
    } catch (e) {
      notice = `读取凭证失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  function askAudit(v: Voucher, audit: boolean): void {
    pendingAudit = { id: v.id, audit };
    auditOpen = true;
  }

  async function doAudit(): Promise<void> {
    const req = pendingAudit;
    auditOpen = false;
    pendingAudit = null;
    if (!req) return;
    busy = true;
    try {
      if (req.audit) {
        await VoucherService.Audit(req.id, OPERATOR);
        notice = '已审核，凭证已登账（进入科目余额表）。';
        noticeClass = 'text-ok';
      } else {
        await VoucherService.UnAudit(req.id, OPERATOR);
        notice = '已反审核，回到草稿（留审计日志）。';
        noticeClass = 'text-warn';
      }
      await refresh();
    } catch (e) {
      notice = `${req.audit ? '审核' : '反审核'}失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  /** 打印预览：新窗口渲染凭证，浏览器打印即出纸质凭证。 */
  function printDetail(): void {
    const v = detail;
    if (!v) return;
    const rows = (v.entries ?? [])
      .map(
        (e) =>
          `<tr><td>${e.accountCode} ${e.accountName}</td><td class="num">${e.debit === '0' ? '' : formatMoney(e.debit)}</td>` +
          `<td class="num">${e.credit === '0' ? '' : formatMoney(e.credit)}</td><td>${e.remark ?? ''}</td></tr>`
      )
      .join('');
    const html = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8" />
<title>凭证 ${v.voucherNo}</title>
<style>
  body { font-family: system-ui, sans-serif; font-size: 13px; color: #222; margin: 24px; }
  h1 { font-size: 17px; margin: 0 0 4px; }
  .meta { color: #555; margin-bottom: 12px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { border: 1px solid #999; padding: 4px 6px; text-align: left; }
  .num { text-align: right; font-variant-numeric: tabular-nums; }
  tfoot td { font-weight: 600; }
</style></head><body>
<h1>记账凭证</h1>
<div class="meta">凭证号 ${v.voucherNo} ｜ 日期 ${v.voucherDate} ｜ 摘要 ${v.summaryText} ｜ 状态 ${voucherStatusText(v.status)}</div>
<table><thead><tr><th>科目</th><th class="num">借方</th><th class="num">贷方</th><th>备注</th></tr></thead>
<tbody>${rows}</tbody>
<tfoot><tr><td>合计</td><td class="num">${formatMoney(debitTotal)}</td><td class="num">${formatMoney(creditTotal)}</td><td></td></tr></tfoot>
</table>
</body></html>`;
    const w = window.open('', '_blank', 'width=900,height=700');
    if (!w) {
      notice = '打印预览被浏览器拦截，请允许弹出窗口后重试';
      noticeClass = 'text-warn';
      return;
    }
    w.document.write(html);
    w.document.close();
    w.focus();
    w.print();
  }

  const auditMessage = $derived(
    pendingAudit?.audit
      ? '审核该凭证吗？审核后不可直接修改，只能反审核。'
      : '反审核该凭证吗？将回到草稿并留审计日志。'
  );

  refresh();
  loadReviewed();
</script>

<section>
  <h3 class="section-title">待制单（已审核汇总）</h3>
  <table class="data-table">
    <thead>
      <tr>
        <th>日期</th><th>收据号</th><th>项目</th><th>事由</th><th class="num">实际收入</th>
        <th class="num">支出</th><th class="num">手续费</th><th>操作</th>
      </tr>
    </thead>
    <tbody>
      {#each reviewed as s (s.id)}
        <tr>
          <td class="whitespace-nowrap">{s.date}</td>
          <td>{s.receiptNo}</td>
          <td>{s.project}</td>
          <td>{s.reason}</td>
          <td class="num">{formatMoney(s.incomeAmount)}</td>
          <td class="num">{formatMoney(s.expenseAmount)}</td>
          <td class="num">{formatMoney(s.feeActual)}</td>
          <td><button class="btn btn-sm" onclick={() => generate(s)} disabled={busy}>生成凭证</button></td>
        </tr>
      {:else}
        <tr><td colspan="8" class="empty">暂无已审核汇总，请先在「数据汇总」页审核</td></tr>
      {/each}
    </tbody>
  </table>

  <h3 class="section-title">凭证列表</h3>
  <div class="toolbar">
    <input class="input" type="date" bind:value={dateFrom} onchange={doSearch} />
    <span class="text-sm text-muted">至</span>
    <input class="input" type="date" bind:value={dateTo} onchange={doSearch} />
    <select class="input" bind:value={statusFilter} onchange={doSearch}>
      <option value="">全部状态</option>
      <option value="draft">草稿</option>
      <option value="audited">已审核</option>
    </select>
    <button class="btn" onclick={doSearch} disabled={busy}>查询</button>
    <span class="text-sm text-muted">共 {total} 条 · 第 {page} / {pageCount} 页</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead><tr><th>凭证号</th><th>日期</th><th>摘要</th><th>状态</th><th>操作</th></tr></thead>
    <tbody>
      {#each items as v (v.id)}
        <tr>
          <td class="whitespace-nowrap">{v.voucherNo}</td>
          <td class="whitespace-nowrap">{v.voucherDate}</td>
          <td>{v.summaryText}</td>
          <td>{voucherStatusText(v.status)}</td>
          <td class="whitespace-nowrap">
            <button class="btn btn-sm" onclick={() => openDetail(v)} disabled={busy}>详情/打印</button>
            {#if v.status === 'draft'}
              <button class="btn btn-sm" onclick={() => askAudit(v, true)} disabled={busy}>审核</button>
            {:else}
              <button class="btn btn-sm btn-danger" onclick={() => askAudit(v, false)} disabled={busy}>反审核</button>
            {/if}
          </td>
        </tr>
      {:else}
        <tr><td colspan="5" class="empty">暂无凭证，请先审核汇总后生成</td></tr>
      {/each}
    </tbody>
  </table>
  <div class="mt-2 flex items-center gap-2">
    <button class="btn btn-sm" onclick={() => goPage(page - 1)} disabled={busy || page <= 1}>上一页</button>
    <button class="btn btn-sm" onclick={() => goPage(page + 1)} disabled={busy || page >= pageCount}>下一页</button>
  </div>

  <Confirm open={auditOpen} message={auditMessage} onconfirm={doAudit} oncancel={() => (auditOpen = false)} />

  {#if detailOpen && detail}
    <div class="modal-overlay" role="presentation" onclick={() => (detailOpen = false)}>
      <div
        class="modal-box modal-box-wide"
        role="dialog"
        aria-modal="true"
        aria-label="凭证详情"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') detailOpen = false; }}
        tabindex="-1"
      >
        <h3 class="mb-1 text-base font-semibold">记账凭证 {detail.voucherNo}</h3>
        <p class="mb-3 text-sm text-muted">
          日期 {detail.voucherDate} ｜ 摘要 {detail.summaryText} ｜ 状态 {voucherStatusText(detail.status)}
        </p>
        <table class="data-table">
          <thead>
            <tr><th>科目代码</th><th>科目名称</th><th class="num">借方</th><th class="num">贷方</th><th>备注</th></tr>
          </thead>
          <tbody>
            {#each detail.entries ?? [] as e (e.id)}
              <tr>
                <td>{e.accountCode}</td>
                <td>{e.accountName}</td>
                <td class="num">{e.debit === '0' ? '' : formatMoney(e.debit)}</td>
                <td class="num">{e.credit === '0' ? '' : formatMoney(e.credit)}</td>
                <td>{e.remark}</td>
              </tr>
            {/each}
          </tbody>
          <tfoot>
            <tr>
              <td colspan="2" class="font-semibold">合计</td>
              <td class="num font-semibold">{formatMoney(debitTotal)}</td>
              <td class="num font-semibold">{formatMoney(creditTotal)}</td>
              <td></td>
            </tr>
          </tfoot>
        </table>
        <div class="modal-actions mt-4">
          <button class="btn" onclick={() => (detailOpen = false)}>关闭</button>
          <button class="btn" onclick={printDetail}>打印预览</button>
        </div>
      </div>
    </div>
  {/if}
</section>
