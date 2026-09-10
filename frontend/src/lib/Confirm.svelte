<!-- 通用确认对话框：余额确认、审核/反审核、恢复备份等破坏性或状态变更操作必须先弹框。 -->
<script lang="ts">
  interface Props {
    open: boolean;
    message: string;
    onconfirm: () => void;
    oncancel: () => void;
  }
  let { open, message, onconfirm, oncancel }: Props = $props();
</script>

{#if open}
  <div class="confirm-mask" role="presentation" onclick={oncancel}>
    <div
      class="confirm-box"
      role="alertdialog"
      aria-modal="true"
      aria-label="确认操作"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => { if (e.key === 'Escape') oncancel(); }}
      tabindex="-1"
    >
      <p class="confirm-msg">{message}</p>
      <div class="confirm-actions">
        <button class="btn" onclick={oncancel}>取消</button>
        <button class="btn btn-danger" onclick={onconfirm}>确认</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .confirm-mask {
    position: fixed;
    inset: 0;
    background: var(--mask);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 50;
  }
  .confirm-box {
    background: var(--bg);
    border-radius: 8px;
    padding: 20px 24px;
    min-width: 320px;
    max-width: 480px;
  }
  .confirm-msg {
    margin: 0 0 16px;
    font-size: 14px;
  }
  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
