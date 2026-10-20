<!-- 导入中心（F1）：拖拽/选择 TSV/CSV/XLSX → 预览列映射 → 人工确认 → 入库 → 批次历史/错误明细。
     流程与视觉基准：docs/uidesign/import-center.html；数据经 ImportService（Wails 绑定）。
     原始表只读：本页无编辑入口，错行靠「重新导入 + 去重跳过」处理。 -->
<script lang="ts">
  import { Dialogs } from '@wailsio/runtime';
  import type { RouteKey } from '../lib/types';
  import { ImportService } from '../../bindings/finlite/internal/services';
  import type {
    ImportResult,
    ImportError,
    ColumnMapping,
    RawFile,
    ImportRawResult,
  } from '../../bindings/finlite/internal/services/models';
  import Icon from '../lib/Icon.svelte';

  /** 结果横幅：一次导入可能产生多条（成功 + 余额警告 + 归档失败提示）。 */
  type Banner = { kind: 'ok' | 'warn' | 'err'; text: string; goto?: boolean };

  interface Props {
    /** 结果横幅「去银行流水确认」跳转（由 App.svelte 注入路由切换）。 */
    onGoto?: (key: RouteKey) => void;
  }
  let { onGoto }: Props = $props();

  const TYPE_LABEL: Record<string, string> = {
    bank_flow: '银行流水',
    order_flow: '订单流水',
  };

  /** 后端 import_errors.field_name → 中文列名（错误抽屉展示）。 */
  const FIELD_LABEL: Record<string, string> = {
    txn_time: '交易时间',
    amount: '金额',
    settle_date: '结算日期',
    icbc_order_no: '工行订单号',
    balance_discontinuity: '余额',
  };

  const STEPS = [
    { t: '选择文件', s: '拖拽或浏览本地文件' },
    { t: '解析并预检列映射', s: '读取表头与样本行' },
    { t: '人工确认字段识别', s: '核对关键列与缺失列' },
    { t: '入库并展示结果', s: '写入本地 SQLite' },
  ];

  let batches = $state<ImportResult[]>([]);
  let step = $state(1);
  let dragging = $state(false);
  let busy = $state(false);

  // 列映射预览
  let preview = $state<ColumnMapping | null>(null);
  let previewOpen = $state(false);
  let previewParsing = $state(false);
  let pending = $state<{ name: string; content: string } | null>(null);
  // 解析请求序号：取消/重选后丢弃旧请求的回写，避免弹窗状态错乱
  let parseSeq = 0;

  // 结果反馈
  let banners = $state<Banner[]>([]);

  // 错误明细抽屉
  let errors = $state<ImportError[]>([]);
  let drawerBatch = $state<ImportResult | null>(null);
  let drawerOpen = $state(false);
  let drawerLoading = $state(false);

  let fileInput = $state<HTMLInputElement | null>(null);

  // 默认路径导入
  let rawDir = $state('');
  let rawDirExists = $state(true);
  let rawFiles = $state<RawFile[]>([]);
  let rawScannedAt = $state('');
  let rawLoading = $state(false);
  let rawImporting = $state(false);
  let archiveAfter = $state(true);
  let selected = $state<string[]>([]); // 已勾选文件名（仅 state=new 可选）
  let rawPendingName = $state(''); // 弹窗当前来源：非空表示来自默认路径预览

  const stats = $derived({
    batches: batches.length,
    ok: batches.reduce((sum, b) => sum + b.successCount, 0),
    skip: batches.reduce((sum, b) => sum + b.skipCount, 0),
    warn: batches.filter((b) => (b.balanceWarning ?? '') !== '').length,
  });

  const matched = $derived(preview?.matched ?? []);
  const missing = $derived(preview?.missing ?? []);
  const emptyFile = $derived(!!preview && preview.totalRows === 0);
  const errorCount = $derived(errors.filter((e) => !isWarnError(e)).length);
  const warnCount = $derived(errors.filter((e) => isWarnError(e)).length);

  const selectableRaw = $derived(rawFiles.filter((f) => f.state === 'new'));
  const selectedCount = $derived(selected.length);
  const allSelected = $derived(selectableRaw.length > 0 && selectedCount === selectableRaw.length);
  const rawSummary = $derived({
    total: rawFiles.length,
    pending: rawFiles.filter((f) => f.state === 'new').length,
    done: rawFiles.filter((f) => f.state === 'done').length,
    bad: rawFiles.filter((f) => f.state === 'bad').length,
  });

  function isWarnError(e: ImportError): boolean {
    return e.fieldName === 'balance_discontinuity';
  }

  function fieldLabel(name: string): string {
    return FIELD_LABEL[name] ?? name ?? '—';
  }

  function errText(e: unknown): string {
    return e instanceof Error ? e.message : String(e);
  }

  function formatCount(n: number): string {
    return n.toLocaleString('zh-CN');
  }

  /** 文件大小：B / KB / MB，2 位以内小数（扫描列表展示）。 */
  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }

  function pushBanners(items: Banner[]): void {
    banners = [...banners, ...items];
  }

  /** RFC3339 → 本地 `YYYY-MM-DD HH:mm`（导入时间展示）。 */
  function formatStamp(iso: string): string {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  }

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
      banners = [{ kind: 'err', text: `加载导入批次失败：${errText(e)}` }];
    }
  }

  function openPicker(): void {
    fileInput?.click();
  }

  function onDrop(ev: DragEvent): void {
    ev.preventDefault();
    dragging = false;
    const file = ev.dataTransfer?.files?.[0];
    if (file) void handleFile(file);
  }

  async function handleFile(file: File): Promise<void> {
    if (busy || rawImporting || previewParsing) return;
    const seq = ++parseSeq;
    banners = [];
    rawPendingName = '';
    pending = { name: file.name, content: '' };
    errors = [];
    drawerBatch = null;
    preview = null;
    previewParsing = true;
    previewOpen = true;
    step = 2;
    try {
      const content = await readFileContent(file);
      if (seq !== parseSeq) return;
      const map = await ImportService.PreviewColumns(file.name, content);
      if (seq !== parseSeq) return;
      preview = map;
      pending = { name: file.name, content };
      step = 3;
    } catch (e) {
      if (seq !== parseSeq) return;
      previewOpen = false;
      pending = null;
      step = 1;
      banners = [{ kind: 'err', text: `解析失败：${errText(e)}` }];
    } finally {
      if (seq === parseSeq) previewParsing = false;
    }
  }

  async function onFileChange(ev: Event): Promise<void> {
    const input = ev.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ''; // 允许重复选择同一文件
    if (file) await handleFile(file);
  }

  function closePreview(): void {
    parseSeq++;
    previewOpen = false;
    previewParsing = false;
    preview = null;
    pending = null;
    rawPendingName = '';
    if (step < 4) step = 1;
  }

  function importOkBanner(res: ImportResult): Banner {
    return {
      kind: 'ok',
      text:
        `导入完成（${TYPE_LABEL[res.sourceType] ?? res.sourceType}）：共 ${formatCount(res.rowCount)} 行，` +
        `成功 ${formatCount(res.successCount)} 行，跳过 ${formatCount(res.skipCount)} 行。`,
    };
  }

  function importWarnBanner(res: ImportResult): Banner | null {
    if (!res.balanceWarning) return null;
    return {
      kind: 'warn',
      text: `余额校验警告（${res.fileName}）：${res.balanceWarning}。已入库并标记为「不平」，不影响其他行。`,
      goto: true,
    };
  }

  async function confirmImport(): Promise<void> {
    const map = preview;
    if (!map) return;
    if (missing.length > 0) {
      closePreview();
      step = 1;
      banners = [
        {
          kind: 'err',
          text: `缺少关键列「${missing.join('、')}」，无法完成字段映射，请检查表头后重新选择文件。`,
        },
      ];
      return;
    }
    if (map.totalRows === 0) {
      closePreview();
      step = 1;
      banners = [{ kind: 'err', text: '文件无数据行，未导入任何流水；请检查文件是否为空白或仅有表头。' }];
      return;
    }
    // 来自默认路径的预览：走 ImportRawFiles（沿用归档开关），并刷新扫描结果。
    const rawName = rawPendingName;
    const p = pending;
    previewOpen = false;
    previewParsing = false;
    preview = null;
    pending = null;
    rawPendingName = '';
    if (rawName) {
      await importRaw([rawName]);
      return;
    }
    if (!p) return;
    busy = true;
    try {
      const res = await ImportService.ImportFile(p.name, p.content);
      banners = [importOkBanner(res)];
      const warn = importWarnBanner(res);
      if (warn) banners = [...banners, warn];
      step = 4;
      await refresh();
    } catch (e) {
      step = 1;
      banners = [{ kind: 'err', text: `导入失败：${errText(e)}` }];
    } finally {
      busy = false;
    }
  }

  async function openErrors(b: ImportResult): Promise<void> {
    drawerBatch = b;
    errors = [];
    drawerOpen = true;
    drawerLoading = true;
    try {
      errors = (await ImportService.ListErrors(b.batchId)) ?? [];
    } catch (e) {
      drawerOpen = false;
      drawerBatch = null;
      banners = [{ kind: 'err', text: `读取错误明细失败：${errText(e)}` }];
    } finally {
      drawerLoading = false;
    }
  }

  function closeDrawer(): void {
    drawerOpen = false;
    drawerBatch = null;
    errors = [];
  }

  function onKeydown(ev: KeyboardEvent): void {
    if (ev.key !== 'Escape') return;
    if (previewOpen) closePreview();
    else if (drawerOpen) closeDrawer();
  }

  /** 样本单元格是否金额/数量：是则右对齐等宽数字（与设计原型一致）。 */
  function isNumericCell(v: string): boolean {
    return /^-?[\d,]+(\.\d+)?$/.test((v ?? '').trim());
  }

  // ---------- 默认路径导入 ----------

  async function scanRaw(): Promise<void> {
    rawLoading = true;
    try {
      const scan = await ImportService.ScanRawDir();
      rawDir = scan.dir;
      rawDirExists = scan.dirExists;
      rawFiles = scan.files ?? [];
      rawScannedAt = scan.scannedAt;
      // 重新扫描后默认勾选全部待导入文件（与设计原型一致）
      selected = rawFiles.filter((f) => f.state === 'new').map((f) => f.name);
    } catch (e) {
      banners = [{ kind: 'err', text: `扫描默认路径失败：${errText(e)}` }];
    } finally {
      rawLoading = false;
    }
  }

  async function chooseRawDir(): Promise<void> {
    try {
      const picked = await Dialogs.OpenFile({
        CanChooseDirectories: true,
        CanChooseFiles: false,
        Title: '选择原始数据目录',
        ButtonText: '选择此目录',
        Directory: rawDir || undefined,
      });
      const dir = Array.isArray(picked) ? picked[0] : picked;
      if (!dir) return;
      rawDir = await ImportService.SetRawDir(dir);
      banners = [];
      await scanRaw();
    } catch (e) {
      banners = [{ kind: 'err', text: `更改默认路径失败：${errText(e)}` }];
    }
  }

  function isSelected(name: string): boolean {
    return selected.includes(name);
  }

  function toggleSelect(name: string, on: boolean): void {
    if (on) selected = [...new Set([...selected, name])];
    else selected = selected.filter((n) => n !== name);
  }

  function toggleSelectAll(): void {
    selected = allSelected ? [] : selectableRaw.map((f) => f.name);
  }

  /** 逐个文件导入并汇总横幅；单文件失败不阻断其他文件。 */
  async function importRaw(names: string[]): Promise<void> {
    if (names.length === 0 || rawImporting) return;
    rawImporting = true;
    banners = [];
    try {
      const results = (await ImportService.ImportRawFiles({ names, archive: archiveAfter })) ?? [];
      banners = rawResultBanners(results);
      const imported = results.some((r) => r.status === 'imported');
      step = imported ? 4 : 1;
      await refresh();
      await scanRaw();
    } catch (e) {
      step = 1;
      banners = [{ kind: 'err', text: `导入失败：${errText(e)}` }];
    } finally {
      rawImporting = false;
    }
  }

  function rawResultBanners(results: ImportRawResult[]): Banner[] {
    const out: Banner[] = [];
    for (const r of results) {
      if (r.status !== 'imported') {
        out.push({ kind: 'err', text: `导入失败（${r.fileName}）：${r.error || '未知错误'}` });
        continue;
      }
      const res = r.result;
      const archived = r.archivedTo ? ' 已归档到 archived/。' : '';
      out.push({
        kind: 'ok',
        text:
          `导入完成（${TYPE_LABEL[res.sourceType] ?? res.sourceType}）${r.fileName}：共 ${formatCount(res.rowCount)} 行，` +
          `成功 ${formatCount(res.successCount)} 行，跳过 ${formatCount(res.skipCount)} 行。${archived}`,
      });
      if (r.error) out.push({ kind: 'warn', text: `${r.fileName}：${r.error}` });
      const warn = importWarnBanner(res);
      if (warn) out.push(warn);
    }
    return out;
  }

  async function openRawPreview(f: RawFile): Promise<void> {
    const seq = ++parseSeq;
    banners = [];
    pending = null;
    rawPendingName = f.name;
    preview = null;
    previewParsing = true;
    previewOpen = true;
    step = 2;
    try {
      const map = await ImportService.PreviewRawFile(f.name);
      if (seq !== parseSeq) return;
      preview = map;
      step = 3;
    } catch (e) {
      if (seq !== parseSeq) return;
      previewOpen = false;
      rawPendingName = '';
      step = 1;
      banners = [{ kind: 'err', text: `解析失败：${errText(e)}` }];
    } finally {
      if (seq === parseSeq) previewParsing = false;
    }
  }

  // 顶栏刷新按钮（App.svelte 广播）：批次列表与默认路径一起重扫。
  $effect(() => {
    const handler = () => {
      void refresh();
      void scanRaw();
    };
    window.addEventListener('finlite:refresh', handler);
    return () => window.removeEventListener('finlite:refresh', handler);
  });

  refresh();
  scanRaw();
