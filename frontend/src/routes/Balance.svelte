<!-- 科目余额表（F7）：期初 + 本期借贷 = 期末，按期间筛；期末按科目方向计算。
     数据经 ReportService.BalanceSheet / DailyIncome（Wails 绑定）。 -->
<script lang="ts">
  import { ReportService } from '../../bindings/finlite/internal/services';
  import type { BalanceRow, DailyRow } from '../../bindings/finlite/internal/services/models';
  import { formatMoney } from '../lib/format';

  let rows = $state<BalanceRow[]>([]);
  let daily = $state<DailyRow[]>([]);
  let dateFrom = $state('');
  let dateTo = $state('');

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  async function refresh(): Promise<void> {
    busy = true;
    try {
      rows = (await ReportService.BalanceSheet(dateFrom, dateTo)) ?? [];
      daily = (await ReportService.DailyIncome(dateFrom, dateTo)) ?? [];
      notice = '';
    } catch (e) {
      rows = [];
      daily = [];
      notice = `加载报表失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  // 顶栏刷新按钮（App.svelte 广播）：重拉余额表与收支日报。
  $effect(() => {
    const handler = () => void refresh();
    window.addEventListener('finlite:refresh', handler);
    return () => window.removeEventListener('finlite:refresh', handler);
  });

  refresh();
</script>

<section>
  <div class="toolbar">
    <input class="input" type="date" bind:value={dateFrom} onchange={refresh} />
    <span class="text-sm">至</span>
    <input class="input" type="date" bind:value={dateTo} onchange={refresh} />
    <button class="btn" onclick={refresh} disabled={busy}>查询</button>
    <span class="text-sm text-muted">口径：期末 = 期初 + 本期借方 - 本期贷方（按科目方向）</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>科目代码</th><th>科目名称</th><th class="num">期初</th>
        <th class="num">本期借方</th><th class="num">本期贷方</th><th class="num">期末</th>
      </tr>
    </thead>
    <tbody>
      {#each rows as r (r.accountCode)}
        <tr>
          <td>{r.accountCode}</td><td>{r.accountName}</td>
          <td class="num">{formatMoney(r.begin)}</td>
          <td class="num">{formatMoney(r.debit)}</td>
          <td class="num">{formatMoney(r.credit)}</td>
          <td class="num">{formatMoney(r.end)}</td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">暂无数据（仅已审核凭证入账）</td></tr>
      {/each}
    </tbody>
  </table>

  <h3 class="section-title">收支日报</h3>
  <table class="data-table">
    <thead><tr><th>日期</th><th class="num">收入</th><th class="num">支出</th></tr></thead>
    <tbody>
      {#each daily as d (d.date)}
        <tr>
          <td class="whitespace-nowrap">{d.date}</td>
          <td class="num">{formatMoney(d.income)}</td>
          <td class="num">{formatMoney(d.expense)}</td>
        </tr>
      {:else}
        <tr><td colspan="3" class="empty">暂无数据</td></tr>
      {/each}
    </tbody>
  </table>
</section>
