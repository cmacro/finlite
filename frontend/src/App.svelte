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

  const nav: { key: RouteKey; label: string; icon: IconName }[] = [
    { key: 'import', label: '导入中心', icon: 'upload' },
    { key: 'bank-flow', label: '银行流水', icon: 'wallet' },
    { key: 'order-flow', label: '订单流水', icon: 'receipt' },
    { key: 'recon', label: '对账中心', icon: 'scale' },
    { key: 'summary', label: '数据汇总', icon: 'file' },
    { key: 'voucher', label: '凭证管理', icon: 'book-open' },
    { key: 'balance', label: '科目余额表', icon: 'bar-chart' },
    { key: 'settings', label: '系统设置', icon: 'settings' },
  ];

  let route = $state<RouteKey>('import');

  const currentLabel = $derived(nav.find((n) => n.key === route)?.label ?? '');

  function go(key: RouteKey): void {
    route = key;
  }
</script>

<!-- 布局：main 锁定视口高度（overflow-hidden），仅右侧内容区独立滚动，左侧导航固定不随内容滚动。 -->
<main class="flex h-screen overflow-hidden">
  <aside class="w-[180px] shrink-0 border-r border-sider px-2 py-4">
    <h1 class="mx-2 mb-4 text-base font-bold">finlite 财务助手</h1>
    <nav>
      {#each nav as item}
        <button
          class="flex w-full cursor-pointer items-center gap-2 rounded-md border-0 bg-transparent px-3 py-2 text-left text-sm text-inherit {route ===
          item.key
            ? 'bg-navactive font-semibold'
            : ''}"
          onclick={() => go(item.key)}
        >
          <Icon name={item.icon} size={16} />
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>
  </aside>
  <!-- 内容列：标题行固定不滚动，仅标题下方的内容主体独立滚动。 -->
  <div class="flex h-full min-w-0 flex-1 flex-col">
    <header class="shrink-0 px-5 pt-4">
      <h2 class="page-title">{currentLabel}</h2>
    </header>
    <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-4">
    {#if route === 'import'}
      <ImportPage />
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
