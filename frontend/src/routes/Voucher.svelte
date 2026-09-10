<!-- 凭证管理（F6）：列表/详情/审核/反审核。借贷不等禁止保存。
TODO: 接 VoucherService.List / Get / Create / Audit / UnAudit（bindings 生成后）。 -->
<script lang="ts">
  import type { Voucher } from '../lib/types';
  import { voucherStatusText } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  let items = $state<Voucher[]>([]);
  let auditOpen = $state(false);
  let pendingId = $state('');

  function askAudit(id: string): void {
    pendingId = id;
    auditOpen = true;
  }
  function doAudit(): void {
    auditOpen = false;
    pendingId = '';
    // TODO: VoucherService.Audit(pendingId)
  }
</script>

<section class="page">
  <h2>凭证管理</h2>
  <div class="toolbar">
    <button class="btn" disabled title="bindings 生成后启用">查询</button>
  </div>
  <table class="dense">
    <thead><tr><th>凭证号</th><th>日期</th><th>摘要</th><th>状态</th><th>操作</th></tr></thead>
    <tbody>
      {#each items as v}
        <tr>
          <td>{v.voucherNo}</td><td>{v.voucherDate}</td><td>{v.summaryText}</td>
          <td>{voucherStatusText(v.status)}</td>
          <td>{#if v.status === 'draft'}<button class="btn btn-small" onclick={() => askAudit(v.id)}>审核</button>{/if}</td>
        </tr>
      {:else}
        <tr><td colspan="5" class="empty">暂无凭证，请先审核汇总后生成</td></tr>
      {/each}
    </tbody>
  </table>
  <Confirm open={auditOpen} message="审核该凭证吗？审核后不可直接修改，只能反审核。" onconfirm={doAudit} oncancel={() => (auditOpen = false)} />
</section>
