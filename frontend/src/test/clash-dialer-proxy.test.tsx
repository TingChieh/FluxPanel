import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { HostRecord } from '@/api/queries/useHostsQuery';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import HostFormModal from '@/pages/hosts/HostFormModal';
import HostList from '@/pages/hosts/HostList';
import { HostFormSchema } from '@/schemas/api/host';
import { ExternalLinkSchema } from '@/schemas/client';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

describe('Clash dialer-proxy support', () => {
  beforeEach(() => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', []));
  });

  it('validates and trims mihomoDialerProxy in HostFormSchema and dialerProxy in ExternalLinkSchema', () => {
    const hostParsed = HostFormSchema.parse({
      inboundId: 1,
      remark: 'Edge-US',
      mihomoDialerProxy: ' Relay-Hub ',
    });
    expect((hostParsed as Record<string, unknown>).mihomoDialerProxy).toBe('Relay-Hub');

    const linkParsed = ExternalLinkSchema.parse({
      kind: 'link',
      value: 'vless://11111111-1111-1111-1111-111111111111@example.com:443#SG',
      dialerProxy: ' Relay-Hub ',
    });
    expect((linkParsed as Record<string, unknown>).dialerProxy).toBe('Relay-Hub');
  });

  it('edits and saves mihomoDialerProxy on HostFormModal Clash tab', async () => {
    const user = userEvent.setup();
    const save = vi.fn().mockResolvedValue({ success: true });
    const relayHost = {
      groupId: 'g-relay',
      remark: 'Relay-Hub',
      inboundIds: [1],
      hosts: ['relay.example.com'],
    } as HostRecord;
    const exitHost = {
      groupId: 'g-exit',
      remark: 'Edge-US',
      inboundIds: [2],
      hosts: ['edge.example.com'],
      mihomoDialerProxy: 'Relay-Hub',
    } as HostRecord;

    renderWithProviders(
      <HostFormModal
        open
        mode="edit"
        host={exitHost}
        inboundOptions={[
          { id: 1, remark: 'Relay-Hub', tag: 'gz', protocol: 'shadowsocks' },
          { id: 2, remark: 'Edge-US', tag: 'sg', protocol: 'vless' },
        ]}
        existingHosts={[relayHost, exitHost]}
        save={save}
        onOpenChange={() => {}}
      />,
    );

    const clashTab = await screen.findByRole('tab', { name: /clash/i });
    await user.click(clashTab);

    const input = await screen.findByLabelText(/dialer proxy/i);
    expect((input as HTMLInputElement).value).toBe('Relay-Hub');

    fireEvent.change(input, { target: { value: 'Backup-Relay' } });
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(save.mock.calls[0][0]).toMatchObject({ mihomoDialerProxy: 'Backup-Relay' });
  });

  it('hydrates and saves dialerProxy on external links in ClientFormModal', async () => {
    const user = userEvent.setup();
    const save = vi.fn().mockResolvedValue({ success: true });
    const vlessInbound = {
      id: 1,
      port: 11701,
      protocol: 'shadowsocks',
      tag: 'relay-hub',
      remark: 'Relay-Hub',
      enable: true,
    } as unknown as InboundOption;
    const client = {
      email: 'alice',
      password: 'pw',
      subId: 'sub-alice',
      enable: true,
    } as unknown as ClientRecord;

    renderWithProviders(
      <ClientFormModal
        open
        mode="edit"
        client={client}
        inbounds={[vlessInbound]}
        attachedIds={[1]}
        attachedExternalLinks={[
          {
            id: 9,
            kind: 'link',
            value: 'vless://11111111-1111-1111-1111-111111111111@edge.example.com:443#SG',
            remark: 'Edge-US',
            enable: true,
            expiryTime: 0,
            namePrefix: '',
            dialerProxy: 'Relay-Hub',
            lastFetchAt: 0,
            lastFetchError: '',
          } as never,
        ]}
        save={save}
        onOpenChange={() => {}}
      />,
    );

    const linksTab = await screen.findByRole('tab', { name: 'Links' });
    await user.click(linksTab);

    const dpInput = await screen.findByPlaceholderText(/dialer-proxy/i);
    expect((dpInput as HTMLInputElement).value).toBe('Relay-Hub');

    fireEvent.change(dpInput, { target: { value: 'Transit-Relay' } });
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(save.mock.calls[0][1].externalLinks[0]).toMatchObject({
      remark: 'Edge-US',
      dialerProxy: 'Transit-Relay',
    });
  });

  it('displays mihomoDialerProxy tag in HostList when configured', () => {
    const noop = () => {};
    renderWithProviders(
      <HostList
        hosts={[
          {
            groupId: 'g-exit',
            remark: 'Edge-US',
            inboundIds: [2],
            hosts: ['edge.example.com'],
            security: 'reality',
            mihomoDialerProxy: 'Relay-Hub',
          } as HostRecord,
        ]}
        inboundOptions={[{ id: 2, remark: 'Edge-US', tag: 'sg', protocol: 'vless' }]}
        selectedGroupIds={[]}
        onSelectionChange={noop}
        onAdd={noop}
        onEdit={noop}
        onDelete={noop}
        onToggleEnable={noop}
        onMove={noop}
        onReorder={noop}
        onBulkEnable={noop}
        onBulkDelete={noop}
      />,
    );

    expect(screen.getByText('Relay-Hub')).toBeTruthy();
  });
});
