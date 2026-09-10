<!-- 对账中心（F4）：按结算日三方勾稽，差异进待处理，不阻塞其他日期。
TODO: 接 ReconService.ReconcileByDate / ListDiffs（bindings 生成后）。 -->
<script lang="ts">
  import type { ReconResult, ReconDiff } from '../lib/types';
  import { formatMoney } from '../lib/format';

  let date = $state('');
  let result = $state<ReconResult | null>(null);
  let diffs = $state<ReconDiff[]>([]);
</script>

<section>
  <h2 class="page-title">对账中心</h2>
  <div class="toolbar">
    <input class="input" type="date" bind:value={date} />
    <button class="btn" disabled title="bindings 生成后启用">对账</button>
  </div>
  {#if result}
    <div class="my-3 flex flex-wrap gap-x-6 gap-y-2 text-sm">
      <div>订单实收 <b>{formatMoney(result.orderNet)}</b></div>
      <div>银行POS转入 <b>{formatMoney(result.bankPOSIn)}</b></div>
      <div>汇总实际收入 <b>{formatMoney(result.summaryActual)}</b></div>
      <div>{result.matched ? '三方一致' : `差异 ${formatMoney(result.diff)}`}</div>
    </div>
  {/if}
  <h3 class="section-title">待处理差异</h3>
  <table class="data-table">
    <thead><tr><th>日期</th><th>字段</th><th class="num">预期</th><th class="num">实际</th><th>说明</th></tr></thead>
    <tbody>
      {#each diffs as d}
        <tr><td>{d.date}</td><td>{d.field}</td><td class="num">{formatMoney(d.expect)}</td><td class="num">{formatMoney(d.actual)}</td><td>{d.detail}</td></tr>
      {:else}
        <tr><td colspan="5" class="empty">暂无差异</td></tr>
      {/each}
    </tbody>
  </table>
</section>
