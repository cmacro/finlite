<!-- 银行流水（F2）：三色余额状态 + 批次确认/撤销（需确认框）。
TODO: 接 BankFlowService.List / ConfirmBatch / UnconfirmBatch（bindings 生成后）。 -->
<script lang="ts">
  import type { BankFlow } from '../lib/types';
  import { formatMoney, statusText, statusClass } from '../lib/format';
  import Confirm from '../lib/Confirm.svelte';

  let flows = $state<BankFlow[]>([]);
  let keyword = $state('');
  let confirmOpen = $state(false);
  let pendingBatch = $state('');

  function askConfirm(batchId: string): void {
    pendingBatch = batchId;
    confirmOpen = true;
  }
  function doConfirm(): void {
    confirmOpen = false;
    pendingBatch = '';
    // TODO: BankFlowService.ConfirmBatch(pendingBatch)
  }
</script>

<section>
  <h2 class="page-title">银行流水</h2>
  <div class="toolbar">
    <input class="input" placeholder="对方/摘要/附言关键字" bind:value={keyword} />
    <button class="btn" disabled title="bindings 生成后启用">查询</button>
    <button class="btn" onclick={() => askConfirm('')} disabled={flows.length === 0}>确认本批余额</button>
  </div>
  <table class="data-table">
    <thead>
      <tr><th>交易时间</th><th>对方单位</th><th>摘要</th><th class="num">转入</th><th class="num">转出</th><th class="num">余额</th><th>校验</th></tr>
    </thead>
    <tbody>
      {#each flows as f}
        <tr>
          <td>{f.txnTime}</td><td>{f.counterparty}</td><td>{f.summary}</td>
          <td class="num">{formatMoney(f.inAmount)}</td><td class="num">{formatMoney(f.outAmount)}</td><td class="num">{formatMoney(f.balance)}</td>
          <td><span class={statusClass(f.displayStatus)}>{statusText(f.displayStatus)}</span></td>
        </tr>
      {:else}
        <tr><td colspan="7" class="empty">暂无数据，请先在导入中心导入银行流水</td></tr>
      {/each}
    </tbody>
  </table>
  <Confirm open={confirmOpen} message="确认本批次余额连续性吗？确认后红底消除，可撤销。" onconfirm={doConfirm} oncancel={() => (confirmOpen = false)} />
</section>
