<!-- 导入中心（F1）：选 TSV 文件 → 解析 → 去重 → 余额校验 → 入库。
TODO: 接 ImportService.ImportBankFile / ImportOrderFile / ListBatches（bindings 生成后）。 -->
<script lang="ts">
  import type { ImportResult } from '../lib/types';

  let batches = $state<ImportResult[]>([]);
  let notice = $state('');
</script>

<section>
  <h2 class="page-title">导入中心</h2>
  <div class="toolbar">
    <button class="btn" disabled title="bindings 生成后启用">导入银行流水 TSV</button>
    <button class="btn" disabled title="bindings 生成后启用">导入订单流水 TSV</button>
  </div>
  {#if notice}<p class="my-2 text-sm text-muted">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr><th>文件名</th><th class="num">总行数</th><th class="num">成功</th><th class="num">跳过</th><th>余额警告</th></tr>
    </thead>
    <tbody>
      {#each batches as b}
        <tr><td>{b.fileName}</td><td class="num">{b.rowCount}</td><td class="num">{b.successCount}</td><td class="num">{b.skipCount}</td><td>{b.balanceWarning}</td></tr>
      {:else}
        <tr><td colspan="5" class="empty">暂无导入批次，请先导入样本 TSV</td></tr>
      {/each}
    </tbody>
  </table>
</section>
