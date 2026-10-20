<!-- 系统设置（F8）：科目维护、费率维护、备份/恢复（恢复需二次确认后重启）。
     数据经 AccountService / FeeRateService / BackupService（Wails 绑定）。 -->
<script lang="ts">
  import { AccountService, FeeRateService, BackupService } from '../../bindings/finlite/internal/services';
  import type { Account, FeeRate } from '../../bindings/finlite/internal/services/models';
  import Confirm from '../lib/Confirm.svelte';

  let accounts = $state<Account[]>([]);
  let rates = $state<FeeRate[]>([]);
  let backups = $state<string[]>([]);

  let busy = $state(false);
  let notice = $state('');
  let noticeClass = $state('text-muted');

  // 科目编辑
  let accountEdit = $state<Account | null>(null);
  // 费率编辑
  let rateEdit = $state<FeeRate | null>(null);
  // 恢复确认
  let restoreOpen = $state(false);
  let pendingFile = $state('');

  async function refresh(): Promise<void> {
    busy = true;
    try {
      accounts = (await AccountService.List()) ?? [];
      rates = (await FeeRateService.List()) ?? [];
      backups = (await BackupService.ListBackups()) ?? [];
    } catch (e) {
      notice = `加载设置失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function doBackup(): Promise<void> {
    busy = true;
    try {
      const p = await BackupService.Backup();
      notice = `备份完成：${p}`;
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `备份失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  function askRestore(f: string): void {
    pendingFile = f;
    restoreOpen = true;
  }

  async function doRestore(): Promise<void> {
    restoreOpen = false;
    const f = pendingFile;
    pendingFile = '';
    if (!f) return;
    busy = true;
    try {
      const safety = await BackupService.Restore(f);
      notice = `已恢复备份，当前库快照留存于 ${safety}。请重启应用以生效。`;
      noticeClass = 'text-warn';
    } catch (e) {
      notice = `恢复失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function saveAccount(): Promise<void> {
    const a = accountEdit;
    accountEdit = null;
    if (!a) return;
    busy = true;
    try {
      await AccountService.Upsert(a);
      notice = `已保存科目 ${a.code} ${a.name}。`;
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `保存科目失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  async function saveRate(): Promise<void> {
    const r = rateEdit;
    rateEdit = null;
    if (!r) return;
    busy = true;
    try {
      await FeeRateService.Update(r.payMethod, r.rate);
      notice = `已更新费率 ${r.payMethod} = ${r.rate}。`;
      noticeClass = 'text-ok';
      await refresh();
    } catch (e) {
      notice = `更新费率失败：${e instanceof Error ? e.message : String(e)}`;
      noticeClass = 'text-danger';
    } finally {
      busy = false;
    }
  }

  function newAccount(): void {
    accountEdit = { code: '', name: '', parentCode: '', accountType: 'expense', isActive: 1 };
  }

  // 顶栏刷新按钮（App.svelte 广播）：重拉科目、费率与备份列表。
  $effect(() => {
    const handler = () => void refresh();
    window.addEventListener('finlite:refresh', handler);
    return () => window.removeEventListener('finlite:refresh', handler);
  });

  refresh();
</script>

<section>
  <h3 class="section-title">会计科目</h3>
  <div class="toolbar">
    <button class="btn" onclick={newAccount} disabled={busy}>新增科目</button>
    <span class="text-sm text-muted">方向：asset/cost/expense 在借方，income 在贷方</span>
  </div>
  <table class="data-table">
    <thead><tr><th>代码</th><th>名称</th><th>上级</th><th>类型</th><th>启用</th><th>操作</th></tr></thead>
    <tbody>
      {#each accounts as a (a.code)}
        <tr>
          <td>{a.code}</td><td>{a.name}</td><td>{a.parentCode}</td><td>{a.accountType}</td>
          <td>{a.isActive ? '是' : '否'}</td>
          <td><button class="btn btn-sm" onclick={() => (accountEdit = { ...a })} disabled={busy}>编辑</button></td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">暂无科目</td></tr>
      {/each}
    </tbody>
  </table>

  <h3 class="section-title">手续费费率</h3>
  <table class="data-table">
    <thead><tr><th>支付方式</th><th class="num">费率</th><th>更新时间</th><th>操作</th></tr></thead>
    <tbody>
      {#each rates as r (r.payMethod)}
        <tr>
          <td>{r.payMethod}</td>
          <td class="num">{r.rate}</td>
          <td class="whitespace-nowrap">{r.updatedAt}</td>
          <td><button class="btn btn-sm" onclick={() => (rateEdit = { ...r })} disabled={busy}>编辑</button></td>
        </tr>
      {:else}
        <tr><td colspan="4" class="empty">暂无费率</td></tr>
      {/each}
    </tbody>
  </table>

  <h3 class="section-title">备份与恢复</h3>
  <div class="toolbar">
    <button class="btn" onclick={doBackup} disabled={busy}>立即备份</button>
    <span class="text-sm text-muted">恢复会覆盖当前库并留 pre-restore 快照，需重启应用生效</span>
  </div>
  {#if notice}<p class="my-2 text-sm {noticeClass}">{notice}</p>{/if}
  <table class="data-table">
    <thead><tr><th>备份文件</th><th>操作</th></tr></thead>
    <tbody>
      {#each backups as b (b)}
        <tr>
          <td class="break-all">{b}</td>
          <td><button class="btn btn-sm btn-danger" onclick={() => askRestore(b)} disabled={busy}>恢复</button></td>
        </tr>
      {:else}
        <tr><td colspan="2" class="empty">暂无备份</td></tr>
      {/each}
    </tbody>
  </table>

  <Confirm
    open={restoreOpen}
    message="恢复备份将覆盖当前数据并重启应用，确认继续吗？"
    onconfirm={doRestore}
    oncancel={() => (restoreOpen = false)}
  />

  {#if accountEdit}
    <div class="modal-overlay" role="presentation" onclick={() => (accountEdit = null)}>
      <div
        class="modal-box"
        role="dialog"
        aria-modal="true"
        aria-label="科目编辑"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') accountEdit = null; }}
        tabindex="-1"
      >
        <h3 class="mb-3 text-base font-semibold">科目</h3>
        <div class="flex flex-col gap-2 text-sm">
          <label class="flex flex-col gap-1">代码
            <input class="input" bind:value={accountEdit.code} disabled={!!accounts.find((x) => x.code === accountEdit?.code)} />
          </label>
          <label class="flex flex-col gap-1">名称
            <input class="input" bind:value={accountEdit.name} />
          </label>
          <label class="flex flex-col gap-1">上级科目代码
            <input class="input" bind:value={accountEdit.parentCode} />
          </label>
          <label class="flex flex-col gap-1">类型
            <select class="input" bind:value={accountEdit.accountType}>
              <option value="asset">asset 资产</option>
              <option value="income">income 收入</option>
              <option value="cost">cost 成本</option>
              <option value="expense">expense 费用</option>
            </select>
          </label>
          <label class="flex items-center gap-2">
            <input
              type="checkbox"
              checked={accountEdit.isActive === 1}
              onchange={(e) => { if (accountEdit) accountEdit.isActive = (e.currentTarget as HTMLInputElement).checked ? 1 : 0; }}
            /> 启用
          </label>
        </div>
        <div class="modal-actions mt-4">
          <button class="btn" onclick={() => (accountEdit = null)}>取消</button>
          <button class="btn" onclick={saveAccount} disabled={busy}>保存</button>
        </div>
      </div>
    </div>
  {/if}

  {#if rateEdit}
    <div class="modal-overlay" role="presentation" onclick={() => (rateEdit = null)}>
      <div
        class="modal-box"
        role="dialog"
        aria-modal="true"
        aria-label="费率编辑"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') rateEdit = null; }}
        tabindex="-1"
      >
        <h3 class="mb-3 text-base font-semibold">费率 {rateEdit.payMethod}</h3>
        <label class="flex flex-col gap-1 text-sm">费率（小数形式，如 0.0025）
          <input class="input num-input" bind:value={rateEdit.rate} />
        </label>
        <div class="modal-actions mt-4">
          <button class="btn" onclick={() => (rateEdit = null)}>取消</button>
          <button class="btn" onclick={saveRate} disabled={busy}>保存</button>
        </div>
      </div>
    </div>
  {/if}
</section>
