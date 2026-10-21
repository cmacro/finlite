<!-- 店面收款（F9）：收款登记 + 通道对账视图。
     数据经 CollectionService.List / Get / Save / Confirm / Reject / Void / GenerateSummary / Recon（Wails 绑定）。
     页面只做展示与表单采集，金额格式化用 lib/format；状态机与金额校验在 Go Service。
     视觉对齐 docs/uidesign/collection.html（stats / filters / sticky table / 抽屉 / 确认弹窗 / Toast）。 -->
<script lang="ts">
  import { CollectionService } from '../../bindings/finlite/internal/services';
  import type {
    AuditLog,
    Collection,
    CollectionAttachment,
    CollectionDetail,
    CollectionFilter,
    CollectionPage,
    CollectionPending,
    CollectionRecon,
    SaveCollectionReq,
  } from '../../bindings/finlite/internal/services/models';
  import Icon from '../lib/Icon.svelte';
  import { formatDateTime, formatMoney } from '../lib/format';
  import type { RouteKey } from '../lib/types';

  interface Props {
    onGoto?: (key: RouteKey) => void;
  }
  let { onGoto }: Props = $props();

  type ToastTone = 'ok' | 'info' | 'warn' | 'err';
  type ViewKey = 'list' | 'recon';

  const OPERATOR = 'local';
  const MAX_ATTACHMENTS = 10;
  const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024;
  const ALLOWED_EXTS = ['jpg', 'jpeg', 'png', 'webp', 'pdf'];
  const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'webp'];
  // 上传前统一压缩：长边 ≤ 2000px、JPEG 质量 0.7。手机原图 3–4MB → 200–400KB，
  // 账套照片年增量从 ~200GB 降到 ~15GB（见业务文档 §8.1）。
  const IMAGE_MAX_EDGE = 2000;
  const IMAGE_QUALITY = 0.7;
  const IMAGE_SKIP_BYTES = 400 * 1024;

  const PAY_METHODS = [
    { value: 'cash', label: '现金' },
    { value: 'wechat', label: '微信个人码' },
    { value: 'alipay', label: '支付宝个人码' },
    { value: 'bank_transfer', label: '对公转账' },
    { value: 'other', label: '其他' },
  ];
  const ACCOUNTS = [
    { value: 'cash', label: '现金账户' },
    { value: 'personal', label: '个人收款账户' },
    { value: 'corporate', label: '对公银行账户' },
  ];
  const STATUS_OPTIONS = [
    { value: 'draft', label: '草稿' },
    { value: 'confirmed', label: '已确认' },
    { value: 'summarized', label: '已汇总' },
    { value: 'voided', label: '已作废' },
  ];
  const METHOD_RECOMMEND: Record<string, string> = {
    cash: 'cash',
    wechat: 'personal',
    alipay: 'personal',
    bank_transfer: 'corporate',
    other: 'cash',
  };
  const STEP_FLOW = [
    { key: 'draft', label: '草稿', meta: '门店经手录入' },
    { key: 'confirmed', label: '已确认', meta: '出纳/会计确认' },
    { key: 'summarized', label: '已汇总', meta: '生成数据汇总' },
  ];

  // ---------- 日期工具 ----------
  function pad(n: number): string {
    return String(n).padStart(2, '0');
  }
  function todayStr(): string {
    const d = new Date();
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  }
  function monthStartStr(): string {
    const d = new Date();
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-01`;
  }
  function monthEndStr(): string {
    const d = new Date();
    const last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(last)}`;
  }
  function periodLabel(from: string, to: string): string {
    if (from && to && from.slice(0, 7) === to.slice(0, 7)) return from.slice(0, 7);
    if (from && to) return `${from} ~ ${to}`;
    return from || to || '全部期间';
  }

  function isZeroAmount(v: string | null | undefined): boolean {
    return /^[+-]?0(?:\.0+)?$/.test(String(v ?? '').trim());
  }

  // ---------- 展示映射（只做格式化，不做业务计算） ----------
  function methodLabel(v: string): string {
    return PAY_METHODS.find((m) => m.value === v)?.label ?? (v || '—');
  }
  function accountLabel(v: string): string {
    return ACCOUNTS.find((a) => a.value === v)?.label ?? (v || '—');
  }
  function statusLabel(v: string): string {
    return STATUS_OPTIONS.find((s) => s.value === v)?.label ?? (v || '—');
  }
  function isImageExt(ext: string): boolean {
    return IMAGE_EXTS.includes((ext || '').toLowerCase());
  }

  /** 读取 Blob 为 data URL（保留原始字节，后端再做 base64 解码）。 */
  function readAsDataURL(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result ?? ''));
      reader.onerror = () => reject(new Error('读取文件失败'));
      reader.readAsDataURL(blob);
    });
  }

  function loadImage(src: string): Promise<HTMLImageElement> {
    return new Promise((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = () => reject(new Error('图片解码失败'));
      img.src = src;
    });
  }

  function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob | null> {
    return new Promise((resolve) => canvas.toBlob((b) => resolve(b), type, quality));
  }

  function replaceExt(name: string, ext: string): string {
    const stem = name.includes('.') ? name.slice(0, name.lastIndexOf('.')) : name;
    return `${stem}.${ext}`;
  }

  interface PreparedUpload {
    name: string;
    ext: string;
    size: number;
    dataBase64: string;
    previewUrl: string;
  }

  /** 图片按需缩放重编码为 JPEG；PDF 与非图片原样上传。压不动（更大）时保留原图。 */
  async function prepareUpload(file: File): Promise<PreparedUpload> {
    const ext = (file.name.split('.').pop() ?? '').toLowerCase();
    if (!isImageExt(ext)) {
      const data = await readAsDataURL(file);
      return { name: file.name, ext, size: file.size, dataBase64: data, previewUrl: data };
    }

    const url = URL.createObjectURL(file);
    try {
      const img = await loadImage(url);
      const maxEdge = Math.max(img.naturalWidth, img.naturalHeight);
      if (maxEdge <= IMAGE_MAX_EDGE && file.size <= IMAGE_SKIP_BYTES) {
        const data = await readAsDataURL(file);
        return { name: file.name, ext, size: file.size, dataBase64: data, previewUrl: data };
      }
      const scale = Math.min(1, IMAGE_MAX_EDGE / Math.max(1, maxEdge));
      const width = Math.max(1, Math.round(img.naturalWidth * scale));
      const height = Math.max(1, Math.round(img.naturalHeight * scale));
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      if (!ctx) throw new Error('当前环境不支持图片压缩');
      ctx.drawImage(img, 0, 0, width, height);
      const blob = await canvasToBlob(canvas, 'image/jpeg', IMAGE_QUALITY);
      if (!blob || blob.size >= file.size) {
        const data = await readAsDataURL(file);
        return { name: file.name, ext, size: file.size, dataBase64: data, previewUrl: data };
      }
      const data = await readAsDataURL(blob);
      // 重编码后内容是 JPEG，扩展名同步改为 jpg，避免 MIME 与内容不一致。
      return { name: replaceExt(file.name, 'jpg'), ext: 'jpg', size: blob.size, dataBase64: data, previewUrl: data };
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  // ---------- Toast ----------
  let toastSeq = 0;
  let toasts = $state<{ id: number; tone: ToastTone; text: string }[]>([]);

  function toast(tone: ToastTone, text: string): void {
    const id = ++toastSeq;
    toasts = [...toasts, { id, tone, text }];
    setTimeout(() => dismissToast(id), 4200);
  }
  function dismissToast(id: number): void {
    toasts = toasts.filter((t) => t.id !== id);
  }

  // ---------- 列表 / 筛选 ----------
  let page = $state<CollectionPage | null>(null);
  let busy = $state(false);
  let loadError = $state('');

  let view = $state<ViewKey>('list');
  let dateFrom = $state(monthStartStr());
  let dateTo = $state(monthEndStr());
  let storeFilter = $state('');
  let methodFilter = $state('');
  let accountFilter = $state('');
  let operatorFilter = $state('');
  let statusFilter = $state('');
  let keyword = $state('');

  const items = $derived<Collection[]>(page?.items ?? []);
  const stats = $derived(page?.stats ?? null);
  const period = $derived(periodLabel(dateFrom, dateTo));
  const filterCount = $derived(page?.total ?? 0);
  const rangeTotal = $derived((stats?.validCount ?? 0) + (stats?.voidCount ?? 0));

  function buildFilter(): CollectionFilter {
    return {
      dateFrom,
      dateTo,
      store: storeFilter,
      payMethod: methodFilter,
      account: accountFilter,
      operator: operatorFilter,
      status: statusFilter,
      keyword: keyword.trim(),
    };
  }

  async function loadList(): Promise<void> {
    busy = true;
    try {
      page = await CollectionService.List(buildFilter());
      loadError = '';
      window.dispatchEvent(
        new CustomEvent<number>('finlite:collection-drafts', {
          detail: page.stats?.draftCount ?? 0,
        })
      );
    } catch (e) {
      page = null;
      loadError = `加载店面收款失败：${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busy = false;
    }
  }

  let recon = $state<CollectionRecon | null>(null);
  let reconLoading = $state(false);

  async function loadRecon(): Promise<void> {
    reconLoading = true;
    try {
      recon = await CollectionService.Recon(buildFilter());
    } catch (e) {
      recon = null;
      toast('err', `加载通道对账失败：${e instanceof Error ? e.message : String(e)}`);
    } finally {
      reconLoading = false;
    }
  }

  async function refreshAll(): Promise<void> {
    await loadList();
    if (view === 'recon') await loadRecon();
  }

  function switchView(v: ViewKey): void {
    view = v;
    if (v === 'recon') void loadRecon();
  }

  let kwTimer: ReturnType<typeof setTimeout> | undefined;
  function onKeywordInput(): void {
    clearTimeout(kwTimer);
    kwTimer = setTimeout(() => void loadList(), 260);
  }

  function doReset(): void {
    dateFrom = monthStartStr();
    dateTo = monthEndStr();
    storeFilter = '';
    methodFilter = '';
    accountFilter = '';
    operatorFilter = '';
    statusFilter = '';
    keyword = '';
    void loadList();
    if (view === 'recon') void loadRecon();
    toast('info', '已重置筛选条件');
  }

  /** 通道明细「查看」：切回收款登记并按通道账户筛选。 */
  function viewChannel(key: string): void {
    accountFilter = key === 'corporate' ? 'corporate' : '';
    statusFilter = '';
    storeFilter = '';
    methodFilter = '';
    operatorFilter = '';
    keyword = '';
    switchView('list');
    void loadList();
  }

  // 顶栏刷新 / 新增 / 批量导入（App.svelte 广播）。
  $effect(() => {
    const onRefresh = () => void refreshAll();
    const onNew = () => openNew();
    const onBatch = () =>
      toast('info', '店面收款批量导入暂未开放，请逐笔登记；银行/订单流水请在「导入中心」导入。');
    window.addEventListener('finlite:refresh', onRefresh);
    window.addEventListener('finlite:collection-new', onNew);
    window.addEventListener('finlite:collection-batch-import', onBatch);
    return () => {
      window.removeEventListener('finlite:refresh', onRefresh);
      window.removeEventListener('finlite:collection-new', onNew);
      window.removeEventListener('finlite:collection-batch-import', onBatch);
    };
  });

  // ---------- 新增 / 编辑抽屉 ----------
  interface EditAttachment {
    id: string;
    name: string;
    ext: string;
    size: number;
    dataBase64: string;
    previewUrl: string;
  }

  let editOpen = $state(false);
  let form = $state<SaveCollectionReq>(blankForm());
  let editAttachments = $state<EditAttachment[]>([]);
  let saving = $state(false);
  let formError = $state('');

  function blankForm(): SaveCollectionReq {
    return {
      id: '',
      date: todayStr(),
      store: '',
      customer: '',
      phone: '',
      project: '',
      payMethod: 'cash',
      account: 'cash',
      amount: '',
      receiptNo: '',
      operator: OPERATOR,
      invoiced: false,
      note: '',
      submit: false,
      attachments: [],
    };
  }

  function openNew(): void {
    closeDetail();
    form = blankForm();
    form.operator = page?.operators?.[0] ?? OPERATOR;
    form.store = storeFilter || page?.stores?.[0] || '';
    form.project = page?.projects?.[0] || '';
    editAttachments = [];
    formError = '';
    editOpen = true;
  }

  async function openEdit(c: Collection): Promise<void> {
    closeDetail();
    if (c.status !== 'draft') {
      toast('warn', `只有草稿可编辑，当前状态：${statusLabel(c.status)}`);
      return;
    }
    try {
      const d = await CollectionService.Get(c.id);
      form = {
        id: d.collection.id,
        date: d.collection.date,
        store: d.collection.store,
        customer: d.collection.customer,
        phone: d.collection.phone,
        project: d.collection.project,
        payMethod: d.collection.payMethod,
        account: d.collection.account,
        amount: d.collection.amount,
        receiptNo: d.collection.receiptNo,
        operator: d.collection.operator,
        invoiced: d.collection.invoiced,
        note: d.collection.note,
        submit: false,
        attachments: [],
      };
      const list = d.attachments ?? [];
      editAttachments = await Promise.all(
        list.map(async (a: CollectionAttachment): Promise<EditAttachment> => {
          let previewUrl = '';
          if (isImageExt(a.ext)) {
            try {
              const data = await CollectionService.GetAttachment(a.id);
              previewUrl = `data:${data.mimeType};base64,${data.dataBase64}`;
            } catch {
              previewUrl = '';
            }
          }
          return {
            id: a.id,
            name: a.fileName,
            ext: a.ext,
            size: a.sizeBytes,
            dataBase64: '',
            previewUrl,
          };
        })
      );
      formError = '';
      editOpen = true;
    } catch (e) {
      toast('err', `读取收款详情失败：${e instanceof Error ? e.message : String(e)}`);
    }
  }

  function onMethodChange(): void {
    form.account = METHOD_RECOMMEND[form.payMethod] ?? 'cash';
  }

  async function onFiles(e: Event): Promise<void> {
    const input = e.currentTarget as HTMLInputElement;
    const files = Array.from(input.files ?? []);
    input.value = '';
    for (const f of files) {
      if (editAttachments.length >= MAX_ATTACHMENTS) {
        toast('err', `单条记录最多 ${MAX_ATTACHMENTS} 张附件`);
        break;
      }
      const ext = (f.name.split('.').pop() ?? '').toLowerCase();
      if (!ALLOWED_EXTS.includes(ext)) {
        toast('err', `「${f.name}」格式不支持（仅 jpg / png / webp / pdf）`);
        continue;
      }
      if (f.size > MAX_ATTACHMENT_BYTES) {
        toast('err', `「${f.name}」超过 10MB，已跳过`);
        continue;
      }
      try {
        const p = await prepareUpload(f);
        if (p.size > MAX_ATTACHMENT_BYTES) {
          toast('err', `「${f.name}」压缩后仍超过 10MB，已跳过`);
          continue;
        }
        editAttachments = [
          ...editAttachments,
          { id: '', name: p.name, ext: p.ext, size: p.size, dataBase64: p.dataBase64, previewUrl: p.previewUrl },
        ];
      } catch (err) {
        toast('err', `「${f.name}」处理失败：${err instanceof Error ? err.message : String(err)}`);
      }
    }
  }

  function removeAttachment(index: number): void {
    const a = editAttachments[index];
    if (!a) return;
    openConfirm({
      title: '删除该附件？',
      message: `删除后需重新上传。确认删除「${a.name}」？`,
      okText: '删除',
      danger: true,
      requireReason: false,
      onOk: () => {
        editAttachments = editAttachments.filter((_, i) => i !== index);
      },
    });
  }

  async function save(submit: boolean): Promise<void> {
    saving = true;
    formError = '';
    try {
      await CollectionService.Save({
        ...form,
        submit,
        attachments: editAttachments.map((a) => ({
          id: a.id,
          fileName: a.name,
          dataBase64: a.id ? '' : a.dataBase64,
        })),
      });
      editOpen = false;
      toast(
        'ok',
        submit
          ? '已提交收款记录，状态：已确认，可继续生成数据汇总。'
          : '已保存草稿，可在列表中继续编辑。'
      );
      await refreshAll();
    } catch (e) {
      formError = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  // ---------- 详情抽屉 ----------
  let detail = $state<CollectionDetail | null>(null);
  let detailLoading = $state(false);
  let detailPreviews = $state<Record<string, string>>({});

  async function loadDetailPreviews(d: CollectionDetail): Promise<void> {
    const map: Record<string, string> = {};
    for (const a of d.attachments ?? []) {
      if (!isImageExt(a.ext)) continue;
      try {
        const data = await CollectionService.GetAttachment(a.id);
        map[a.id] = `data:${data.mimeType};base64,${data.dataBase64}`;
      } catch {
        // 预览失败不阻断详情展示
      }
    }
    detailPreviews = map;
  }

  async function openDetail(c: Collection): Promise<void> {
    await openDetailByID(c.id);
  }

  async function openDetailByID(id: string): Promise<void> {
    detailLoading = true;
    try {
      const d = await CollectionService.Get(id);
      detail = d;
      detailPreviews = {};
      await loadDetailPreviews(d);
    } catch (e) {
      toast('err', `读取收款详情失败：${e instanceof Error ? e.message : String(e)}`);
    } finally {
      detailLoading = false;
    }
  }

  async function reloadDetail(id: string): Promise<void> {
    if (!detail || detail.collection.id !== id) return;
    try {
      const d = await CollectionService.Get(id);
      detail = d;
      await loadDetailPreviews(d);
    } catch {
      detail = null;
    }
  }

  function closeDetail(): void {
    detail = null;
    detailPreviews = {};
  }

  // ---------- 确认弹窗（状态变更必须二次确认） ----------
  interface ConfirmSpec {
    title: string;
    message: string;
    okText: string;
    danger: boolean;
    requireReason: boolean;
    onOk: (reason: string) => Promise<void> | void;
  }

  let confirmSpec = $state<ConfirmSpec | null>(null);
  let confirmReason = $state('');
  let confirmError = $state('');
  let confirmBusy = $state(false);

  function openConfirm(spec: ConfirmSpec): void {
    confirmReason = '';
    confirmError = '';
    confirmSpec = spec;
  }
  function closeConfirm(): void {
    confirmSpec = null;
    confirmReason = '';
    confirmError = '';
  }
  async function runConfirm(): Promise<void> {
    const spec = confirmSpec;
    if (!spec) return;
    const reason = confirmReason.trim();
    if (spec.requireReason && !reason) {
      confirmError = '请填写原因（必填）';
      return;
    }
    confirmBusy = true;
    confirmError = '';
    try {
      await spec.onOk(reason);
      closeConfirm();
    } catch (e) {
      confirmError = e instanceof Error ? e.message : String(e);
    } finally {
      confirmBusy = false;
    }
  }

  function askConfirm(c: Collection): void {
    openConfirm({
      title: '确认收款',
      message: `确认收款 ${c.id}（¥${formatMoney(c.amount)}）吗？确认后记录进入待汇总。`,
      okText: '确认收款',
      danger: false,
      requireReason: false,
      onOk: async () => {
        await CollectionService.Confirm(c.id, OPERATOR);
        toast('ok', '已确认收款，记录进入待汇总。');
        await refreshAll();
        await reloadDetail(c.id);
      },
    });
  }

  function askReject(c: Collection): void {
    openConfirm({
      title: '驳回该收款记录',
      message: '驳回后记录退回草稿，请填写驳回原因。',
      okText: '确认驳回',
      danger: true,
      requireReason: true,
      onOk: async (reason) => {
        await CollectionService.Reject(c.id, reason, OPERATOR);
        toast('warn', '已驳回，等待门店修改。');
        await refreshAll();
        await reloadDetail(c.id);
      },
    });
  }

  function askVoid(c: Collection): void {
    openConfirm({
      title: '作废收款记录',
      message: '作废后该记录不再计入合计，且不可恢复。请填写作废原因。',
      okText: '确认作废',
      danger: true,
      requireReason: true,
      onOk: async (reason) => {
        await CollectionService.Void(c.id, reason, OPERATOR);
        toast('warn', '记录已作废。');
        await refreshAll();
        await reloadDetail(c.id);
      },
    });
  }

  function askSummary(c: Collection): void {
    openConfirm({
      title: '生成数据汇总',
      message: `将按「日期 + 门店」归集：${c.date} · ${c.store}。生成后跳转至「数据汇总」，可继续审核并生成凭证。`,
      okText: '生成并跳转',
      danger: false,
      requireReason: false,
      onOk: async () => {
        const ref = await CollectionService.GenerateSummary(c.id, OPERATOR);
        toast('ok', `已生成数据汇总 ${ref.summaryId.slice(0, 8)}，请在汇总页审核。`);
        await refreshAll();
        onGoto?.('summary');
      },
    });
  }

  // ---------- 详情展示辅助 ----------
  function stepClass(status: string, index: number): string {
    if (status === 'voided') return '';
    const cur = STEP_FLOW.findIndex((s) => s.key === status);
    if (cur < 0) return '';
    if (index < cur) return 'done';
    if (index === cur) return 'current';
    return '';
  }

  function auditText(a: AuditLog): string {
    switch (a.action) {
      case 'create':
        return '提交收款记录';
      case 'submit':
        return '保存并提交收款记录';
      case 'update':
        return '修改收款记录';
      case 'confirm':
        return '确认收款';
      case 'reject':
        return '驳回为草稿';
      case 'void':
        return '作废该记录';
      case 'summary':
        return '生成数据汇总';
      default:
        return a.action || '操作';
    }
  }

  let lightbox = $state<{ src: string; caption: string } | null>(null);
  function showLightbox(src: string, caption: string): void {
    lightbox = { src, caption };
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return;
    if (lightbox) {
      lightbox = null;
      return;
    }
    if (confirmSpec) {
      closeConfirm();
      return;
    }
    if (detail) {
      closeDetail();
      return;
    }
    if (editOpen) editOpen = false;
  }

  function onPendingAction(p: CollectionPending): void {
    void openDetailByID(p.id);
  }

  // 首次加载 + 期间变更后重载（日期区间变化时 stats 与列表一起刷新）。
  loadList();
</script>

<svelte:window onkeydown={onKeydown} />

<section>
  <!-- ============ 视图切换 ============ -->
  <div class="cl-tabs" role="tablist">
    <button
      class="cl-tab {view === 'list' ? 'active' : ''}"
      role="tab"
      aria-selected={view === 'list'}
      onclick={() => switchView('list')}>收款登记</button
    >
    <button
      class="cl-tab {view === 'recon' ? 'active' : ''}"
      role="tab"
      aria-selected={view === 'recon'}
      onclick={() => switchView('recon')}>通道对账视图</button
    >
  </div>

  {#if loadError}
    <div class="cl-banner danger" role="alert">
      <span class="ico"><Icon name="alert-triangle" size={18} /></span>
      <div>
        <div class="bt">加载失败</div>
        <div class="bb">{loadError}</div>
      </div>
    </div>
  {/if}

  {#if view === 'list'}
    <!-- ============ 统计卡 ============ -->
    <div class="cl-stats">
      <div class="cl-stat">
        <div class="cl-stat-label">本期收款合计</div>
        <div class="cl-stat-value">¥{formatMoney(stats?.totalAmount ?? '0.00')}</div>
        <div class="cl-stat-sub">共 <b>{stats?.validCount ?? 0}</b> 笔有效记录 · 期间 {period}</div>
      </div>
      <div class="cl-stat is-warn">
        <div class="cl-stat-label">待审核（草稿）</div>
        <div class="cl-stat-value">{stats?.draftCount ?? 0}<span class="cur">笔</span></div>
        <div class="cl-stat-sub">金额 <b>¥{formatMoney(stats?.draftAmount ?? '0.00')}</b> · 待出纳提交</div>
      </div>
      <div class="cl-stat">
        <div class="cl-stat-label">按到账账户合计</div>
        <div class="cl-mini-rows">
          <div class="cl-mini-row"><span class="cl-dot cl-dot-ok"></span>现金 <b>¥{formatMoney(stats?.cashAmount ?? '0.00')}</b></div>
          <div class="cl-mini-row"><span class="cl-dot cl-dot-accent"></span>个人码 <b>¥{formatMoney(stats?.personalAmount ?? '0.00')}</b></div>
          <div class="cl-mini-row"><span class="cl-dot cl-dot-violet"></span>对公转账 <b>¥{formatMoney(stats?.corporateAmount ?? '0.00')}</b></div>
        </div>
      </div>
      <div class="cl-stat">
        <div class="cl-stat-label">未生成汇总</div>
        <div class="cl-stat-value">{stats?.confirmedCount ?? 0}<span class="cur">笔</span></div>
        <div class="cl-stat-sub">金额 <b>¥{formatMoney(stats?.confirmedAmount ?? '0.00')}</b> · 未纳入数据汇总</div>
      </div>
    </div>

    <!-- ============ 筛选 + 表格 ============ -->
    <div class="cl-panel">
      <div class="cl-panel-head">
        <div class="cl-filters">
          <div class="cl-fin" title="收款日期区间">
            <Icon name="calendar" size={14} />
            <input type="date" aria-label="收款起始日期" bind:value={dateFrom} onchange={() => void loadList()} />
            <span class="sep">~</span>
            <input type="date" aria-label="收款结束日期" bind:value={dateTo} onchange={() => void loadList()} />
          </div>
          <select class="cl-fin" aria-label="门店" bind:value={storeFilter} onchange={() => void loadList()}>
            <option value="">全部门店</option>
            {#each page?.stores ?? [] as s (s)}
              <option value={s}>{s}</option>
            {/each}
          </select>
          <select class="cl-fin" aria-label="收款方式" bind:value={methodFilter} onchange={() => void loadList()}>
            <option value="">全部收款方式</option>
            {#each PAY_METHODS as m (m.value)}
              <option value={m.value}>{m.label}</option>
            {/each}
          </select>
          <select class="cl-fin" aria-label="到账账户" bind:value={accountFilter} onchange={() => void loadList()}>
            <option value="">全部到账账户</option>
            {#each ACCOUNTS as a (a.value)}
              <option value={a.value}>{a.label}</option>
            {/each}
          </select>
          <select class="cl-fin" aria-label="经手人" bind:value={operatorFilter} onchange={() => void loadList()}>
            <option value="">全部经手人</option>
            {#each page?.operators ?? [] as o (o)}
              <option value={o}>{o}</option>
            {/each}
          </select>
          <select class="cl-fin" aria-label="状态" bind:value={statusFilter} onchange={() => void loadList()}>
            <option value="">全部状态</option>
            {#each STATUS_OPTIONS as s (s.value)}
              <option value={s.value}>{s.label}</option>
            {/each}
          </select>
          <div class="cl-fin cl-fin-search">
            <Icon name="search" size={14} />
            <input
              type="text"
              placeholder="客户 / 收据号 / 事由"
              aria-label="关键字"
              bind:value={keyword}
              oninput={onKeywordInput}
            />
          </div>
          <button class="btn" onclick={doReset} disabled={busy}>重置</button>
        </div>
      </div>

      <div class="cl-scroll">
        <table class="cl-table">
          <colgroup>
            <col style="width:96px" /><col style="width:128px" /><col style="width:104px" />
            <col style="width:150px" /><col style="width:112px" /><col style="width:112px" />
            <col style="width:104px" /><col style="width:140px" /><col style="width:80px" />
            <col style="width:64px" /><col style="width:86px" /><col style="width:116px" />
          </colgroup>
          <thead>
            <tr>
              <th>收款日期</th>
              <th>门店</th>
              <th>客户</th>
              <th>项目</th>
              <th>收款方式</th>
              <th>到账账户</th>
              <th class="num">金额</th>
              <th>收据号</th>
              <th>经手人</th>
              <th>附件</th>
              <th>状态</th>
              <th class="col-actions">操作</th>
            </tr>
          </thead>
          <tbody>
            {#each items as c (c.id)}
              <tr
                class="cl-row {c.status === 'voided' ? 'row-void' : ''}"
                tabindex="0"
                onclick={() => void openDetail(c)}
                onkeydown={(e) => {
                  if (e.key === 'Enter') void openDetail(c);
                }}
              >
                <td class="mono">{c.date}</td>
                <td class="store">{c.store}</td>
                <td class="cust">{c.customer}</td>
                <td class="proj">{c.project}</td>
                <td>{methodLabel(c.payMethod)}</td>
                <td class="acct">{accountLabel(c.account)}</td>
                <td class="num mono">
                  {#if c.status === 'voided'}
                    <span class="cl-void-amt">¥{formatMoney(c.amount)}</span>
                  {:else}
                    ¥{formatMoney(c.amount)}
                  {/if}
                </td>
                <td class="mono receipt">{c.receiptNo || '—'}</td>
                <td>{c.operator}</td>
                <td>
                  <span class="cl-attach {c.attachmentCount > 0 ? '' : 'none'}">
                    <Icon name="paperclip" size={14} />
                    {c.attachmentCount}
                  </span>
                </td>
                <td>
                  <span class="cl-badge {c.status}"><span class="t">{statusLabel(c.status)}</span></span>
                </td>
                <td class="col-actions">
                  <div class="cl-row-actions">
                    <button
                      class="cl-link"
                      onclick={(e) => {
                        e.stopPropagation();
                        void openDetail(c);
                      }}>查看</button
                    >
                    {#if c.status === 'draft'}
                      <button
                        class="cl-link strong"
                        onclick={(e) => {
                          e.stopPropagation();
                          askConfirm(c);
                        }}>审核</button
                      >
                    {:else if c.status === 'confirmed'}
                      <button
                        class="cl-link strong"
                        onclick={(e) => {
                          e.stopPropagation();
                          askSummary(c);
                        }}>汇总</button
                      >
                    {/if}
                  </div>
                </td>
              </tr>
            {:else}
              <tr>
                <td colspan="12">
                  <div class="cl-empty">
                    <div class="cl-empty-ico"><Icon name="store" size={26} /></div>
                    <div class="cl-empty-title">没有符合当前筛选条件的收款记录</div>
                    <div class="cl-empty-sub">调整筛选或点击「新增收款」录入店面线下收款。</div>
                    <button class="btn btn-primary" onclick={openNew}>
                      <Icon name="plus" size={15} />新增收款
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <div class="cl-panel-foot">
        <div class="cl-foot-total">
          <span>当前筛选合计</span>
          <b>¥{formatMoney(page?.totalAmount ?? '0.00')}</b>
          <span class="sep">|</span>
          <span>共 <b class="n">{page?.validCount ?? 0}</b> 笔</span>
          <span class="sep">|</span>
          <span class="void-note">
            {(page?.voidCount ?? 0) > 0
              ? `含 ${page?.voidCount} 笔作废（不计入合计）`
              : '无作废记录'}
          </span>
        </div>
        <div class="cl-foot-meta">显示 {filterCount} / {rangeTotal} 条</div>
      </div>
    </div>
  {:else}
    <!-- ============ 通道对账视图 ============ -->
    {#if recon}
      <div class="cl-recon-cards">
        {#each recon.channels ?? [] as ch, i (ch.key)}
          <div class="cl-recon-card">
            <div class="cl-recon-hd">
              <div class="cl-recon-name">
                <span class="cl-dot {i === 0 ? 'cl-dot-ok' : 'cl-dot-violet'}"></span>{ch.name}
              </div>
              <span class="cl-badge {ch.status === 'matched' ? 'confirmed' : 'draft'}">
                <span class="t">{ch.status === 'matched' ? '已平' : '待处理'}</span>
              </span>
            </div>
            <div class="cl-recon-compare">
              <div class="cl-recon-cell">
                <div class="k">登记合计</div>
                <div class="v">¥{formatMoney(ch.registerTotal)}</div>
              </div>
              <div class="cl-recon-cell">
                <div class="k">{i === 0 ? '汇总合计' : '银行到账合计'}</div>
                <div class="v">¥{formatMoney(ch.reconTotal)}</div>
              </div>
              <div class="cl-recon-cell">
                <div class="k">差异</div>
                <div class="v {isZeroAmount(ch.diff) ? 'ok' : 'warn'}">¥{formatMoney(ch.diff)}</div>
              </div>
            </div>
            <div class="cl-attach-hint">{ch.note}</div>
          </div>
        {/each}
      </div>

      <div class="cl-panel">
        <div class="cl-panel-head spread">
          <div class="cl-panel-title">通道核对明细</div>
          <div class="cl-filter-note">数据源：店面收款登记 · 银行流水 · 数据汇总</div>
        </div>
        <div class="cl-scroll recon">
          <table class="cl-table recon">
            <colgroup>
              <col style="width:260px" /><col style="width:140px" /><col style="width:140px" />
              <col style="width:120px" /><col style="width:96px" /><col style="width:96px" />
              <col style="width:120px" />
            </colgroup>
            <thead>
              <tr>
                <th>通道</th>
                <th class="num">登记合计</th>
                <th class="num">对账合计</th>
                <th class="num">差异</th>
                <th class="num">登记笔数</th>
                <th>状态</th>
                <th class="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {#each recon.channels ?? [] as ch (ch.key)}
                <tr>
                  <td class="chan">{ch.name}</td>
                  <td class="num mono">¥{formatMoney(ch.registerTotal)}</td>
                  <td class="num mono">¥{formatMoney(ch.reconTotal)}</td>
                  <td class="num mono {isZeroAmount(ch.diff) ? 'ok-text' : 'warn-text'}">
                    ¥{formatMoney(ch.diff)}
                  </td>
                  <td class="num mono">{ch.registerCount}</td>
                  <td>
                    <span class="cl-badge {ch.status === 'matched' ? 'confirmed' : 'draft'}">
                      <span class="t">{ch.status === 'matched' ? '已平' : '待处理'}</span>
                    </span>
                  </td>
                  <td class="col-actions">
                    <div class="cl-row-actions">
                      <button class="cl-link" onclick={() => viewChannel(ch.key)}>查看明细</button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>

      <div class="cl-panel cl-panel-gap">
        <div class="cl-panel-head spread">
          <div class="cl-panel-title">待处理队列</div>
          <div class="cl-filter-note">{recon.pendingCount} 项待跟进</div>
        </div>
        <div class="cl-pending-list">
          {#each recon.pending ?? [] as p (p.channel + p.id)}
            <div class="cl-pending-item">
              <span class="cl-pd-ic"><Icon name="alert-triangle" size={14} /></span>
              <span class="cl-pd-main">
                <b>{p.title}</b>
                <i>{p.meta}</i>
              </span>
              <span class="cl-pd-amt">{p.amount}</span>
              <button class="cl-link" onclick={() => onPendingAction(p)}>处理</button>
            </div>
          {:else}
            <div class="cl-empty-sm">暂无待处理项，通道账实相符。</div>
          {/each}
        </div>
      </div>
    {:else if reconLoading}
      <div class="cl-empty-sm">正在加载通道对账…</div>
    {:else}
      <div class="cl-empty-sm">暂无通道对账数据，请先登记收款。</div>
    {/if}
  {/if}
</section>

<!-- ============ 新增 / 编辑收款抽屉 ============ -->
{#if editOpen}
  <div class="cl-overlay" role="presentation" onclick={() => (editOpen = false)}></div>
  <div class="cl-drawer" role="dialog" aria-modal="true" aria-label={form.id ? '编辑收款' : '新增收款'}>
    <div class="cl-drawer-head">
      <div>
        <div class="ttl">{form.id ? '编辑收款' : '新增收款'}</div>
        <div class="sub">{form.id || '店面线下收款登记'}</div>
      </div>
      <button class="cl-icon-btn" aria-label="关闭" onclick={() => (editOpen = false)}>
        <Icon name="x" size={17} />
      </button>
    </div>
    <div class="cl-drawer-body">
      {#if formError}
        <div class="cl-form-error"><Icon name="alert-triangle" size={15} /><span>{formError}</span></div>
      {/if}

      <div class="cl-section-title">基础信息</div>
      <div class="cl-form-grid">
        <label class="cl-form-item">
          <span class="lbl">收款日期 <i class="req">*</i></span>
          <input class="cl-input" type="date" bind:value={form.date} />
        </label>
        <label class="cl-form-item">
          <span class="lbl">门店 <i class="req">*</i></span>
          <input class="cl-input" list="cl-store-options" placeholder="选择或填写门店" bind:value={form.store} />
          <datalist id="cl-store-options">
            {#each page?.stores ?? [] as s (s)}<option value={s}></option>{/each}
          </datalist>
        </label>
        <label class="cl-form-item">
          <span class="lbl">客户姓名 <i class="req">*</i></span>
          <input class="cl-input" placeholder="请输入客户姓名" bind:value={form.customer} />
        </label>
        <label class="cl-form-item">
          <span class="lbl">客户电话</span>
          <input class="cl-input" placeholder="如 160****593" bind:value={form.phone} />
        </label>
        <label class="cl-form-item full">
          <span class="lbl">项目 / 事由类别 <i class="req">*</i></span>
          <input class="cl-input" list="cl-project-options" placeholder="选择或填写项目" bind:value={form.project} />
          <datalist id="cl-project-options">
            {#each page?.projects ?? [] as p (p)}<option value={p}></option>{/each}
          </datalist>
        </label>
        <label class="cl-form-item">
          <span class="lbl">收款方式 <i class="req">*</i></span>
          <select class="cl-select" bind:value={form.payMethod} onchange={onMethodChange}>
            {#each PAY_METHODS as m (m.value)}<option value={m.value}>{m.label}</option>{/each}
          </select>
        </label>
        <label class="cl-form-item">
          <span class="lbl">到账账户 <i class="req">*</i> <i class="hint">随收款方式推荐</i></span>
          <select class="cl-select" bind:value={form.account}>
            {#each ACCOUNTS as a (a.value)}<option value={a.value}>{a.label}</option>{/each}
          </select>
        </label>
        <label class="cl-form-item">
          <span class="lbl">收款金额（元） <i class="req">*</i></span>
          <div class="cl-amount">
            <span class="cur">¥</span>
            <input class="cl-input num" inputmode="decimal" placeholder="0.00" bind:value={form.amount} />
          </div>
        </label>
        <label class="cl-form-item">
          <span class="lbl">收据号</span>
          <input class="cl-input" placeholder="SR-20240617-01" bind:value={form.receiptNo} />
        </label>
        <div class="cl-form-item">
          <span class="lbl">是否开票</span>
          <div class="cl-switch-row">
            <label class="cl-switch">
              <input type="checkbox" bind:checked={form.invoiced} />
              <span class="track"></span>
            </label>
            <span class="sw-label">{form.invoiced ? '已开票' : '未开票'}</span>
          </div>
        </div>
        <label class="cl-form-item">
          <span class="lbl">经手人 <i class="req">*</i></span>
          <input class="cl-input" list="cl-operator-options" placeholder="选择或填写经手人" bind:value={form.operator} />
          <datalist id="cl-operator-options">
            {#each page?.operators ?? [] as o (o)}<option value={o}></option>{/each}
          </datalist>
        </label>
        <label class="cl-form-item full">
          <span class="lbl">事由 / 备注</span>
          <textarea class="cl-textarea" placeholder="补充说明，如会员卡号、退款背景等" bind:value={form.note}></textarea>
        </label>
      </div>

      <div class="cl-section-title with-gap">单据照片附件</div>
      <div class="cl-attach-area">
        <div class="cl-attach-grid">
          {#each editAttachments as a, i (a.id || `${a.name}-${i}`)}
            <div class="cl-thumb">
              {#if a.previewUrl && isImageExt(a.ext)}
                <img
                  src={a.previewUrl}
                  alt={a.name}
                  role="presentation"
                  onclick={() => showLightbox(a.previewUrl, a.name)}
                />
              {:else}
                <div class="cl-thumb-file">
                  <Icon name={a.ext === 'pdf' ? 'file' : 'image'} size={20} />
                  <span>{a.name}</span>
                </div>
              {/if}
              <button class="cl-thumb-del" aria-label="删除附件" onclick={() => removeAttachment(i)}>
                <Icon name="trash" size={13} />
              </button>
              <span class="cl-thumb-tag">{(a.ext || 'img').toUpperCase()}</span>
            </div>
          {/each}
          {#if editAttachments.length < MAX_ATTACHMENTS}
            <label class="cl-attach-add">
              <Icon name="plus" size={20} />
              <span>添加照片</span>
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp,application/pdf"
                multiple
                hidden
                onchange={onFiles}
              />
            </label>
          {/if}
        </div>
        <div class="cl-attach-hint">
          支持 jpg / png / webp / pdf，单张 ≤ 10MB，单条记录 ≤ 10 张。照片上传前自动压缩（长边 ≤ 2000px、JPEG 质量 0.7），PDF 不压缩；附件保存在账套 attachments/ 目录（内容寻址，同图只存一份），形成「收款记录 → 附件 → 数据汇总 → 凭证」可追溯链。
        </div>
      </div>
    </div>
    <div class="cl-drawer-foot">
      <button class="btn" onclick={() => (editOpen = false)} disabled={saving}>取消</button>
      <button class="btn" onclick={() => void save(false)} disabled={saving}>保存草稿</button>
      <button class="btn btn-primary" onclick={() => void save(true)} disabled={saving}>保存并提交</button>
    </div>
  </div>
{/if}

<!-- ============ 收款详情抽屉 ============ -->
{#if detail}
  {@const c = detail.collection}
  <div class="cl-overlay" role="presentation" onclick={closeDetail}></div>
  <div class="cl-drawer" role="dialog" aria-modal="true" aria-label="收款详情">
    <div class="cl-drawer-head">
      <div>
        <div class="ttl">收款详情</div>
        <div class="sub">{c.id} · {c.date}</div>
      </div>
      <button class="cl-icon-btn" aria-label="关闭" onclick={closeDetail}>
        <Icon name="x" size={17} />
      </button>
    </div>
    <div class="cl-drawer-body">
      <div class="cl-section-title">状态流转</div>
      <div class="cl-stepper">
        {#each STEP_FLOW as st, i (st.key)}
          <div class="cl-step {stepClass(c.status, i)}">
            <span class="cl-bullet">
              {#if stepClass(c.status, i) === 'done'}
                <Icon name="check" size={12} />
              {:else}
                {i + 1}
              {/if}
            </span>
            <span class="cl-step-txt"><b>{st.label}</b><i>{st.meta}</i></span>
            <span class="cl-bar"></span>
          </div>
        {/each}
      </div>
      {#if c.status === 'voided'}
        <div class="cl-void-strip">
          <Icon name="alert-triangle" size={16} />
          <div><b>该记录已作废</b> · 原因：{c.voidReason || '原因已记录'}</div>
        </div>
      {/if}

      <div class="cl-section-title with-gap">收款信息</div>
      <div class="cl-info-list">
        <div class="cl-info-row"><span class="k">收款日期</span><span class="v mono">{c.date}</span></div>
        <div class="cl-info-row"><span class="k">门店</span><span class="v">{c.store}</span></div>
        <div class="cl-info-row"><span class="k">客户姓名</span><span class="v">{c.customer}</span></div>
        <div class="cl-info-row"><span class="k">客户电话</span><span class="v mono">{c.phone || '—'}</span></div>
        <div class="cl-info-row"><span class="k">项目</span><span class="v">{c.project}</span></div>
        <div class="cl-info-row"><span class="k">收款方式</span><span class="v">{methodLabel(c.payMethod)}</span></div>
        <div class="cl-info-row"><span class="k">到账账户</span><span class="v">{accountLabel(c.account)}</span></div>
        <div class="cl-info-row"><span class="k">收据号</span><span class="v mono">{c.receiptNo || '—'}</span></div>
        <div class="cl-info-row"><span class="k">经手人</span><span class="v">{c.operator}</span></div>
        <div class="cl-info-row"><span class="k">是否开票</span><span class="v">{c.invoiced ? '已开票' : '未开票'}</span></div>
        <div class="cl-info-row full">
          <span class="k">收款金额</span>
          <span class="v big">¥{formatMoney(c.amount)}</span>
        </div>
        <div class="cl-info-row full">
          <span class="k">事由 / 备注</span>
          <span class="v note">{c.note || '—'}</span>
        </div>
        {#if c.summaryId}
          <div class="cl-info-row full">
            <span class="k">关联数据汇总</span>
            <span class="v mono">{c.summaryId.slice(0, 8)} · {detail.summaryStatus || '—'}</span>
          </div>
        {/if}
      </div>

      <div class="cl-section-title with-gap">单据照片附件（{detail.attachments?.length ?? 0}）</div>
      <div class="cl-attach-grid">
        {#each detail.attachments ?? [] as a (a.id)}
          <div class="cl-thumb">
            {#if isImageExt(a.ext) && detailPreviews[a.id]}
              <img
                src={detailPreviews[a.id]}
                alt={a.fileName}
                role="presentation"
                onclick={() => showLightbox(detailPreviews[a.id], `${c.customer} · ${a.fileName}`)}
              />
            {:else}
              <div class="cl-thumb-file">
                <Icon name={a.ext === 'pdf' ? 'file' : 'image'} size={20} />
                <span>{a.fileName}</span>
              </div>
            {/if}
            <span class="cl-thumb-tag">{(a.ext || 'img').toUpperCase()}</span>
          </div>
        {:else}
          <div class="cl-attach-hint">该记录暂无附件，可在编辑中补充单据照片。</div>
        {/each}
      </div>

      <div class="cl-section-title with-gap">操作记录</div>
      <div class="cl-audit">
        {#each detail.auditLogs ?? [] as log (log.id)}
          <div class="cl-audit-item">
            <span class="when">{formatDateTime(log.operatedAt)}</span>
            <span class="what"><b>{auditText(log)}</b> · {log.operator || OPERATOR}</span>
          </div>
        {:else}
          <div class="cl-empty-sm">暂无操作记录。</div>
        {/each}
      </div>
    </div>
    <div class="cl-drawer-foot">
      <button class="btn btn-ghost left" onclick={closeDetail}>关闭</button>
      {#if c.status === 'draft'}
        <button class="btn btn-danger" onclick={() => askVoid(c)} disabled={detailLoading}>作废</button>
        <button class="btn" onclick={() => void openEdit(c)} disabled={detailLoading}>编辑</button>
        <button class="btn btn-primary" onclick={() => askConfirm(c)} disabled={detailLoading}>确认收款</button>
      {:else if c.status === 'confirmed'}
        <button class="btn btn-danger" onclick={() => askVoid(c)}>作废</button>
        <button class="btn" onclick={() => askReject(c)}>驳回</button>
        <button class="btn btn-primary" onclick={() => askSummary(c)}>生成汇总</button>
      {:else if c.status === 'summarized'}
        <button class="btn btn-primary" onclick={() => onGoto?.('summary')}>查看数据汇总</button>
      {:else}
        <span class="cl-foot-note">该记录已作废，不可再操作</span>
      {/if}
    </div>
  </div>
{/if}

<!-- ============ 确认弹窗（含原因输入） ============ -->
{#if confirmSpec}
  <div
    class="cl-modal-mask"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) closeConfirm();
    }}
  >
    <div class="cl-modal" role="alertdialog" aria-modal="true" aria-label={confirmSpec.title} tabindex="-1">
      <h3 class:danger={confirmSpec.danger}>
        <Icon name={confirmSpec.danger ? 'alert-triangle' : 'info'} size={19} />
        {confirmSpec.title}
      </h3>
      <p>{confirmSpec.message}</p>
      {#if confirmSpec.requireReason}
        <textarea
          class="cl-textarea"
          placeholder="请填写原因（必填）"
          aria-label="原因"
          bind:value={confirmReason}
        ></textarea>
      {/if}
      {#if confirmError}
        <div class="cl-form-error"><Icon name="alert-triangle" size={15} /><span>{confirmError}</span></div>
      {/if}
      <div class="cl-modal-actions">
        <button class="btn" onclick={closeConfirm} disabled={confirmBusy}>取消</button>
        <button
          class="btn {confirmSpec.danger ? 'btn-danger' : 'btn-primary'}"
          onclick={() => void runConfirm()}
          disabled={confirmBusy}>{confirmSpec.okText}</button
        >
      </div>
    </div>
  </div>
{/if}

<!-- ============ 附件灯箱 ============ -->
{#if lightbox}
  <div
    class="cl-lightbox"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) lightbox = null;
    }}
  >
    <button class="cl-lb-close" aria-label="关闭预览" onclick={() => (lightbox = null)}>
      <Icon name="x" size={19} />
    </button>
    <img src={lightbox.src} alt={lightbox.caption} />
    <div class="cl-lb-cap">{lightbox.caption}</div>
  </div>
{/if}

<!-- ============ Toast ============ -->
<div class="cl-toasts">
  {#each toasts as t (t.id)}
    <div class="cl-toast {t.tone}">
      <Icon name={t.tone === 'err' || t.tone === 'warn' ? 'alert-triangle' : 'circle-check'} size={16} />
      <span>{t.text}</span>
      <button class="cl-toast-close" aria-label="关闭" onclick={() => dismissToast(t.id)}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/each}
</div>
