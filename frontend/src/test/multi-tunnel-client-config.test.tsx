import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import ClientInfoModal from '@/pages/clients/ClientInfoModal';
import ClientQrModal from '@/pages/clients/ClientQrModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import type { HostRecord } from '@/schemas/api/host';
import { renderWithProviders } from './test-utils';

const usWgInbound: InboundOption = {
  id: 201,
  tag: 'wg-us',
  remark: 'US · New York',
  port: 51820,
  protocol: 'wireguard',
  nodeAddress: 'us.vpn.example.com',
  wgPublicKey: 'usWgServerPublicKey==',
  wgDns: '1.1.1.1',
  wgMtu: 1420,
};

const euWgInbound: InboundOption = {
  id: 202,
  tag: 'wg-eu',
  remark: 'EU · Frankfurt',
  port: 51821,
  protocol: 'wireguard',
  nodeAddress: 'eu.vpn.example.com',
  wgPublicKey: 'euWgServerPublicKey==',
  wgDns: '9.9.9.9',
  wgMtu: 1400,
};

const multiWgClient: ClientRecord = {
  id: 'c2',
  email: 'WG-CLIENT',
  privateKey: 'wgClientPrivateKey==',
  publicKey: 'wgClientPublicKey==',
  preSharedKey: 'wgClientPsk==',
  allowedIPs: '10.0.0.2/32',
  keepAlive: 25,
  inboundIds: [201, 202],
  enable: true,
} as unknown as ClientRecord;

const singleWgClient: ClientRecord = {
  id: 'c3',
  email: 'SINGLE-CLIENT',
  privateKey: 'wgClientPrivateKey==',
  publicKey: 'wgClientPublicKey==',
  allowedIPs: '10.0.0.2/32',
  inboundIds: [201],
  enable: true,
} as unknown as ClientRecord;

describe('Multi-tunnel Client Modals', () => {
  it('renders distinct labeled ConfigBlocks in ClientInfoModal for multiple WireGuard inbounds', () => {
    renderWithProviders(
      <ClientInfoModal
        open
        client={multiWgClient}
        inboundsById={{ 201: usWgInbound, 202: euWgInbound }}
        isOnline={false}
        tunnelAllowedIPs={{ 201: '10.0.1.2/32', 202: '10.0.2.2/32' }}
        onOpenChange={() => {}}
      />,
    );

    expect(screen.getAllByText('US · New York')).toHaveLength(2);
    expect(screen.getByText('EU · Frankfurt')).toBeTruthy();
    expect(document.querySelectorAll('.config-block')).toHaveLength(2);
  });

  it('renders single default-labeled ConfigBlock in ClientInfoModal for single inbound', () => {
    renderWithProviders(
      <ClientInfoModal
        open
        client={singleWgClient}
        inboundsById={{ 201: usWgInbound }}
        isOnline={false}
        onOpenChange={() => {}}
      />,
    );

    expect(document.querySelectorAll('.config-block')).toHaveLength(1);
    expect(screen.getByText('Config')).toBeTruthy();
  });

  it('renders separate collapse panels in ClientQrModal for multiple WireGuard inbounds', () => {
    renderWithProviders(
      <MemoryRouter initialEntries={['/clients']}>
        <ClientQrModal
          open
          client={multiWgClient}
          inboundsById={{ 201: usWgInbound, 202: euWgInbound }}
          tunnelAllowedIPs={{ 201: '10.0.1.2/32', 202: '10.0.2.2/32' }}
          onOpenChange={() => {}}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText('US · New York')).toBeTruthy();
    expect(screen.getByText('EU · Frankfurt')).toBeTruthy();
  });

  // The subscription beside these configs advertises the inbound's Hosts, so
  // the panel-built configs must too — one per Host address.
  const edgeHosts: HostRecord[] = [
    {
      groupId: 'cdn',
      inboundIds: [201],
      hosts: ['edge.example.com:443', 'edge2.example.com'],
      remark: 'CDN',
    },
  ];
  const edgeClient = { ...multiWgClient, inboundIds: [201] } as ClientRecord;
  const edgeLabels = [
    'US · New York - edge.example.com:443',
    'US · New York - edge2.example.com:51820',
  ];

  it('renders one ConfigBlock per Host in ClientInfoModal', () => {
    renderWithProviders(
      <ClientInfoModal
        open
        client={edgeClient}
        inboundsById={{ 201: usWgInbound }}
        isOnline={false}
        hosts={edgeHosts}
        onOpenChange={() => {}}
      />,
    );

    for (const label of edgeLabels) expect(screen.getByText(label)).toBeTruthy();
  });

  it('renders one collapse panel per Host in ClientQrModal', () => {
    renderWithProviders(
      <MemoryRouter initialEntries={['/clients']}>
        <ClientQrModal
          open
          client={edgeClient}
          inboundsById={{ 201: usWgInbound }}
          hosts={edgeHosts}
          onOpenChange={() => {}}
        />
      </MemoryRouter>,
    );

    for (const label of edgeLabels) expect(screen.getByText(label)).toBeTruthy();
  });
});
