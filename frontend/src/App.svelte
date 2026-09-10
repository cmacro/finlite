<!-- finlite 主框架：左侧导航 + 8 个业务页面（Svelte 5 runes）。
页面数据一律经 Wails Service 绑定获取（bindings 生成后接线），本文件只做路由切换。 -->
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

  function go(key: RouteKey): void {
    route = key;
  }
</script>

<main class="layout">
  <aside class="sidebar">
    <h1 class="app-title">finlite 财务助手</h1>
    <nav>
      {#each nav as item}
        <button class="nav-item" class:active={route === item.key} onclick={() => go(item.key)}>
          <Icon name={item.icon} size={16} />
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>
  </aside>
  <div class="content">
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
</main>

<style>
  .layout {
    display: flex;
    min-height: 100vh;
  }
  .sidebar {
    width: 180px;
    flex-shrink: 0;
    border-right: 1px solid var(--border-side);
    padding: 16px 8px;
  }
  .app-title {
    font-size: 16px;
    margin: 0 8px 16px;
  }
  .nav-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    text-align: left;
    padding: 8px 12px;
    border: 0;
    background: transparent;
    cursor: pointer;
    font-size: 14px;
    border-radius: 6px;
    color: inherit;
  }
  .nav-item.active {
    background: var(--nav-active-bg);
    font-weight: 600;
  }
  .content {
    flex: 1;
    padding: 16px 20px;
    min-width: 0;
  }
</style>
