<script lang="ts">
  import { onMount } from 'svelte';
  import AppShell from './components/AppShell.svelte';
  import { t } from './lib/i18n';
  import type { NavItem } from './lib/ui';
  import { routeFromHash, routes } from './routes';

  let route = $state(routeFromHash(location.hash));

  onMount(() => {
    const onHash = () => (route = routeFromHash(location.hash));
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  });

  const items: NavItem[] = routes.map((r) => ({ id: r.id, href: r.path, label: t[r.labelKey], icon: r.icon }));

  $effect(() => {
    document.title = `${t[route.labelKey]} · ${t.brandName}`;
  });
</script>

<AppShell {items} current={route.id}>
  <route.component />
</AppShell>
