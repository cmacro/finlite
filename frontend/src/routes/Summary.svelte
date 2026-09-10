<!-- 数据汇总（F5）：CRUD + 预填 + 审核/驳回。未审核不可生成凭证。
TODO: 接 SummaryService.List / Save / Audit / GenerateFromRecon（bindings 生成后）。 -->
<script lang="ts">
  import type { Summary } from '../lib/types';
  import { formatMoney, summaryStatusText } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  let items = $state<Summary[]>([]);
  let auditOpen = $state(false);
  let pendingId = $state('');

  function askAudit(id: string): void {
    pendingId = id;
    auditOpen = true;
  }
  function doAudit(): void {
    auditOpen = false;
    pendingId = '';
    // TODO: SummaryService.Audit(pendingId, true)
  }
</script>

<section class="page">
  <h2>数据汇总</h2>
  <div class="toolbar">
    <button class="btn" disabled title="bindings 生成后启用">从对账预填</button>
    <button class="btn" disabled title="bindings 生成后启用">新增汇总</button>
  </div>
  <table class="dense">
    <thead>
      <tr><th>日期</th><th>收据号</th><th>一级</th><th>项目</th><th>事由</th><th class="num">实际收入</th><th class="num">支出</th><th class="num">手续费</th><th>状态</th><th>操作</th></tr>
    </thead>
    <tbody>
      {#each items as s}
        <tr>
          <td>{s.date}</td><td>{s.receiptNo}</td><td>{s.category}</td><td>{s.project}</td><td>{s.reason}</td>
          <td class="num">{formatMoney(s.incomeAmount)}</td><td class="num">{formatMoney(s.expenseAmount)}</td>
          <td class="num">{formatMoney(s.feeActual)}{#if s.feeManualAdjusted}<span class="fee-flag">费</span>{/if}</td>
          <td>{summaryStatusText(s.status)}</td>
          <td>{#if s.status === 'draft'}<button class="btn btn-small" onclick={() => askAudit(s.id)}>审核</button>{/if}</td>
        </tr>
      {:else}
        <tr><td colspan="10" class="empty">暂无汇总，请从对账预填或手工新增</td></tr>
      {/each}
    </tbody>
  </table>
  <Confirm open={auditOpen} message="审核通过该汇总吗？审核后方可生成凭证。" onconfirm={doAudit} oncancel={() => (auditOpen = false)} />
</section>
