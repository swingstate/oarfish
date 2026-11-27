// The texts of the default routes and the name servers on the Dashboard's
// network card and the page Netzwerk (tasks N0 and N1, ORB-NET-001, docs/UI.md). Addresses and interface
// names come from the service and are text only.
import { fmt, t } from './i18n';
import type { DefaultRoute, Routing } from './metrics';
import type { KeyValueItem } from './ui';

function routeItem(label: string, r: DefaultRoute, testid: string): KeyValueItem {
  const item: KeyValueItem = { label, value: null, mono: true, testid };
  if (r.status === 'none') return { ...item, placeholder: t.netRouteNone };
  if (r.status !== 'found' || r.interface === null) return item; // "Nicht verfügbar"
  const iface = r.interface;
  if (r.gateway_hidden) return { ...item, value: fmt('netRouteHidden', { interface: iface }), note: t.netRouteHiddenNote };
  if (r.gateway === null) return { ...item, value: fmt('netRouteDirect', { interface: iface }) };
  return { ...item, value: fmt('netRouteVia', { gateway: r.gateway, interface: iface }) };
}

function dnsItem(d: Routing['dns']): KeyValueItem {
  const item: KeyValueItem = { label: t.netDns, value: null, mono: true, testid: 'dns-servers' };
  if (d.status === 'none') return { ...item, placeholder: t.netDnsNone };
  if (d.status !== 'found') return item;
  const notes: string[] = [];
  if (d.via_local_resolver) notes.push(t.netDnsViaResolver);
  if (d.servers.some((s) => s.local)) notes.push(t.netDnsLocal);
  if (d.hidden > 0) notes.push(fmt('netDnsHidden', { count: d.hidden }));
  const value = d.servers.map((s) => (s.local ? fmt('netDnsLocalServer', { address: s.address }) : s.address)).join(', ');
  return { ...item, value: value === '' ? null : value, placeholder: t.netDnsAllHidden, note: notes.length > 0 ? notes.join('; ') : null };
}

/** The rows of the routing part: IPv4 and IPv6 default route, then the name servers. */
export function routingItems(r: Routing): KeyValueItem[] {
  return [routeItem(t.netRouteIPv4, r.ipv4, 'route-ipv4'), routeItem(t.netRouteIPv6, r.ipv6, 'route-ipv6'), dnsItem(r.dns)];
}
