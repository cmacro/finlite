<!-- finlite 主框架：左侧导航 + 8 个业务页面（Svelte 5 runes）。
页面数据一律经 Wails Service 绑定获取（bindings 生成后接线），本文件只做路由切换。
布局样式为 Tailwind utilities（token 见 styles/tokens.css），无 <style> 块。 -->
<script lang="ts">
  import type { RouteKey } from './lib/types';
  import Icon, { type IconName } from './lib/Icon.svelte';
  import ImportPage from './routes/Import.svelte';
  import BankFlowPage from './routes/BankFlow.svelte';
  import OrderFlowPage from './routes/OrderFlow.svelte';
  import ReconPage from './routes/Recon.svelte';
  import SummaryPage from './routes/Summary.svelte';
  import VoucherPage from './routes/Voucher.svelte';
  import BalancePage from './routes/Balance.svelte';
  import SettingsPage from './routes/Settings.svelte';

  // 导航分组与顺序对齐 docs/uidesign 的 canonical sidebar。
  // 顶栏左侧标题统一为「分类 | 模块名称」，分类取当前页所属导航分组。
  const navGroups: {
    label: string;
    items: { key: RouteKey; label: string; icon: IconName }[];
  }[] = [
    { label: '数据接入', items: [{ key: 'import', label: '导入中心', icon: 'upload' }] },
    {
      label: '流水账务',
      items: [
        { key: 'bank-flow', label: '银行流水', icon: 'wallet' },
        { key: 'order-flow', label: '订单流水', icon: 'receipt' },
      ],
    },
    {
      label: '对账与汇总',
      items: [
        { key: 'recon', label: '对账中心', icon: 'scale' },
        { key: 'summary', label: '数据汇总', icon: 'file' },
      ],
    },
    {
      label: '账务处理',
      items: [
        { key: 'voucher', label: '凭证管理', icon: 'book-open' },
        { key: 'balance', label: '科目余额表', icon: 'bar-chart' },
      ],
    },
    { label: '系统', items: [{ key: 'settings', label: '系统设置', icon: 'settings' }] },
  ];

  const navItems = navGroups.flatMap((g) => g.items);

  let route = $state<RouteKey>('import');

  const currentLabel = $derived(navItems.find((n) => n.key === route)?.label ?? '');
  // 顶栏分类：当前页所属导航分组。
  const currentGroup = $derived.by(() => {
    const group = navGroups.find((g) => g.items.some((i) => i.key === route));
    return group?.label ?? '';
  });

  function go(key: RouteKey): void {
    route = key;
  }

  /** 顶栏刷新：广播给当前页（各页面监听后重拉自己的数据）。 */
  function requestRefresh(): void {
    window.dispatchEvent(new CustomEvent('finlite:refresh'));
  }
</script>

<!-- 布局：main 锁定视口高度（overflow-hidden），仅右侧内容区独立滚动，左侧导航固定不随内容滚动。 -->
<main class="flex h-screen overflow-hidden">
  <aside class="sider">
    <div class="brand">
      <div class="brand-mark">F</div>
      <div class="brand-name">Finlite</div>
      <div class="brand-sub">单账套</div>
    </div>
    <nav class="sider-nav" aria-label="主导航">
      {#each navGroups as group}
        <div class="nav-group">
          <div class="nav-label">{group.label}</div>
          {#each group.items as item}
            <button
              class="nav-item {route === item.key ? 'active' : ''}"
              aria-current={route === item.key ? 'page' : undefined}
              onclick={() => go(item.key)}
            >
              <Icon name={item.icon} size={16} />
              <span>{item.label}</span>
            </button>
          {/each}
        </div>
      {/each}
    </nav>
    <div class="nav-foot"><span class="nav-dot"></span><span>账套正常</span></div>
  </aside>
  <!-- 内容列：顶栏固定不滚动，仅顶栏下方的内容主体独立滚动。 -->
  <div class="flex h-full min-w-0 flex-1 flex-col">
    <header class="topbar">
      <h1 class="topbar-title">
        <span class="topbar-cat">{currentGroup}</span>
        <span class="topbar-sep">|</span>{currentLabel}
      </h1>
      <div class="topbar-actions">
        <button
          class="icon-btn"
          type="button"
          title="刷新"
          aria-label="刷新"
          onclick={requestRefresh}
        >
          <Icon name="refresh" size={15} />
        </button>
        {#if route === 'bank-flow'}
          <button
            class="btn btn-primary"
            type="button"
            title="前往导入中心导入银行流水"
            onclick={() => go('import')}
          >
            <Icon name="upload" size={15} />
            前往导入中心
          </button>
        {/if}
      </div>
    </header>
    <div class="min-h-0 flex-1 overflow-y-auto px-[22px] pt-5 pb-14">
    {#if route === 'import'}
      <ImportPage onGoto={go} />
    {:else if route === 'bank-flow'}
      <BankFlowPage />
    {:else if route === 'order-flow'}
      <OrderFlowPage />
    {:else if route === 'recon'}
      <ReconPage />
    {:else if route === 'summary'}
      <SummaryPage />
    {:else if route === 'voucher'}
      <VoucherPage />
    {:else if route === 'balance'}
      <BalancePage />
    {:else}
      <SettingsPage />
    {/if}
    </div>
  </div>
</main>
