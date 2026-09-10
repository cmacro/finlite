<!-- 系统设置（F8）：科目维护、费率维护、备份/恢复（恢复需二次确认）。
TODO: 接 AccountService / FeeRateService / BackupService（bindings 生成后）。 -->
<script lang="ts">
  import Confirm from '../lib/Confirm.svelte';

  let backups = $state<string[]>([]);
  let restoreOpen = $state(false);
  let pendingFile = $state('');

  function askRestore(f: string): void {
    pendingFile = f;
    restoreOpen = true;
  }
  function doRestore(): void {
    restoreOpen = false;
    pendingFile = '';
    // TODO: 恢复备份需二次确认后重启应用
  }
</script>

<section>
  <h2 class="page-title">系统设置</h2>
  <h3 class="section-title">备份与恢复</h3>
  <div class="toolbar">
    <button class="btn" disabled title="bindings 生成后启用">立即备份</button>
  </div>
  <table class="data-table">
    <thead><tr><th>备份文件</th><th>操作</th></tr></thead>
    <tbody>
      {#each backups as b}
        <tr><td>{b}</td><td><button class="btn btn-sm btn-danger" onclick={() => askRestore(b)}>恢复</button></td></tr>
      {:else}
        <tr><td colspan="2" class="empty">暂无备份</td></tr>
      {/each}
    </tbody>
  </table>
  <Confirm open={restoreOpen} message="恢复备份将覆盖当前数据并重启应用，确认继续吗？" onconfirm={doRestore} oncancel={() => (restoreOpen = false)} />
</section>
