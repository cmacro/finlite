<!-- 导入中心（F1）：选 TSV 文件 → 按文件名自动识别类型 → 解析 → 去重 → 余额校验 → 入库。
     前端读取文件内容后传 ImportService.ImportFile（Service 不依赖原生对话框）。 -->
<script lang="ts">
  import { ImportService } from '../../bindings/finlite/internal/services';
  import type { ImportResult } from '../../bindings/finlite/internal/services/models';

  let batches = $state<ImportResult[]>([]);
  let notice = $state('');
  let noticeClass = $state('text-muted');
  let busy = $state(false);
  let fileInput = $state<HTMLInputElement | null>(null);

  const TYPE_LABEL: Record<string, string> = {
    bank_flow: '银行流水',
    order_flow: '订单流水',
  };

  async function refresh() {
    try {
      batches = (await ImportService.ListBatches('')) ?? [];
    } catch (e) {
      notice = `加载导入批次失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    }
  }

  async function onPickFile() {
    fileInput?.click();
  }

  async function onFileChange(ev: Event) {
    const input = ev.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ''; // 允许重复选择同一文件
    if (!file) {
      notice = '';
      return;
    }
    busy = true;
    notice = `正在导入 ${file.name} …`;
    noticeClass = 'text-muted';
    try {
      const content = await file.text();
      const res = await ImportService.ImportFile(file.name, content);
      const warn = res.balanceWarning ? `；余额警告：${res.balanceWarning}` : '';
      notice =
        `导入完成（${TYPE_LABEL[res.sourceType] ?? res.sourceType}）` +
        `：共 ${res.rowCount} 行，成功 ${res.successCount}，跳过 ${res.skipCount}${warn}`;
      noticeClass = res.balanceWarning ? 'text-warn' : 'text-ok';
      await refresh();
    } catch (e) {
      notice = `导入失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  refresh();
</script>

<section>
  <div class="toolbar">
    <button class="btn" onclick={onPickFile} disabled={busy}>
      选择文件导入
    </button>
    <input
      bind:this={fileInput}
      type="file"
      accept=".tsv,.csv,text/tab-separated-values,text/csv"
      class="hidden"
      onchange={onFileChange}
    />
    <span class="text-sm text-muted">支持银行流水 / 订单流水 TSV，按文件名自动识别类型</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>类型</th><th>文件名</th><th class="num">总行数</th><th class="num">成功</th><th class="num">跳过</th><th>余额警告</th>
      </tr>
    </thead>
    <tbody>
      {#each batches as b}
        <tr>
          <td>{TYPE_LABEL[b.sourceType] ?? b.sourceType}</td>
          <td>{b.fileName}</td>
          <td class="num">{b.rowCount}</td>
          <td class="num">{b.successCount}</td>
          <td class="num">{b.skipCount}</td>
          <td>{b.balanceWarning}</td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">暂无导入批次，请选择银行流水 / 订单流水 TSV</td></tr>
      {/each}
    </tbody>
  </table>
</section>