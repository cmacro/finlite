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
    | 'settings'
    | 'info'
    | 'check'
    | 'circle-check'
    | 'x'
    | 'alert-triangle'
    | 'arrow-right'
    | 'refresh'
    | 'folder'
    | 'inbox'
    | 'plus'
    | 'calendar'
    | 'lock';
</script>

<script lang="ts">
  import type { Component } from 'svelte';
  import {
    ArrowRight,
    Book,
    BookOpen,
    Building2,
    Calendar,
    ChartNoAxesColumnIncreasing,
    Check,
    CircleCheck,
    Download,
    FilePlus,
    FileText,
    Folder,
    House,
    Inbox,
    Info,
    ListChecks,
    Lock,
    Plus,
    Receipt,
    RefreshCw,
    Scale,
    Search,
    Settings,
    SquareCheck,
    Target,
    TriangleAlert,
    Upload,
    Users,
    Wallet,
    X,
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
    info: Info,
    check: Check,
    'circle-check': CircleCheck,
    x: X,
    'alert-triangle': TriangleAlert,
    'arrow-right': ArrowRight,
    refresh: RefreshCw,
    folder: Folder,
    inbox: Inbox,
    plus: Plus,
    calendar: Calendar,
    lock: Lock,
  };

  const C = $derived(map[name] ?? House);
</script>

<C {size} />
