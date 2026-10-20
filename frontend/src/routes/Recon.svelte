<!-- 对账中心（F4）：按结算日三方勾稽，匹配/差异两栏，差异进待处理不阻塞其他日期。
     数据经 ReconService.ReconcileByDate / ListDiffs（Wails 绑定）。 -->
<script lang="ts">
  import { ReconService } from '../../bindings/finlite/internal/services';
  import type { ReconResult, ReconDiff } from '../../bindings/finlite/internal/services/models';
  import { formatMoney } from '../lib/format';

  let date = $state('');
  let result = $state<ReconResult | null>(null);
  let diffs = $state<ReconDiff[]>([]);
  let dateFrom = $state('');
  let dateTo = $state('');

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  async function refreshDiffs(): Promise<void> {
    busy = true;
    try {
      diffs = (await ReconService.ListDiffs(dateFrom, dateTo)) ?? [];
      notice = diffs.length === 0 ? '全部日期三方一致' : '';
      noticeClass = diffs.length === 0 ? 'text-ok' : 'text-muted';
    } catch (e) {
      diffs = [];
      notice = `加载差异失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function reconcile(): Promise<void> {
    if (!date) {
      notice = '请先选择对账日期';
      noticeClass = 'text-warn';
      return;
    }
    busy = true;
    try {
      result = await ReconService.ReconcileByDate(date);
      notice = '';
    } catch (e) {
      result = null;
      notice = `对账失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  /** 差异行一键定位：跳到银行流水页并带上该日期（原行含文件+行号）。 */
  function locate(d: ReconDiff): void {
    date = d.date;
    reconcile();
  }

  // 顶栏刷新按钮（App.svelte 广播）：重拉待处理差异。
  $effect(() => {
    const handler = () => void refreshDiffs();
    window.addEventListener('finlite:refresh', handler);
    return () => window.removeEventListener('finlite:refresh', handler);
  });

  refreshDiffs();
</script>

<section>
  <div class="toolbar">
    <input class="input" type="date" bind:value={date} />
    <button class="btn" onclick={reconcile} disabled={busy}>对账</button>
    <span class="text-sm text-muted">待处理区间</span>
    <input class="input" type="date" bind:value={dateFrom} onchange={refreshDiffs} />
    <span class="text-sm text-muted">至</span>
    <input class="input" type="date" bind:value={dateTo} onchange={refreshDiffs} />
    <button class="btn" onclick={refreshDiffs} disabled={busy}>刷新差异</button>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  {#if result}
    <div class="my-3 flex flex-wrap gap-x-6 gap-y-2 text-sm">
      <div>订单实收 <b class="tabular-nums">{formatMoney(result.orderNet)}</b></div>
      <div>银行POS转入 <b class="tabular-nums">{formatMoney(result.bankPOSIn)}</b></div>
      <div>汇总实际收入 <b class="tabular-nums">{formatMoney(result.summaryActual)}</b></div>
      <div>
        {#if result.matched}
          <span class="text-ok font-semibold">三方一致</span>
        {:else}
          <span class="text-danger font-semibold">存在差异 {formatMoney(result.diff)}</span>
        {/if}
      </div>
    </div>
  {/if}
  <h3 class="section-title">待处理差异</h3>
  <table class="data-table">
    <thead>
      <tr><th>日期</th><th>字段</th><th class="num">预期</th><th class="num">实际</th><th>说明</th><th>操作</th></tr>
    </thead>
    <tbody>
      {#each diffs as d (d.date + d.field)}
        <tr>
          <td class="whitespace-nowrap">{d.date}</td>
          <td>{d.field}</td>
          <td class="num">{formatMoney(d.expect)}</td>
          <td class="num">{formatMoney(d.actual)}</td>
          <td>{d.detail}</td>
          <td><button class="btn btn-sm" onclick={() => locate(d)}>定位该日</button></td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">暂无差异</td></tr>
      {/each}
    </tbody>
  </table>
</section>
