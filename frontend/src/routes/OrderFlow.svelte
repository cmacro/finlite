<!-- 订单流水（F3）：费率复算差异提示，±0.01 外标异。
TODO: 接 OrderFlowService.List（bindings 生成后）。 -->
<script lang="ts">
  import type { OrderFlow } from '../lib/types';
  import { formatMoney, feeFlagText } from '../lib/format';

  let flows = $state<OrderFlow[]>([]);
  let settleDate = $state('');
  let onlyFeeAdj = $state(false);
</script>

<section>
  <div class="toolbar">
    <input class="input" type="date" bind:value={settleDate} />
    <label class="flex items-center gap-1 text-sm"><input type="checkbox" bind:checked={onlyFeeAdj} /> 仅看费率差异</label>
    <button class="btn" disabled title="bindings 生成后启用">查询</button>
  </div>
  <table class="data-table">
    <thead>
      <tr><th>工行订单号</th><th>结算日期</th><th class="num">订单金额</th><th class="num">手续费</th><th class="num">实收</th><th>支付方式</th><th>费率检查</th></tr>
    </thead>
    <tbody>
      {#each flows as o}
        <tr>
          <td>{o.icbcOrderNo}</td><td>{o.settleDate}</td>
          <td class="num">{formatMoney(o.orderAmount)}</td><td class="num">{formatMoney(o.feeAmount)}</td><td class="num">{formatMoney(o.netAmount)}</td>
          <td>{o.payMethod || o.debitCreditFlag}</td>
          <td>{#if o.feeFlag}<span class="ml-1 font-semibold text-warn">{feeFlagText(o.feeFlag)}</span>{/if}</td>
        </tr>
      {:else}
        <tr><td colspan="7" class="empty">暂无数据，请先在导入中心导入订单流水</td></tr>
      {/each}
    </tbody>
  </table>
</section>
