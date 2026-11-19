import { describe, expect, it } from 'vitest';

import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import { withHostEndpoints } from '@/lib/hosts/host-link';
import { formatTunnelConfigMeta } from '@/lib/inbounds/label';
import { genWireguardPeerConfigs } from '@/lib/xray/inbound-link';
import { tunnelConfigEndpoints } from '@/pages/clients/tunnelEndpoints';
import { buildWireguardClientConfig } from '@/pages/clients/wireguardConfig';
import { InboundSchema } from '@/schemas/api/inbound';
import type { HostRecord } from '@/schemas/api/host';

const PANEL = 'panel.example.com';
const HOSTS: HostRecord[] = [
  {
    groupId: 'cdn',
    inboundIds: [7],
    hosts: ['edge.example.com:443', 'edge2.example.com'],
    remark: 'CDN',
  },
];

function endpointLines(configs: string[]): string[] {
  return configs.map((cfg) => cfg.match(/^Endpoint = (.*)$/m)?.[1] ?? '');
}

describe('inbounds page tunnel configs follow Hosts', () => {
  it('renders one WireGuard config per Host for each peer', () => {
    const inbound = InboundSchema.parse({
      port: 51820,
      protocol: 'wireguard',
      settings: {
        secretKey: 'iJ2cBkrSGqRwIfYIDIxk7hr5RXfdR93MfJUL7yqkkH8=',
        peers: [],
        clients: [
          {
            email: 'alice',
            privateKey: 'QGVlb2dXc1ZTWGw0ZXBzZndsWmtMaUM5MUlNYjBHWFdYbz0=',
            allowedIPs: ['10.0.0.2/32'],
          },
        ],
      },
    });
    const peers = genWireguardPeerConfigs({
      inbound: withHostEndpoints(inbound, 7, HOSTS, '', PANEL),
      remark: 'wg',
      fallbackHostname: PANEL,
    });
    expect(peers).toHaveLength(1);
    expect(endpointLines(peers[0])).toEqual(['edge.example.com:443', 'edge2.example.com:51820']);
  });
});

describe('clients page tunnel configs follow Hosts', () => {
  const client = {
    email: 'alice',
    privateKey: 'clientPrivKey==',
    allowedIPs: '10.0.0.2/32',
    uuid: 'e79b9107-1607-4e6c-a496-d8f99e4f0dc5',
    password: 'secret',
  } as unknown as ClientRecord;

  it('advertises each Host in the WireGuard config', () => {
    const inbound = { id: 7, remark: 'wg', protocol: 'wireguard', port: 51820 } as InboundOption;
    const configs = tunnelConfigEndpoints(inbound, HOSTS, PANEL, '').map((ep) =>
      buildWireguardClientConfig(client, inbound, PANEL, '', '', ep),
    );
    expect(endpointLines(configs)).toEqual(['edge.example.com:443', 'edge2.example.com:51820']);
  });

  it('keeps the inbound address when no Host applies to it', () => {
    const inbound = { id: 8, remark: 'wg', protocol: 'wireguard', port: 51820 } as InboundOption;
    const configs = tunnelConfigEndpoints(inbound, HOSTS, PANEL, '').map((ep) =>
      buildWireguardClientConfig(client, inbound, PANEL, '', '', ep),
    );
    expect(endpointLines(configs)).toEqual([`${PANEL}:51820`]);
  });

  it('names two Hosts of one inbound apart', () => {
    const inbound = { id: 7, remark: 'wg' };
    const a = formatTunnelConfigMeta(inbound, 'alice', 2, 'edge.example.com:443');
    const b = formatTunnelConfigMeta(inbound, 'alice', 2, 'edge2.example.com:51820');
    expect([a.fileName, b.fileName]).toEqual([
      'alice-wg-edge.example.com_443.conf',
      'alice-wg-edge2.example.com_51820.conf',
    ]);
    expect([a.label, b.label]).toEqual([
      'wg - edge.example.com:443',
      'wg - edge2.example.com:51820',
    ]);
  });

  it('expands a CIDR Host into distinct endpoint configs', () => {
    const inbound = { id: 7, remark: 'wg', protocol: 'wireguard', port: 51820 } as InboundOption;
    const cidrHosts: HostRecord[] = [
      {
        groupId: 'cidr-group',
        inboundIds: [7],
        hosts: ['198.51.100.0/30'],
        randomCount: 2,
        remark: 'CIDR-WG',
      },
    ];
    const eps = tunnelConfigEndpoints(inbound, cidrHosts, PANEL, '');
    expect(eps).toHaveLength(2);
    expect(eps[0]?.remark).toBe('CIDR-WG - 1');
    expect(eps[1]?.remark).toBe('CIDR-WG - 2');
    expect(eps.map((e) => e?.port)).toEqual([51820, 51820]);
    for (const ep of eps) {
      expect(ep?.dest).toMatch(/^198\.51\.100\.[12]$/);
    }
  });
});
