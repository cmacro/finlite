<!-- 通用确认对话框：余额确认、审核/反审核、恢复备份等破坏性或状态变更操作必须先弹框。
样式来自 styles/modal.css，无 <style> 块。 -->
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
  <div class="modal-overlay" role="presentation" onclick={oncancel}>
    <div
      class="modal-box"
      role="alertdialog"
      aria-modal="true"
      aria-label="确认操作"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => { if (e.key === 'Escape') oncancel(); }}
      tabindex="-1"
    >
      <p class="modal-msg">{message}</p>
      <div class="modal-actions">
        <button class="btn" onclick={oncancel}>取消</button>
        <button class="btn btn-danger" onclick={onconfirm}>确认</button>
      </div>
    </div>
  </div>
{/if}
