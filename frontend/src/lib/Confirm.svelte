<!-- 通用确认对话框：余额确认、审核/反审核、恢复备份等破坏性或状态变更操作必须先弹框。
可选 title / confirmLabel / variant（info 主操作蓝底，danger 危险操作红描边，默认 danger）。
样式来自 styles/modal.css，无 <style> 块。 -->
<script lang="ts">
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    message: string;
    onconfirm: () => void;
    oncancel: () => void;
    title?: string;
    confirmLabel?: string;
    variant?: 'info' | 'danger';
  }
  let {
    open,
    message,
    onconfirm,
    oncancel,
    title = '',
    confirmLabel = '确认',
    variant = 'danger',
  }: Props = $props();
</script>

{#if open}
  <div class="modal-overlay" role="presentation" onclick={oncancel}>
    <div
      class="modal-box"
      role="alertdialog"
      aria-modal="true"
      aria-label={title || '确认操作'}
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => { if (e.key === 'Escape') oncancel(); }}
      tabindex="-1"
    >
      {#if title}
        <div class="modal-head">
          <span class="modal-ico {variant}">
            <Icon name={variant === 'danger' ? 'alert-triangle' : 'info'} size={18} />
          </span>
          <h3 class="modal-title">{title}</h3>
        </div>
      {/if}
      <p class="modal-msg">{message}</p>
      <div class="modal-actions">
        <button class="btn" onclick={oncancel}>取消</button>
        <button
          class="btn {variant === 'danger' ? 'btn-danger' : 'btn-primary'}"
          onclick={onconfirm}
        >{confirmLabel}</button>
      </div>
    </div>
  </div>
{/if}
