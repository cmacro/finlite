<!-- 科目余额表（F7）：期末 = 期初 + 借 - 贷。
TODO: 接 ReportService.BalanceSheet / DailyIncome（bindings 生成后）。 -->
<script lang="ts">
  import type { BalanceRow } from '../lib/types';
  import { formatMoney } from '../lib/format';

  let rows = $state<BalanceRow[]>([]);
  let dateFrom = $state('');
  let dateTo = $state('');
</script>

<section class="page">
  <h2>科目余额表</h2>
  <div class="toolbar">
    <input class="input" type="date" bind:value={dateFrom} />
    <span>至</span>
    <input class="input" type="date" bind:value={dateTo} />
    <button class="btn" disabled title="bindings 生成后启用">查询</button>
  </div>
  <table class="dense">
    <thead><tr><th>科目代码</th><th>科目名称</th><th class="num">期初</th><th class="num">本期借方</th><th class="num">本期贷方</th><th class="num">期末</th></tr></thead>
    <tbody>
      {#each rows as r}
        <tr><td>{r.accountCode}</td><td>{r.accountName}</td><td class="num">{formatMoney(r.begin)}</td><td class="num">{formatMoney(r.debit)}</td><td class="num">{formatMoney(r.credit)}</td><td class="num">{formatMoney(r.end)}</td></tr>
      {:else}
        <tr><td colspan="6" class="empty">暂无数据</td></tr>
      {/each}
    </tbody>
  </table>
</section>