</script>

<svelte:window onkeydown={onKeydown} />

<section>
  <!-- 1. 只读与校验口径说明 -->
  <div class="info-banner" role="note">
    <span class="ico"><Icon name="info" size={17} /></span>
    <div class="body">
      <strong>本页为只读原始表，不支持编辑。</strong>
      重复导入按「工行订单号 / 银行行」去重，重复行将自动跳过并计数。
      <span class="sep">·</span>
      银行流水导入后会进行<strong>严格余额连续性校验</strong>，余额不连续<strong>不阻断导入</strong>，但会标红等待人工确认。
    </div>
  </div>

  <!-- 2. 导入流程 -->
  <div class="stepper" aria-label="导入流程">
    {#each STEPS as s, i (s.t)}
      {#if i > 0}<div class="step-line {step > i ? 'done' : ''}"></div>{/if}
      <div class="step {step === i + 1 ? 'active' : ''} {step > i + 1 ? 'done' : ''}">
        <span class="step-num">{i + 1}</span>
        <span class="step-text"><span class="t">{s.t}</span><span class="s">{s.s}</span></span>
      </div>
    {/each}
  </div>

  <!-- 3. 统计卡 -->
  <div class="stats">
    <div class="stat">
      <div class="stat-label"><Icon name="file" size={14} />已导入批次</div>
      <div class="stat-value">{formatCount(stats.batches)}<span class="stat-unit">个</span></div>
      <div class="stat-sub">当前账套累计</div>
    </div>
    <div class="stat">
      <div class="stat-label"><Icon name="check" size={14} />累计成功行</div>
      <div class="stat-value">{formatCount(stats.ok)}<span class="stat-unit">行</span></div>
      <div class="stat-sub">已写入流水明细</div>
    </div>
    <div class="stat">
      <div class="stat-label"><Icon name="list" size={14} />累计跳过（去重）行</div>
      <div class="stat-value">{formatCount(stats.skip)}<span class="stat-unit">行</span></div>
      <div class="stat-sub">重复行自动跳过</div>
    </div>
    <div class="stat {stats.warn > 0 ? 'warn' : ''}">
      <div class="stat-label"><Icon name="alert-triangle" size={14} />余额警告批次</div>
      <div class="stat-value">{formatCount(stats.warn)}<span class="stat-unit">个</span></div>
      <div class="stat-sub">待人工确认余额衔接</div>
    </div>
  </div>

  <!-- 4. 上传区 -->
  <div class="section">
    <div class="card">
      <div class="card-head">
        <span class="card-title"><span class="ico"><Icon name="upload" size={15} /></span>导入新文件</span>
        <div class="flex-1"></div>
        <span class="text-[12px] text-muted">单据只读 · 按文件名自动识别类型</span>
      </div>
      <div class="card-body">
        <div
          class="dropzone {dragging ? 'dragover' : ''}"
          tabindex="0"
          role="button"
          aria-label="拖拽或点击选择文件"
          onclick={openPicker}
          onkeydown={(ev) => {
            if (ev.key === 'Enter' || ev.key === ' ') {
              ev.preventDefault();
              openPicker();
            }
          }}
          ondragover={(ev) => {
            ev.preventDefault();
            dragging = true;
          }}
          ondragleave={() => (dragging = false)}
          ondrop={onDrop}
        >
          <div class="dz-icon"><Icon name="upload" size={26} /></div>
          <div class="dz-title">将 TSV / CSV / XLSX 文件拖到此处，或<span class="accent">点击选择文件</span></div>
          <div class="dz-sub">支持银行流水、订单流水；导入前先预览列映射再确认入库</div>
          <div class="dz-tags">
            <span class="fmt">TSV</span>
            <span class="fmt">CSV</span>
            <span class="fmt">XLSX</span>
          </div>
          <button
            class="btn btn-primary btn-lg"
            type="button"
            disabled={busy || rawImporting}
            onclick={(ev) => {
              ev.stopPropagation();
              openPicker();
            }}
          >
            <Icon name="plus" size={15} />
            选择文件
          </button>
        </div>
        <input
          bind:this={fileInput}
          type="file"
          accept=".tsv,.csv,.xlsx,text/tab-separated-values,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          class="hidden"
          onchange={onFileChange}
        />

        <!-- 5. 结果反馈：上传导入与默认路径导入共用同一结果区 -->
        {#if banners.length > 0}
          <div class="results" aria-live="polite">
            {#each banners as b, i (i)}
              <div class="banner {b.kind}">
                <span class="ico">
                  <Icon name={b.kind === 'ok' ? 'circle-check' : b.kind === 'warn' ? 'alert-triangle' : 'x'} size={16} />
                </span>
                <div class="body">
                  {b.text}
                  {#if b.goto && onGoto}
                    <button
                      class="goto cursor-pointer border-0 bg-transparent p-0 text-[13px]"
                      onclick={() => onGoto?.('bank-flow')}
                    >
                      去银行流水确认余额
                    </button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>

  <!-- 6. 默认路径导入 -->
  <div class="section">
    <div class="card">
      <div class="card-head">
        <span class="card-title"><span class="ico"><Icon name="folder" size={15} /></span>默认路径导入</span>
        <div class="flex-1"></div>
        <span class="text-[12px] text-muted">扫描原始数据目录 · 免手动逐份选择</span>
      </div>
      <div class="card-body">
        <div class="path-bar">
          <span class="path-label">默认路径</span>
          <code class="path-value">{rawDir || '未设置'}</code>
          <span class="path-hint">
            {rawDirExists ? '目录存在 · 仅扫描 TSV / CSV / XLSX' : '目录不存在，请更改目录或放入文件'}
          </span>
          <div class="flex-1"></div>
          <button class="btn btn-sm" onclick={scanRaw} disabled={rawLoading || rawImporting}>
            <Icon name="refresh" size={14} />
            重新扫描
          </button>
          <button class="btn btn-sm" onclick={chooseRawDir} disabled={rawLoading || rawImporting}>
            <Icon name="folder" size={14} />
            更改目录…
          </button>
        </div>

        <div class="scan-meta">
          {#if rawLoading}
            <span>正在扫描 {rawDir} …</span>
          {:else}
            <span>扫描到 <b>{formatCount(rawSummary.total)}</b> 个文件</span><span class="sep"></span>
            <span>待导入 <b>{formatCount(rawSummary.pending)}</b></span><span class="sep"></span>
            <span>已导入 <b>{formatCount(rawSummary.done)}</b></span>
            {#if rawSummary.bad > 0}
              <span class="sep"></span><span>不支持 <b>{formatCount(rawSummary.bad)}</b></span>
            {/if}
            <span class="sep"></span><span>最近扫描 {rawScannedAt ? formatStamp(rawScannedAt) : '—'}</span>
          {/if}
        </div>

        <table class="data-table">
          <thead>
            <tr>
              <th class="cb">
                <input
                  type="checkbox"
                  aria-label="全选"
                  checked={allSelected}
                  disabled={selectableRaw.length === 0 || rawLoading}
                  onchange={toggleSelectAll}
                />
              </th>
              <th>文件名</th>
              <th style="width:118px">识别类型</th>
              <th class="num" style="width:96px">大小</th>
              <th style="width:150px">修改时间</th>
              <th style="width:170px">状态</th>
              <th style="width:184px">操作</th>
            </tr>
          </thead>
          <tbody>
            {#if rawLoading}
              <tr>
                <td colspan="7" class="px-3.5 py-5 text-center text-muted">正在扫描 {rawDir} …</td>
              </tr>
            {/if}
            {#each rawLoading ? [] : rawFiles as f (f.name)}
              {@const missCols = f.missingColumns ?? []}
              <tr>
                <td class="cb">
                  <input
                    type="checkbox"
                    aria-label="选择 {f.name}"
                    checked={isSelected(f.name)}
                    disabled={f.state !== 'new' || rawLoading || rawImporting}
                    onchange={(ev) => toggleSelect(f.name, ev.currentTarget.checked)}
                  />
                </td>
                <td>
                  <span class="file-name">{f.name}</span>
                  {#if missCols.length > 0}
                    <span class="miss-note">缺列：{missCols.join('、')}</span>
                  {/if}
                </td>
                <td>
                  {#if f.sourceType}
                    <span class="tbl-tag {f.sourceType === 'bank_flow' ? 'bank' : ''}">
                      <span class="dot"></span>{TYPE_LABEL[f.sourceType] ?? f.sourceType}
                    </span>
                  {:else}
                    <span class="tbl-tag"><span class="dot"></span>未识别</span>
                  {/if}
                </td>
                <td class="num">{formatSize(f.size)}</td>
                <td class="num text-muted">{formatStamp(f.modTime)}</td>
                <td>
                  <span class="status-badge {f.state}">
                    <span class="dot"></span>
                    {f.state === 'new' ? '待导入' : f.state === 'done' ? '已导入' : '不支持'}
                  </span>
                  {#if f.state === 'done' && f.importedAt}
                    <span class="ml-1.5 text-[11.5px] text-muted">{formatStamp(f.importedAt)}</span>
                  {/if}
                </td>
                <td>
                  {#if f.state === 'new'}
                    <div class="row-actions">
                      <button class="link-btn" onclick={() => openRawPreview(f)} disabled={rawImporting}>
                        预览列映射
                        <Icon name="arrow-right" size={13} />
                      </button>
                      <button class="link-btn" onclick={() => importRaw([f.name])} disabled={rawImporting}>导入</button>
                    </div>
                  {:else if f.state === 'done'}
                    <button class="link-btn" disabled>已写入</button>
                  {:else}
                    <button class="link-btn" disabled title={f.error}>跳过</button>
                  {/if}
                </td>
              </tr>
            {:else}
              {#if !rawLoading}
                <tr>
                  <td colspan="7">
                    <div class="empty-block">
                      <span class="ico"><Icon name="inbox" size={26} /></span>
                      <div class="t">{rawDirExists ? '默认路径下暂无可导入文件' : '默认路径不存在'}</div>
                      <div class="s">
                        {rawDirExists
                          ? '把银行流水 / 订单流水文件放入该目录后点「重新扫描」。'
                          : '点「更改目录…」选择原始数据目录，或把文件放入默认路径后重新扫描。'}
                      </div>
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>

        <div class="scan-actions">
          <label class="chk">
            <input
              type="checkbox"
              checked={archiveAfter}
              onchange={(ev) => (archiveAfter = ev.currentTarget.checked)}
            />
            导入后归档到 archived/（避免重复导入）
          </label>
          <div class="flex-1"></div>
          <span class="text-[12px] text-muted">{selectedCount > 0 ? `已选择 ${selectedCount} 个文件` : '未选择文件'}</span>
          <button
            class="btn"
            disabled={rawImporting || rawLoading || selectedCount !== 1}
            onclick={() => {
              const f = rawFiles.find((x) => isSelected(x.name));
              if (f) openRawPreview(f);
            }}
          >
            预览列映射
          </button>
          <button
            class="btn btn-primary"
            disabled={rawImporting || rawLoading || selectedCount === 0}
            onclick={() => importRaw(selected)}
          >
            导入选中文件
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- 7. 导入批次历史 -->
  <div class="section">
    <div class="section-head">
      <h2>导入批次历史</h2>
      <span class="count">共 {formatCount(batches.length)} 个批次 · 全部展示</span>
    </div>
    <div class="card">
      <table class="data-table">
        <thead>
          <tr>
            <th style="width:118px">类型</th>
            <th>文件名</th>
            <th class="num" style="width:96px">总行数</th>
            <th class="num" style="width:84px">成功</th>
            <th class="num" style="width:84px">跳过</th>
            <th style="width:150px">余额警告</th>
            <th style="width:150px">导入时间</th>
            <th style="width:132px">操作</th>
          </tr>
        </thead>
        <tbody>
          {#each batches as b (b.batchId)}
            <tr>
              <td>
                <span class="tbl-tag {b.sourceType === 'bank_flow' ? 'bank' : ''}">
                  <span class="dot"></span>{TYPE_LABEL[b.sourceType] ?? b.sourceType}
                </span>
              </td>
              <td><span class="file-name">{b.fileName}</span></td>
              <td class="num">{formatCount(b.rowCount)}</td>
              <td class="num">{formatCount(b.successCount)}</td>
              <td class="num {b.skipCount === 0 ? 'text-muted' : ''}">{formatCount(b.skipCount)}</td>
              <td>
                {#if b.balanceWarning}
                  <span class="warn-badge"><Icon name="alert-triangle" size={13} />{b.balanceWarning}</span>
                {:else}
                  <span class="text-muted">—</span>
                {/if}
              </td>
              <td class="num text-muted">{formatStamp(b.importedAt)}</td>
              <td>
                <button class="link-btn" onclick={() => openErrors(b)}>
                  查看错误明细
                  <Icon name="arrow-right" size={13} />
                </button>
              </td>
            </tr>
          {:else}
            <tr>
              <td colspan="8">
                <div class="empty-block">
                  <span class="ico"><Icon name="inbox" size={26} /></span>
                  <div class="t">暂无导入批次</div>
                  <div class="s">选择银行流水或订单流水文件导入后，批次会显示在这里。</div>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      <div class="table-foot">
        <Icon name="lock" size={13} />
        <span>共 {formatCount(batches.length)} 个批次 · 全部展示</span>
        <div class="flex-1"></div>
        <span>批次一次性完整展示，不提供分页</span>
      </div>
    </div>
  </div>
</section>

<!-- 列映射预览弹窗 -->
{#if previewOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="map-overlay" role="presentation" onclick={closePreview}>
    <div
      class="map-modal"
      role="dialog"
      aria-modal="true"
      aria-label="列映射预览"
      tabindex="-1"
      onclick={(ev) => ev.stopPropagation()}
    >
      <div class="map-head">
        <span class="map-title-ico"><Icon name="file" size={16} /></span>
        <h3>列映射预览</h3>
        <div class="flex-1"></div>
        <button class="map-x" type="button" aria-label="关闭" onclick={closePreview}>
          <Icon name="x" size={17} />
        </button>
      </div>
      <div class="map-body">
        {#if previewParsing || !preview}
          <div class="parsing">
            <div class="spinner"></div>
            <div>正在解析列映射…</div>
          </div>
        {:else}
          <div class="meta-grid">
            <div class="meta">
              <div class="k">文件名</div>
              <div class="v"><span class="file-name">{preview.fileName}</span></div>
            </div>
            <div class="meta">
              <div class="k">识别类型</div>
              <div class="v">
                <span class="tbl-tag {preview.sourceType === 'bank_flow' ? 'bank' : ''}">
                  <span class="dot"></span>{TYPE_LABEL[preview.sourceType] ?? preview.sourceType}
                </span>
              </div>
            </div>
            <div class="meta">
              <div class="k">数据行数</div>
              <div class="v">{formatCount(preview.totalRows)} 行</div>
            </div>
          </div>

          <div class="field-block">
            <div class="field-label">已识别关键列 <span class="tally">（{matched.length}）</span></div>
            <div class="chips">
              {#each matched as name (name)}
                <span class="chip ok"><Icon name="check" size={13} />{name}</span>
              {:else}
                <span class="chip err"><Icon name="x" size={13} />未识别到任何关键列</span>
              {/each}
            </div>
          </div>

          <div class="field-block">
            <div class="field-label">缺失列 <span class="tally">（{missing.length}）</span></div>
            {#if missing.length > 0}
              <div class="chips">
                {#each missing as name (name)}
                  <span class="chip err"><Icon name="x" size={13} />{name}</span>
                {/each}
              </div>
              <div class="field-note">
                <span class="mt-px shrink-0"><Icon name="alert-triangle" size={16} /></span>
                <span>缺少关键列 <b>{missing.join('、')}</b>，无法完成字段映射，请检查表头后重新选择文件。</span>
              </div>
            {:else}
              <div class="field-note ok">
                <span class="mt-px shrink-0"><Icon name="circle-check" size={16} /></span>
                <span>关键列完整，字段映射可直接入库。</span>
              </div>
            {/if}
          </div>

          <div class="raw-head">
            <span class="raw-label">原始表头与样本行</span>
            <span class="raw-tag">横向滚动查看全部列</span>
            <span class="raw-tag">仅预览前 {(preview.sampleRows ?? []).length} 行</span>
          </div>
          <div class="raw-scroll">
            <table class="raw-table">
              <thead>
                <tr>
                  {#each preview.header ?? [] as h, i (i)}
                    <th class={isNumericCell((preview.sampleRows?.[0] ?? [])[i] ?? '') ? 'n' : ''}>{h}</th>
                  {/each}
                </tr>
              </thead>
              <tbody>
                {#each preview.sampleRows ?? [] as row, i (i)}
                  <tr>
                    {#each row as c, j (j)}
                      <td class={isNumericCell(c) ? 'n' : ''}>{c}</td>
                    {/each}
                  </tr>
                {:else}
                  <tr>
                    <td colspan={(preview.header ?? []).length} class="px-3 py-6 text-center text-muted">文件无数据行</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
      <div class="map-foot">
        <span class="hint">
          {missing.length > 0
            ? '存在缺失列，无法导入；请检查表头后重新选择文件'
            : emptyFile
              ? '文件无数据行，无法导入；请检查文件内容'
              : '确认后写入本地 SQLite；原始表只读，不支持编辑'}
        </span>
        <div class="flex-1"></div>
        <button class="btn" type="button" onclick={closePreview}>取消</button>
        <button
          class="btn btn-primary"
          type="button"
          disabled={busy || previewParsing || !preview || missing.length > 0 || emptyFile}
          onclick={confirmImport}
        >
          确认导入
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- 错误明细抽屉 -->
{#if drawerOpen && drawerBatch}
  <div class="drawer-scrim" role="presentation" onclick={closeDrawer}></div>
  <div class="drawer" role="dialog" aria-modal="true" aria-label="错误明细">
    <div class="drawer-head">
      <div>
        <h3>错误明细</h3>
        <div class="drawer-sub">
          <span class="tbl-tag {drawerBatch.sourceType === 'bank_flow' ? 'bank' : ''}">
            <span class="dot"></span>{TYPE_LABEL[drawerBatch.sourceType] ?? drawerBatch.sourceType}
          </span>
          <span class="file-name">{drawerBatch.fileName}</span>
          <span>· 导入时间 {formatStamp(drawerBatch.importedAt)}</span>
        </div>
      </div>
      <button class="drawer-x" type="button" aria-label="关闭" onclick={closeDrawer}>
        <Icon name="x" size={17} />
      </button>
    </div>
    <div class="drawer-body">
      <div class="err-summary">
        <div class="err-pill err">
          <div class="k">错误行</div>
          <div class="v">{formatCount(errorCount)}</div>
        </div>
        <div class="err-pill warn">
          <div class="k">警告 / 跳过行</div>
          <div class="v">{formatCount(warnCount || drawerBatch.skipCount)}</div>
        </div>
        <div class="err-pill">
          <div class="k">成功写入</div>
          <div class="v">{formatCount(drawerBatch.successCount)}</div>
        </div>
      </div>

      {#if drawerLoading}
        <div class="parsing">
          <div class="spinner"></div>
          <div>正在读取错误明细…</div>
        </div>
      {:else if errors.length === 0}
        <div class="empty-block">
          <span class="ico"><Icon name="circle-check" size={26} /></span>
          <div class="t">该批次无错误记录</div>
          <div class="s">共 {formatCount(drawerBatch.rowCount)} 行，成功写入 {formatCount(drawerBatch.successCount)} 行。</div>
        </div>
      {:else}
        <div class="err-list">
          {#each errors as e (e.id)}
            <div class="err-row {isWarnError(e) ? 'warn' : ''}">
              <div class="top">
                {#if e.rowNo > 0}<span class="row-no">第 {e.rowNo} 行</span>{/if}
                <span class="field-tag">{fieldLabel(e.fieldName)}</span>
              </div>
              <div class="err-desc">{e.message}</div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}
