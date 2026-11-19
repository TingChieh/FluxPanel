import { describe, expect, it } from 'vitest';

import { genWireguardConfig, wireguardConfigFromLink } from '@/lib/xray/inbound-link';
import { buildWireguardClientConfig } from '@/pages/clients/wireguardConfig';
import type { WireguardInboundSettings } from '@/schemas/protocols/inbound/wireguard';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

// A value carrying a line break adds a line such as a root PostUp to the .conf;
// node sync, an import or an external subscription can each supply one.
const INJECTED = '140232109\nPostUp = touch /tmp/pwned';

const endpoint = { address: 'vpn.example.test', port: 51820, peerIndex: 0, remark: 'peer-1' };

function wgSettings(dns: string) {
  return {
    secretKey: '',
    mtu: 1420,
    dns,
    peers: [{ privateKey: 'clientPrivKey==', allowedIPs: ['10.0.0.2/32'] }],
  } as unknown as WireguardInboundSettings;
}

function wgInbound(wgDns: string) {
  return {
    id: 2,
    tag: 'wg-1',
    remark: 'wg',
    port: 51820,
    protocol: 'wireguard',
    wgPublicKey: 'serverPubKey==',
    wgDns,
  } as unknown as InboundOption;
}

const client = {
  email: 'peer-1',
  privateKey: 'clientPrivKey==',
  allowedIPs: '10.8.1.2/32',
} as unknown as ClientRecord;

describe('.conf builders render nothing when a value carries a line break', () => {
  it('genWireguardConfig', () => {
    expect(genWireguardConfig({ settings: wgSettings('9.9.9.9'), ...endpoint })).toContain(
      'DNS = 9.9.9.9\n',
    );
    expect(genWireguardConfig({ settings: wgSettings(INJECTED), ...endpoint })).toBe('');
  });

  it('buildWireguardClientConfig', () => {
    expect(buildWireguardClientConfig(client, wgInbound('9.9.9.9'), 'vpn.example.test')).toContain(
      'DNS = 9.9.9.9\n',
    );
    expect(buildWireguardClientConfig(client, wgInbound(INJECTED), 'vpn.example.test')).toBe('');
  });

  it('wireguardConfigFromLink', () => {
    const link = (address: string) =>
      `wireguard://PRIVKEY@203.0.113.9:51820/?address=${address}&publickey=PEERPUB&dns=1.1.1.1&allowedips=0.0.0.0%2F0`;
    expect(wireguardConfigFromLink(link('10.0.0.2%2F32'))).toContain('Address = 10.0.0.2/32\n');
    expect(
      wireguardConfigFromLink(link('10.0.0.2%2F32%0APostUp%20%3D%20touch%20%2Ftmp%2Fpwned')),
    ).toBe('');
  });
});
