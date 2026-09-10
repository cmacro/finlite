<!-- 图标映射组件：名字 → Lucide 图标，与 ForgeFin `src/components/icon.rs` 同名对照，
     后续从 ForgeFin 搬页面时图标名可直接沿用。按需导入，Vite 只打包用到的图标。 -->
<script lang="ts" module>
  export type IconName =
    | 'home'
    | 'book'
    | 'book-open'
    | 'file'
    | 'file-plus'
    | 'report'
    | 'receipt'
    | 'check-square'
    | 'list'
    | 'search'
    | 'scale'
    | 'bar-chart'
    | 'download'
    | 'upload'
    | 'building'
    | 'users'
    | 'wallet'
    | 'target'
    | 'settings';
</script>

<script lang="ts">
  import type { Component } from 'svelte';
  import {
    Book,
    BookOpen,
    Building2,
    ChartNoAxesColumnIncreasing,
    Download,
    FilePlus,
    FileText,
    House,
    ListChecks,
    Receipt,
    Scale,
    Search,
    Settings,
    SquareCheck,
    Target,
    Upload,
    Users,
    Wallet,
  } from '@lucide/svelte';

  interface Props {
    name: IconName;
    size?: number;
  }
  let { name, size = 16 }: Props = $props();

  const map: Record<IconName, Component> = {
    home: House,
    book: Book,
    'book-open': BookOpen,
    file: FileText,
    'file-plus': FilePlus,
    report: FileText,
    receipt: Receipt,
    'check-square': SquareCheck,
    list: ListChecks,
    search: Search,
    scale: Scale,
    'bar-chart': ChartNoAxesColumnIncreasing,
    download: Download,
    upload: Upload,
    building: Building2,
    users: Users,
    wallet: Wallet,
    target: Target,
    settings: Settings,
  };

  const C = $derived(map[name] ?? House);
</script>

<C {size} />
