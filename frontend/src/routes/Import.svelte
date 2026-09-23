<!-- 导入中心（F1）：选 TSV/CSV/XLSX 文件 → 预检列映射 → 确认后导入。
     流程：选文件 → PreviewColumns（识别关键列/样本行）→ 人工确认 → ImportFile 入库。 -->
<script lang="ts">
  import { ImportService } from '../../bindings/finlite/internal/services';
  import type { ImportResult, ImportError, ColumnMapping } from '../../bindings/finlite/internal/services/models';

  let batches = $state<ImportResult[]>([]);
  let errors = $state<ImportError[]>([]);
  let errorBatch = $state('');
  let preview = $state<ColumnMapping | null>(null);
  let pending = $state<{ name: string; content: string } | null>(null);
  let previewOpen = $state(false);

  let notice = $state('');
  let noticeClass = $state('text-muted');
  let busy = $state(false);
  let fileInput = $state<HTMLInputElement | null>(null);

  const TYPE_LABEL: Record<string, string> = {
    bank_flow: '银行流水',
    order_flow: '订单流水',
  };

  /** XLSX 是二进制，按文本读会破坏内容；此处用 ArrayBuffer + 单字节字符串保留原始字节。 */
  async function readFileContent(file: File): Promise<string> {
    if (file.name.toLowerCase().endsWith('.xlsx')) {
      const buf = new Uint8Array(await file.arrayBuffer());
      let s = '';
      const CHUNK = 8192;
      for (let i = 0; i < buf.length; i += CHUNK) {
        s += String.fromCharCode(...buf.subarray(i, i + CHUNK));
      }
      return s;
    }
    return file.text();
  }

  async function refresh(): Promise<void> {
    try {
      batches = (await ImportService.ListBatches('')) ?? [];
    } catch (e) {
      notice = `加载导入批次失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    }
  }

  function onPickFile(): void {
    fileInput?.click();
  }

  async function onFileChange(ev: Event): Promise<void> {
    const input = ev.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ''; // 允许重复选择同一文件
    if (!file) return;
    busy = true;
    notice = `正在解析 ${file.name} …`;
    noticeClass = 'text-muted';
    try {
      const content = await readFileContent(file);
      preview = await ImportService.PreviewColumns(file.name, content);
      pending = { name: file.name, content };
      previewOpen = true;
      notice = '';
    } catch (e) {
      notice = `解析失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function confirmImport(): Promise<void> {
    const p = pending;
    previewOpen = false;
    if (!p) return;
    busy = true;
    notice = `正在导入 ${p.name} …`;
    noticeClass = 'text-muted';
    try {
      const res = await ImportService.ImportFile(p.name, p.content);
      const warn = res.balanceWarning ? `；余额警告：${res.balanceWarning}` : '';
      notice =
        `导入完成（${TYPE_LABEL[res.sourceType] ?? res.sourceType}）` +
        `：共 ${res.rowCount} 行，成功 ${res.successCount}，跳过 ${res.skipCount}${warn}`;
      noticeClass = res.balanceWarning ? 'text-warn' : 'text-ok';
      pending = null;
      preview = null;
      await refresh();
    } catch (e) {
      notice = `导入失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function showErrors(b: ImportResult): Promise<void> {
    try {
      errors = (await ImportService.ListErrors(b.batchId)) ?? [];
      errorBatch = b.fileName;
    } catch (e) {
      notice = `读取错误明细失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    }
  }

  refresh();
</script>

<section>
  <div class="toolbar">
    <button class="btn" onclick={onPickFile} disabled={busy}>选择文件导入</button>
    <input
      bind:this={fileInput}
      type="file"
      accept=".tsv,.csv,.xlsx,text/tab-separated-values,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
      class="hidden"
      onchange={onFileChange}
    />
    <span class="text-sm text-muted">支持 TSV / CSV / XLSX，按文件名自动识别类型；导入前先预览列映射</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead>
      <tr>
        <th>类型</th><th>文件名</th><th class="num">总行数</th><th class="num">成功</th>
        <th class="num">跳过</th><th>余额警告</th><th>明细</th>
      </tr>
    </thead>
    <tbody>
      {#each batches as b (b.batchId)}
        <tr>
          <td>{TYPE_LABEL[b.sourceType] ?? b.sourceType}</td>
          <td>{b.fileName}</td>
          <td class="num">{b.rowCount}</td>
          <td class="num">{b.successCount}</td>
          <td class="num">{b.skipCount}</td>
          <td>{b.balanceWarning}</td>
          <td><button class="btn btn-sm" onclick={() => showErrors(b)} disabled={busy}>错误明细</button></td>
        </tr>
      {:else}
        <tr><td colspan="7" class="empty">暂无导入批次，请选择银行流水 / 订单流水文件</td></tr>
      {/each}
    </tbody>
  </table>

  {#if errorBatch}
    <h3 class="section-title">批次「{errorBatch}」错误明细</h3>
    <table class="data-table">
      <thead><tr><th class="num">行号</th><th>字段</th><th>说明</th></tr></thead>
      <tbody>
        {#each errors as e (e.id)}
          <tr><td class="num">{e.rowNo}</td><td>{e.fieldName}</td><td>{e.message}</td></tr>
        {:else}
          <tr><td colspan="3" class="empty">该批次无错误明细</td></tr>
        {/each}
      </tbody>
    </table>
  {/if}

  {#if previewOpen && preview}
    <div class="modal-overlay" role="presentation" onclick={() => (previewOpen = false)}>
      <div
        class="modal-box modal-box-wide"
        role="dialog"
        aria-modal="true"
        aria-label="列映射预览"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') previewOpen = false; }}
        tabindex="-1"
      >
        <h3 class="mb-1 text-base font-semibold">列映射预览：{preview.fileName}</h3>
        <p class="mb-3 text-sm text-muted">
          识别类型 {TYPE_LABEL[preview.sourceType] ?? preview.sourceType} ｜ 数据 {preview.totalRows} 行
        </p>
        <p class="mb-2 text-sm">
          已识别关键列：
          {#if (preview.matched ?? []).length > 0}<b>{(preview.matched ?? []).join('、')}</b>{:else}<span class="text-danger">无</span>{/if}
        </p>
        {#if (preview.missing ?? []).length > 0}
          <p class="mb-2 text-sm text-warn">缺失列：{(preview.missing ?? []).join('、')}（导入将报错，请检查表头）</p>
        {/if}
        <table class="data-table">
          <thead>
            <tr>{#each preview.header ?? [] as h, i (i)}<th>{h}</th>{/each}</tr>
          </thead>
          <tbody>
            {#each preview.sampleRows ?? [] as r, i (i)}
              <tr>{#each r as c, j (j)}<td>{c}</td>{/each}</tr>
            {:else}
              <tr><td colspan={(preview.header ?? []).length} class="empty">文件无数据行</td></tr>
            {/each}
          </tbody>
        </table>
        <div class="modal-actions mt-4">
          <button class="btn" onclick={() => (previewOpen = false)}>取消</button>
          <button class="btn" onclick={confirmImport} disabled={busy}>确认导入</button>
        </div>
      </div>
    </div>
  {/if}
</section>
