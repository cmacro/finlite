<!-- 订单流水（F3）：只读提示条 + 统计卡 + 筛选条 + 按结算日期（T+1）归集的密集表格
     （行内展开手续费复算、行详情抽屉）；原始表只读，页面无编辑/删除入口。
     数据经 OrderFlowService.List（Wails 绑定）：一次性返回全部明细，费率复算与合计均由 Go decimal 完成。
     视觉对齐 docs/uidesign/order-flow.html；金额只格式化，不做业务计算。 -->
<script lang="ts">
  import { OrderFlowService } from '../../bindings/finlite/internal/services';
  import type {
    OrderFlow,
    OrderFlowFilter,
    OrderFlowGroup,
    OrderFlowPage,
  } from '../../bindings/finlite/internal/services/models';
  import Icon from '../lib/Icon.svelte';
  import {
    feeFlagText,
    formatDateTime,
    formatMoney,
    formatSignedMoney,
    prettyJson,
    weekdayCN,
  } from '../lib/format';
  import type { RouteKey } from '../lib/types';

  interface Props {
    onGoto?: (key: RouteKey) => void;
  }
  let { onGoto }: Props = $props();

  type ToastTone = 'info' | 'ok' | 'warn' | 'danger';

  let page = $state<OrderFlowPage | null>(null);

  // 输入框与「已生效筛选」分离：日期变更/回车/查询按钮才应用（对齐原型交互）。
  let settleInput = $state('');
  let kwInput = $state('');
  let onlyFeeAdj = $state(false);
  let filter = $state<OrderFlowFilter>({ settleDate: '', keyword: '', onlyFeeAdj: '' });

  let busy = $state(false);
  let loadError = $state('');
  let expanded = $state<Record<string, boolean>>({});
  let drawerRow = $state<OrderFlow | null>(null);

  const groups = $derived<OrderFlowGroup[]>(page?.groups ?? []);

  const stats = $derived({
    total: page?.total ?? 0,
    orderTotal: page?.orderTotal ?? '0.00',
    feeTotal: page?.feeTotal ?? '0.00',
    netTotal: page?.netTotal ?? '0.00',
    reconDiff: page?.reconDiff ?? '0.00',
    feeDiff: page?.feeDiffCount ?? 0,
    netDiff: page?.netDiffCount ?? 0,
    parseErr: page?.parseErrCount ?? 0,
    anomaly: page?.anomalyCount ?? 0,
  });

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

  // ---------- 数据加载 ----------
  async function load(): Promise<void> {
    busy = true;
    try {
      page = await OrderFlowService.List(filter);
      loadError = '';
    } catch (e) {
      page = null;
      loadError = `加载订单流水失败：${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busy = false;
    }
  }

  async function doQuery(): Promise<void> {
    filter = {
      settleDate: settleInput,
      keyword: kwInput.trim(),
      onlyFeeAdj: onlyFeeAdj ? '1' : '',
    };
    expanded = {};
    await load();
    if (!loadError) toast('info', `查询完成，命中 ${stats.total} 条订单明细。`);
  }

  function doReset(): void {
    settleInput = '';
    kwInput = '';
    onlyFeeAdj = false;
    filter = { settleDate: '', keyword: '', onlyFeeAdj: '' };
    expanded = {};
    void load();
    toast('info', '筛选条件已重置，展示全部订单流水。');
  }

  // 顶栏刷新按钮（App.svelte 广播）：重拉订单流水。
  $effect(() => {
    const onRefresh = () => void load();
    window.addEventListener('finlite:refresh', onRefresh);
    return () => window.removeEventListener('finlite:refresh', onRefresh);
  });

  // ---------- 行交互 ----------
  function toggleExpand(id: string): void {
    expanded = { ...expanded, [id]: !expanded[id] };
  }
  function isExpanded(id: string): boolean {
    return !!expanded[id];
  }
  function openDrawer(row: OrderFlow): void {
    drawerRow = row;
  }
  function closeDrawer(): void {
    drawerRow = null;
  }
  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && drawerRow) closeDrawer();
  }

  // ---------- 展示映射（只做格式化，不做算术） ----------
  type Tone = '' | 'warn' | 'err';

  function rowTone(flag: string): Tone {
    if (flag === 'fee_diff') return 'warn';
    if (flag === 'net_diff' || flag === 'fee_parse_err') return 'err';
    return '';
  }

  /** 金额是否为 0（字符串判断，避免前端做算术）。 */
  function isZeroAmount(v: string | null | undefined): boolean {
    return /^[+-]?0(?:\.0+)?$/.test(String(v ?? '').trim());
  }

  function payTone(pay: string): string {
    const p = (pay || '').toLowerCase();
    if (pay.includes('微信') || p.includes('wechat')) return 'of-pay-wechat';
    if (pay.includes('支付宝') || p.includes('alipay')) return 'of-pay-alipay';
    return '';
  }

  function posBatchNo(settleDate: string): string {
    return `POS-${settleDate.replace(/-/g, '')}`;
  }

  function statusMessage(o: OrderFlow): string {
    switch (o.feeFlag) {
      case 'fee_diff':
        return `实际手续费与按 ${o.feeRate} 复算的预期手续费不符，请核对商户费率或导出字段。`;
      case 'net_diff':
        return '订单金额 − 手续费 与记录的商户实收不一致，请人工复核。';
      case 'fee_parse_err':
        return '手续费字段无法解析，费率复算已跳过，请核对源文件。';
      default:
        return '费率与实收恒等式校验通过，无需人工干预。';
    }
  }

  load();
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet feeBadge(o: OrderFlow)}
  {#if o.feeFlag === 'fee_diff'}
    <span class="of-badge of-badge-warn">{feeFlagText(o.feeFlag)}</span>
  {:else if o.feeFlag === 'net_diff' || o.feeFlag === 'fee_parse_err'}
    <span class="of-badge of-badge-err">{feeFlagText(o.feeFlag)}</span>
  {:else}
    <span class="of-rate-ok">费率正确</span>
  {/if}
{/snippet}

<section>
  <!-- ============ 只读提示条 ============ -->
  {#if loadError}
    <div class="of-banner danger" role="alert">
      <span class="ib-ico"><Icon name="alert-triangle" size={18} /></span>
      <div>
        <div class="ib-title">加载失败</div>
        <div class="ib-body">{loadError}</div>
      </div>
    </div>
  {:else}
    <div class="of-banner" role="note">
      <span class="ib-ico"><Icon name="info" size={18} /></span>
      <div>
        <div class="ib-title">收入归属规则：按结算日期（T+1）入账，非交易时间</div>
        <div class="ib-body">
          本表为工行商户后台导出的订单原始明细，交易级只读数据，页面不提供编辑 / 删除。
          金额恒等式 <code>订单金额 − 手续费 = 实收</code>，允许 <b>±0.01</b> 元舍入误差；超出阈值即标记为差异行，需人工复核。
        </div>
      </div>
    </div>
  {/if}

  <!-- ============ 统计卡 ============ -->
  <section class="of-stats">
    <div class="of-stat">
      <div class="of-stat-label">订单笔数</div>
      <div class="of-stat-value">{stats.total}<span class="cur">笔</span></div>
      <div class="of-stat-sub">全部展示 · 未分页</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">订单金额合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.orderTotal)}</div>
      <div class="of-stat-sub">{stats.total} 笔命中 · 全部展示</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">手续费合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.feeTotal)}</div>
      <div class="of-stat-sub">微信 / 支付宝 0.25% · 借记卡 0.5% · 贷记卡 0.6%</div>
    </div>
    <div class="of-stat">
      <div class="of-stat-label">商户实收合计</div>
      <div class="of-stat-value"><span class="cur">¥</span>{formatMoney(stats.netTotal)}</div>
      <div class="of-stat-sub">订单金额 − 手续费</div>
    </div>
    <div class="of-stat warn">
      <div class="of-stat-label">费率异常笔数</div>
      <div class="of-stat-value">{stats.anomaly}<span class="cur">笔</span></div>
      <div class="of-stat-sub">
        费率差异 {stats.feeDiff} · 实收差异 {stats.netDiff} · 解析失败 {stats.parseErr}
      </div>
    </div>
  </section>

  <!-- ============ 筛选条 ============ -->
  <section class="of-filters">
    <div class="of-field">
      <span class="fl">结算日期</span>
      <input
        type="date"
        aria-label="结算日期"
        bind:value={settleInput}
        onchange={() => void doQuery()}
      />
    </div>
    <div class="of-sep"></div>
    <div class="of-field">
      <span class="fl">关键字</span>
      <input
        type="text"
        placeholder="工行订单号 / 第三方订单号"
        aria-label="关键字"
        bind:value={kwInput}
        onkeydown={(e) => {
          if (e.key === 'Enter') void doQuery();
        }}
      />
    </div>
    <div class="of-sep"></div>
    <label class="of-switch">
      <input
        type="checkbox"
        bind:checked={onlyFeeAdj}
        onchange={() => void doQuery()}
      />
      <span class="track"><span class="thumb"></span></span>
      <span class="sw-label">仅看费率差异</span>
    </label>
    <button class="btn btn-primary" onclick={() => void doQuery()} disabled={busy}>
      <Icon name="search" size={15} />
      查询
    </button>
    <button class="btn" onclick={doReset} disabled={busy}>重置</button>
    <div class="of-spacer"></div>
    <div class="of-count">共 <b>{stats.total}</b> 条 · 全部展示</div>
  </section>

  <!-- ============ 表格卡片 ============ -->
  <section class="of-table-card">
    {#if groups.length === 0}
      <div class="of-empty">
        <div class="of-empty-ico"><Icon name="receipt" size={26} /></div>
        <div class="of-empty-title">暂无数据，请先在导入中心导入订单流水</div>
        <div class="of-empty-sub">
          订单流水来自工行商户后台导出的 TSV 文件，导入后按结算日期（T+1）自动归集与勾稽。
        </div>
        <button class="btn btn-primary" onclick={() => onGoto?.('import')}>
          <Icon name="upload" size={15} />
          前往导入中心
        </button>
      </div>
    {:else}
      <div class="of-scroll">
        <table class="of-table">
          <colgroup>
            <col style="width:40px" /><col style="width:100px" /><col style="width:136px" />
            <col style="width:146px" /><col style="width:102px" /><col style="width:104px" />
            <col style="width:90px" /><col style="width:104px" /><col style="width:82px" />
            <col style="width:74px" /><col style="width:126px" /><col style="width:78px" />
          </colgroup>
          <thead>
            <tr>
              <th aria-label="展开"></th>
              <th>工行订单号</th>
              <th>第三方订单号</th>
              <th>交易时间</th>
              <th>结算日期</th>
              <th class="num">订单金额</th>
              <th class="num">手续费</th>
              <th class="num">实收</th>
              <th>支付方式</th>
              <th>借贷记</th>
              <th>费率检查</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {#each groups as g (g.settleDate)}
              <tr class="of-group-row">
                <td colspan="12">
                  <div class="of-group-inner">
                    <div class="of-group-date">
                      <span class="gd-k">结算日期</span>
                      <span class="gd-v">{g.settleDate}</span>
                      <span class="gd-wd">· {weekdayCN(g.settleDate)}</span>
                      <span class="gd-n">· {g.count} 笔</span>
                    </div>
                    <div class="of-group-pos">
                      银行 POS 到账（次日凌晨）
                      {#if g.anomalyCount > 0}
                        <span class="of-badge of-badge-warn">待勾稽</span>
                      {:else}
                        <span class="of-badge of-badge-ok">已匹配</span>
                      {/if}
                    </div>
                    <div class="of-group-spacer"></div>
                    <div class="of-group-subs">
                      <span class="of-group-sub">订单金额 <b>¥{formatMoney(g.orderTotal)}</b></span>
                      <span class="of-group-sub">手续费 <b>¥{formatMoney(g.feeTotal)}</b></span>
                      <span class="of-group-sub">实收 <b>¥{formatMoney(g.netTotal)}</b></span>
                      {#if g.parseErrCount > 0}
                        <span class="of-group-note">· {g.parseErrCount} 笔解析失败未计入手续费小计</span>
                      {/if}
                    </div>
                  </div>
                </td>
              </tr>
              {#each g.items ?? [] as o (o.id)}
                {@const tone = rowTone(o.feeFlag)}
                <tr
                  class="of-row {tone === 'warn' ? 'row-warn' : tone === 'err' ? 'row-err' : ''}"
                  tabindex="0"
                  onclick={() => openDrawer(o)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') openDrawer(o);
                  }}
                >
                  <td>
                    <button
                      class="of-expand {isExpanded(o.id) ? 'open' : ''}"
                      aria-label="展开手续费复算"
                      aria-expanded={isExpanded(o.id) ? 'true' : 'false'}
                      onclick={(e) => {
                        e.stopPropagation();
                        toggleExpand(o.id);
                      }}
                    >
                      <Icon name="chevron-right" size={13} />
                    </button>
                  </td>
                  <td class="mono" title={o.icbcOrderNo}>{o.icbcOrderNo}</td>
                  <td class="mono" title={o.thirdOrderNo}>
                    {#if o.thirdOrderNo}
                      {o.thirdOrderNo}
                    {:else}
                      <span class="of-none">—</span>
                    {/if}
                  </td>
                  <td class="tnum">{o.txnTime}</td>
                  <td class="tnum">{o.settleDate}</td>
                  <td class="num">¥{formatMoney(o.orderAmount)}</td>
                  <td class="num">
                    {#if o.feeFlag === 'fee_parse_err'}
                      <span class="of-fee-fail">解析失败</span>
                    {:else}
                      ¥{formatMoney(o.feeAmount)}
                    {/if}
                  </td>
                  <td class="num">¥{formatMoney(o.netAmount)}</td>
                  <td>
                    <span class="of-pay {payTone(o.payMethod)}">
                      <span class="dot"></span>{o.payMethod || '—'}
                    </span>
                  </td>
                  <td><span class="of-card">{o.debitCreditFlag || '—'}</span></td>
                  <td>{@render feeBadge(o)}</td>
                  <td>
                    <button
                      class="link-btn"
                      onclick={(e) => {
                        e.stopPropagation();
                        openDrawer(o);
                      }}>查看详情</button
                    >
                  </td>
                </tr>
                {#if isExpanded(o.id)}
                  <tr class="of-detail-row">
                    <td colspan="12">
                      <div class="of-recalc">
                        <div class="of-recalc-head">
                          <span class="ico"><Icon name="list" size={13} /></span>
                          手续费复算 · {o.icbcOrderNo}
                        </div>
                        {#if o.feeFlag === 'fee_parse_err'}
                          <div class="of-parse-note">
                            <Icon name="alert-triangle" size={14} />
                            手续费字段无法解析（源文件原始值缺失或格式异常），已跳过费率复算，标记为「金额解析失败」待人工复核。
                          </div>
                        {:else}
                          <div class="of-recalc-line">
                            <div class="of-rc">
                              <span class="rk">订单金额 × 费率</span>
                              <span class="rv">¥{formatMoney(o.orderAmount)} × {o.feeRate}</span>
                            </div>
                            <span class="of-rc-op">＝</span>
                            <div class="of-rc">
                              <span class="rk">预期手续费</span>
                              <span class="rv">¥{formatMoney(o.expectedFee)}</span>
                            </div>
                            <div class="of-rc">
                              <span class="rk">实际手续费</span>
                              <span class="rv">¥{formatMoney(o.feeAmount)}</span>
                            </div>
                            <div class="of-rc">
                              <span class="rk">差额</span>
                              <span
                                class="rv {isZeroAmount(o.feeDiff)
                                  ? 'ok'
                                  : o.feeFlag === 'fee_diff'
                                    ? 'warn'
                                    : 'err'}"
                              >
                                {formatSignedMoney(o.feeDiff)}
                              </span>
                            </div>
                          </div>
                          <div class="of-identity">
                            <span class="id-k">恒等式</span>
                            <span class="id-eq">
                              订单金额 − 手续费 = 应到实收 → ¥{formatMoney(o.orderAmount)} − ¥{formatMoney(
                                o.feeAmount
                              )} = ¥{formatMoney(o.expectedNet)}
                            </span>
                            <span class="id-diff {isZeroAmount(o.netDiff) ? 'ok' : 'err'}">
                              实收差异 {formatSignedMoney(o.netDiff)}
                            </span>
                          </div>
                        {/if}
                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
            {/each}
          </tbody>
        </table>
      </div>

      <!-- ============ 表尾合计 ============ -->
      <div class="of-summary">
        <div class="of-summary-left">当前筛选合计 · 共 <b>{stats.total}</b> 条 · 全部展示</div>
        <div class="of-summary-metrics">
          <div class="of-sm"><span class="sm-k">订单金额</span><span class="sm-v">¥{formatMoney(stats.orderTotal)}</span></div>
          <div class="of-sm"><span class="sm-k">手续费</span><span class="sm-v">¥{formatMoney(stats.feeTotal)}</span></div>
          <div class="of-sm"><span class="sm-k">商户实收</span><span class="sm-v">¥{formatMoney(stats.netTotal)}</span></div>
          {#if stats.total > 0 && !isZeroAmount(stats.reconDiff)}
            <span class="of-sm-chip">勾稽差额 {formatSignedMoney(stats.reconDiff)}</span>
          {/if}
        </div>
      </div>
    {/if}
  </section>
</section>

<!-- ============ 行详情抽屉 ============ -->
{#if drawerRow}
  {@const o = drawerRow}
  {@const tone = rowTone(o.feeFlag)}
  <div class="of-drawer-overlay" role="presentation" onclick={closeDrawer}></div>
  <div class="of-drawer" role="dialog" aria-modal="true" aria-label="订单详情" tabindex="-1">
    <div class="of-drawer-head">
      <div>
        <div class="dh-title">订单详情</div>
        <div class="dh-sub">{o.icbcOrderNo}</div>
      </div>
      <button class="of-drawer-close" aria-label="关闭" onclick={closeDrawer}>
        <Icon name="x" size={16} />
      </button>
    </div>
    <div class="of-drawer-body">
      <div class="of-dstatus {tone === 'warn' ? 'warn' : tone === 'err' ? 'err' : 'ok'}">
        <span class="ico">
          <Icon name={tone === '' ? 'circle-check' : 'alert-triangle'} size={15} />
        </span>
        <span>
          <b>{feeFlagText(o.feeFlag) || '费率正确'}</b> · {statusMessage(o)}
        </span>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">订单标识</div>
        <div class="of-dgrid">
          <div class="of-dfield wide"><span class="dk">工行订单号</span><span class="dv mono">{o.icbcOrderNo}</span></div>
          <div class="of-dfield wide">
            <span class="dk">第三方订单号</span>
            <span class="dv mono {o.thirdOrderNo ? '' : 'empty-v'}">{o.thirdOrderNo || '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">支付方式</span><span class="dv">{o.payMethod || '—'}</span></div>
          <div class="of-dfield"><span class="dk">借贷记</span><span class="dv">{o.debitCreditFlag || '—'}</span></div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">金额与费率</div>
        <div class="of-dgrid">
          <div class="of-dfield"><span class="dk">订单金额</span><span class="dv num">¥{formatMoney(o.orderAmount)}</span></div>
          <div class="of-dfield">
            <span class="dk">手续费</span>
            <span class="dv num {o.feeFlag === 'fee_parse_err' ? 'err' : ''}">
              {o.feeFlag === 'fee_parse_err' ? '解析失败' : `¥${formatMoney(o.feeAmount)}`}
            </span>
          </div>
          <div class="of-dfield"><span class="dk">商户实收</span><span class="dv num">¥{formatMoney(o.netAmount)}</span></div>
          <div class="of-dfield">
            <span class="dk">预期手续费（{o.feeRate}）</span>
            <span class="dv num">{o.feeFlag === 'fee_parse_err' ? '—' : `¥${formatMoney(o.expectedFee)}`}</span>
          </div>
          <div class="of-dfield"><span class="dk">费率检查</span><span class="dv">{feeFlagText(o.feeFlag) || '费率正确'}</span></div>
          <div class="of-dfield">
            <span class="dk">复核状态</span>
            <span class="dv">{o.feeFlag ? '待人工复核' : '无需复核'}</span>
          </div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">时间与收入归属（T+1）</div>
        <div class="of-attr">
          <b>收入归属日 = 结算日期，非交易时间。</b>工行商户订单按 T+1 结算，本笔交易于交易时间发生，次一自然日入账。
          <div class="of-attr-flow">
            <span class="of-af-node">交易时间 {o.txnTime}</span>
            <span class="of-af-arrow">→</span>
            <span class="of-af-node hl">结算日期 {o.settleDate}（收入归属日）</span>
          </div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">数据来源</div>
        <div class="of-dgrid">
          <div class="of-dfield wide">
            <span class="dk">来源文件</span>
            <span class="dv {o.fileName ? '' : 'empty-v'}">{o.fileName || '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">导入批次</span><span class="dv mono">{o.batchId}</span></div>
          <div class="of-dfield"><span class="dk">导入时间</span><span class="dv">{formatDateTime(o.importedAt)}</span></div>
          <div class="of-dfield">
            <span class="dk">原始行号</span>
            <span class="dv {o.sourceRowNo > 0 ? '' : 'empty-v'}">{o.sourceRowNo > 0 ? `第 ${o.sourceRowNo} 行` : '—'}</span>
          </div>
          <div class="of-dfield"><span class="dk">数据性质</span><span class="dv">原始数据 · 只读</span></div>
        </div>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">原始 JSON 摘要</div>
        <pre class="of-json">{prettyJson(o.rawJson)}</pre>
      </div>

      <div class="of-dsec">
        <div class="of-dsec-title">关联勾稽</div>
        <div class="of-recon-list">
          <div class="of-recon-item">
            <span class="ri-ic"><Icon name="wallet" size={14} /></span>
            <div class="of-recon-main">
              <div class="of-recon-k">银行 POS 到账（次日凌晨）</div>
              <div class="of-recon-v">{o.settleDate} · 结算批次 {posBatchNo(o.settleDate)}</div>
            </div>
            {#if o.feeFlag}
              <span class="of-badge of-badge-warn">待勾稽</span>
            {:else}
              <span class="of-badge of-badge-ok">已匹配</span>
            {/if}
          </div>
          <div class="of-recon-item">
            <span class="ri-ic"><Icon name="bar-chart" size={14} /></span>
            <div class="of-recon-main">
              <div class="of-recon-k">数据汇总 · 店面收款归集</div>
              <div class="of-recon-v">按结算日期并入 {o.settleDate} 日汇总</div>
            </div>
            <span class="of-badge of-badge-ok">已归集</span>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

<!-- ============ Toast ============ -->
<div class="of-toasts">
  {#each toasts as t (t.id)}
    <div class="of-toast {t.tone}">
      <span class="of-toast-bar"></span>
      <div class="of-toast-body">{t.text}</div>
      <button class="of-toast-close" aria-label="关闭" onclick={() => dismissToast(t.id)}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/each}
</div>
