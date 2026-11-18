import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ClientInfoModal from '@/pages/clients/ClientInfoModal';
import NodeList from '@/pages/nodes/NodeList';
import type { ClientRecord } from '@/hooks/useClients';
import type { NodeRecord } from '@/schemas/node';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const noop = () => {};

const directNode: NodeRecord = {
  id: 1,
  name: 'edge-node-1',
  guid: 'g1',
  scheme: 'https',
  address: '198.51.100.11',
  port: 2053,
  basePath: '/',
  transitive: false,
  enable: true,
  status: 'online',
  totalUp: 536870912,
  totalDown: 1073741824,
  totalTraffic: 1610612736,
};

const transitiveNode: NodeRecord = {
  id: 0,
  name: 'sub-edge',
  guid: 'g2',
  parentGuid: 'g1',
  scheme: 'https',
  address: '10.0.0.2',
  port: 2053,
  transitive: true,
  status: 'online',
};

describe('Node traffic breakdown UI', () => {
  const getSpy = vi.spyOn(HttpUtil, 'get');
  const defaultGet = getSpy.getMockImplementation();

  afterEach(() => {
    if (defaultGet) {
      getSpy.mockImplementation(defaultGet);
    }
  });

  it('shows cumulative traffic in NodeList and opens per-user breakdown modal', async () => {
    const user = userEvent.setup();
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url === '/panel/api/nodes/trafficBreakdown/1') {
        return new Msg(true, '', {
          nodeId: 1,
          nodeName: 'edge-node-1',
          totalUp: 536870912,
          totalDown: 1073741824,
          totalTraffic: 1610612736,
          clients: [
            { email: 'alice', up: 268435456, down: 805306368, total: 1073741824 },
            { email: 'carol', up: 268435456, down: 268435456, total: 536870912 },
          ],
        });
      }
      return new Msg(true, '', {});
    });

    renderWithProviders(
      <NodeList
        nodes={[directNode, transitiveNode]}
        isMobile={false}
        selectedIds={[]}
        onSelectionChange={noop}
        onAdd={noop}
        onMtls={noop}
        onEdit={noop}
        onDelete={noop}
        onProbe={noop}
        onToggleEnable={noop}
        onUpdateNode={noop}
        onUpdateSelected={noop}
      />,
    );

    const trigger = screen.getByRole('button', { name: /edge-node-1.*1\.50 GB/i });
    await user.click(trigger);

    await waitFor(() => {
      expect(screen.getByText('alice')).toBeTruthy();
      expect(screen.getByText('carol')).toBeTruthy();
    });
  });

  it('displays per-node traffic breakdown inside ClientInfoModal', async () => {
    const client = {
      id: '1',
      email: 'alice',
      subId: 'sub-1',
      enable: true,
      inboundIds: [1],
      traffic: { up: 600000, down: 1400000 },
    } as unknown as ClientRecord;

    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/clients/subLinks/')) return new Msg(true, '', []);
      if (url === '/panel/api/clients/nodeTraffics/alice') {
        return new Msg(true, '', [
          { nodeId: 0, nodeName: '', up: 100000, down: 400000, total: 500000 },
          { nodeId: 1, nodeName: 'edge-node-1', up: 300000, down: 700000, total: 1000000 },
          { nodeId: 2, nodeName: 'edge-node-2', up: 200000, down: 300000, total: 500000 },
        ]);
      }
      return new Msg(true, '', {});
    });

    renderWithProviders(
      <ClientInfoModal
        open
        client={client}
        inboundsById={{}}
        isOnline={false}
        onOpenChange={noop}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/Local panel/)).toBeTruthy();
      expect(screen.getByText(/edge-node-1/)).toBeTruthy();
      expect(screen.getByText(/edge-node-2/)).toBeTruthy();
    });
  });
});
