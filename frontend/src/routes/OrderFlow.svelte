<!-- 订单流水（F3）：列表 + 费率复算差异提示，手续费 ±0.01 外标异。
     数据经 OrderFlowService.List（Wails 绑定）；"仅看费率差异" 在服务端过滤（OnlyFeeAdj）。 -->
<script lang="ts">
  import { OrderFlowService } from '../../bindings/finlite/internal/services';
  import type { OrderFlow, OrderFlowPage } from '../../bindings/finlite/internal/services/models';
  import { formatMoney, feeFlagText } from '../lib/format';

  const PAGE_SIZE = 50;

  let flows = $state<OrderFlow[]>([]);
  let total = $state(0);
  let page = $state(1);
  let settleDate = $state('');
  let keyword = $state('');
  let onlyFeeAdj = $state(false);

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  let pageCount = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

  async function refresh(): Promise<void> {
    busy = true;
    try {
      const res: OrderFlowPage = await OrderFlowService.List({
        settleDate,
        keyword: keyword.trim(),
        onlyFeeAdj: onlyFeeAdj ? '1' : '',
        page,
        pageSize: PAGE_SIZE,
      });
      flows = res.items ?? [];
      total = res.total;
      page = res.page;
      notice = '';
    } catch (e) {
      flows = [];
      total = 0;
      notice = `加载订单流水失败：${e instanceof Error ? e.message : String(e)}`;
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

  refresh();
</script>

<section>
  <div class="toolbar">
    <input class="input" type="date" bind:value={settleDate} onchange={doSearch} />
    <input
      class="input"
      placeholder="工行订单号 / 第三方订单号"
      bind:value={keyword}
      onkeydown={(e) => { if (e.key === 'Enter') doSearch(); }}
    />
    <label class="flex items-center gap-1 text-sm">
      <input type="checkbox" bind:checked={onlyFeeAdj} onchange={doSearch} /> 仅看费率差异
    </label>
    <button class="btn" onclick={doSearch} disabled={busy}>查询</button>
    <span class="text-sm text-muted">共 {total} 条 · 第 {page} / {pageCount} 页</span>
    {#if busy}<span class="text-sm text-muted">加载中…</span>{/if}
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>工行订单号</th><th>结算日期</th><th class="num">订单金额</th><th class="num">手续费</th>
        <th class="num">实收</th><th>支付方式</th><th>费率检查</th>
      </tr>
    </thead>
    <tbody>
      {#each flows as o (o.id)}
        <tr>
          <td class="whitespace-nowrap">{o.icbcOrderNo}</td>
          <td class="whitespace-nowrap">{o.settleDate}</td>
          <td class="num">{formatMoney(o.orderAmount)}</td>
          <td class="num">{formatMoney(o.feeAmount)}</td>
          <td class="num">{formatMoney(o.netAmount)}</td>
          <td>{o.payMethod || o.debitCreditFlag}</td>
          <td>
            {#if o.feeFlag}<span class="font-semibold text-warn">{feeFlagText(o.feeFlag)}</span>{/if}
          </td>
        </tr>
      {:else}
        <tr><td colspan="7" class="empty">暂无数据，请先在导入中心导入订单流水</td></tr>
      {/each}
    </tbody>
  </table>
  <div class="mt-2 flex items-center gap-2">
    <button class="btn btn-sm" onclick={() => goPage(page - 1)} disabled={busy || page <= 1}>上一页</button>
    <button class="btn btn-sm" onclick={() => goPage(page + 1)} disabled={busy || page >= pageCount}>下一页</button>
  </div>
</section>
