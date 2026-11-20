import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import TrafficPage from '@/pages/traffic/TrafficPage';
import { ThemeProvider } from '@/hooks/useTheme';
import { HttpUtil, Msg } from '@/utils';
import type {
  TrafficClientDetailResponse,
  TrafficClientsPageResponse,
  TrafficNodeDetailResponse,
  TrafficOverviewResponse,
} from '@/schemas/traffic';

const wsListeners = vi.hoisted(() => new Map<string, Set<(...args: unknown[]) => void>>());

vi.mock('@/api/websocket', () => ({
  getSharedWebSocketClient: () => ({
    isConnected: true,
    connect: vi.fn(),
    on: (ev: string, fn: (...args: unknown[]) => void) => {
      const set = wsListeners.get(ev) ?? new Set();
      set.add(fn);
      wsListeners.set(ev, set);
    },
    off: (ev: string, fn: (...args: unknown[]) => void) => {
      wsListeners.get(ev)?.delete(fn);
    },
  }),
}));

vi.mock('@/layouts/AppSidebar', () => ({
  default: () => <nav data-testid="sidebar" />,
}));

vi.mock('uplot', () => ({
  default: class {
    static paths = { spline: () => undefined };
    static pxRatio = 1;
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

function makeOverview(overrides: Partial<TrafficOverviewResponse> = {}): TrafficOverviewResponse {
  return {
    period: 'day',
    anchor: '2026-11-20',
    today: '2026-11-20',
    timezone: 'Asia/Shanghai',
    rangeStart: '2026-11-20',
    rangeEnd: '2026-11-20',
    bucketUnit: 'hour',
    hourlyRetentionCutoff: '2026-08-22',
    summary: {
      totalUp: 1024 * 1024 * 100,
      totalDown: 1024 * 1024 * 900,
      total: 1024 * 1024 * 1000,
      prevTotal: 1024 * 1024 * 800,
      changePct: 25,
      activeClients: 2,
      totalClients: 5,
      topClientEmail: 'alice',
      topClientTotal: 1024 * 1024 * 600,
      onlineNodes: 2,
      totalNodes: 2,
      topNodeId: 1,
      topNodeName: 'HK-01',
      topNodeTotal: 1024 * 1024 * 700,
      liveUpSpeed: 1024 * 100,
      liveDownSpeed: 1024 * 900,
      hasHistory: true,
      hourlyExpired: false,
    },
    series: [
      {
        timestamp: 1763600000,
        label: '08:00',
        up: 1024 * 1024 * 40,
        down: 1024 * 1024 * 400,
        total: 1024 * 1024 * 440,
        future: false,
      },
      {
        timestamp: 1763603600,
        label: '09:00',
        up: 1024 * 1024 * 60,
        down: 1024 * 1024 * 500,
        total: 1024 * 1024 * 560,
        future: false,
      },
    ],
    nodes: [
      {
        nodeId: 1,
        nodeName: 'HK-01',
        status: 'online',
        up: 1024 * 1024 * 70,
        down: 1024 * 1024 * 630,
        total: 1024 * 1024 * 700,
        sharePct: 70,
        upSpeed: 1024 * 80,
        downSpeed: 1024 * 700,
        deleted: false,
      },
      {
        nodeId: 0,
        nodeName: 'Local',
        status: 'online',
        up: 1024 * 1024 * 30,
        down: 1024 * 1024 * 270,
        total: 1024 * 1024 * 300,
        sharePct: 30,
        upSpeed: 1024 * 20,
        downSpeed: 1024 * 200,
        deleted: false,
      },
    ],
    ...overrides,
  };
}

function makeClientsPage(): TrafficClientsPageResponse {
  return {
    total: 2,
    page: 1,
    pageSize: 25,
    periodTotal: 1024 * 1024 * 1000,
    items: [
      {
        email: 'alice',
        up: 1024 * 1024 * 60,
        down: 1024 * 1024 * 540,
        total: 1024 * 1024 * 600,
        sharePct: 60,
        online: true,
        upSpeed: 1024 * 50,
        downSpeed: 1024 * 500,
        deleted: false,
      },
      {
        email: 'bob',
        up: 1024 * 1024 * 40,
        down: 1024 * 1024 * 360,
        total: 1024 * 1024 * 400,
        sharePct: 40,
        online: false,
        upSpeed: 0,
        downSpeed: 0,
        deleted: false,
      },
    ],
  };
}

function makeClientDetail(): TrafficClientDetailResponse {
  return {
    email: 'alice',
    online: true,
    deleted: false,
    upSpeed: 1024 * 50,
    downSpeed: 1024 * 500,
    summary: {
      totalUp: 1024 * 1024 * 60,
      totalDown: 1024 * 1024 * 540,
      total: 1024 * 1024 * 600,
      prevTotal: 1024 * 1024 * 500,
      changePct: 20,
    },
    series: makeOverview().series,
    nodes: [
      {
        nodeId: 1,
        nodeName: 'HK-01',
        status: 'online',
        up: 1024 * 1024 * 50,
        down: 1024 * 1024 * 450,
        total: 1024 * 1024 * 500,
        sharePct: 83.33,
        deleted: false,
      },
    ],
  };
}

function makeNodeDetail(): TrafficNodeDetailResponse {
  return {
    nodeId: 1,
    nodeName: 'HK-01',
    status: 'online',
    deleted: false,
    upSpeed: 1024 * 80,
    downSpeed: 1024 * 700,
    summary: {
      totalUp: 1024 * 1024 * 70,
      totalDown: 1024 * 1024 * 630,
      total: 1024 * 1024 * 700,
      prevTotal: 1024 * 1024 * 600,
      changePct: 16.67,
    },
    series: makeOverview().series,
    clientsTotal: 2,
    clients: [
      {
        email: 'alice',
        online: true,
        up: 1024 * 1024 * 50,
        down: 1024 * 1024 * 450,
        total: 1024 * 1024 * 500,
        sharePct: 71.43,
        deleted: false,
        unattributed: false,
      },
      {
        email: '',
        online: false,
        up: 1024 * 1024 * 20,
        down: 1024 * 1024 * 180,
        total: 1024 * 1024 * 200,
        sharePct: 28.57,
        deleted: false,
        unattributed: true,
      },
    ],
  };
}

function renderTrafficPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <ThemeProvider>
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/traffic']}>
          <TrafficPage />
        </MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>,
  );
}

describe('TrafficPage', () => {
  beforeEach(() => {
    wsListeners.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders KPIs, dual rankings, period switching, live WS updates, and cross-drill drawer', async () => {
    const requestedUrls: string[] = [];
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      requestedUrls.push(url);
      if (url.startsWith('/panel/api/traffic/overview')) {
        return new Msg(true, '', makeOverview());
      }
      if (url.startsWith('/panel/api/traffic/clients')) {
        return new Msg(true, '', makeClientsPage());
      }
      if (url.startsWith('/panel/api/traffic/client/')) {
        return new Msg(true, '', makeClientDetail());
      }
      if (url.startsWith('/panel/api/traffic/node/')) {
        return new Msg(true, '', makeNodeDetail());
      }
      return new Msg(true, '', {});
    });

    renderTrafficPage();

    await waitFor(() => {
      expect(screen.getAllByText('alice').length).toBeGreaterThan(0);
      expect(screen.getAllByText('HK-01').length).toBeGreaterThan(0);
      expect(screen.getByText('+25%')).toBeTruthy();
    });

    // Switch period to week and confirm overview refetches with period=week.
    fireEvent.click(screen.getByText('Week'));
    await waitFor(() => {
      expect(requestedUrls.some((u) => u.includes('/overview?period=week'))).toBe(true);
    });

    // Search + sort by live speed + activeOnly toggle forward exact query params to /clients.
    fireEvent.change(screen.getByPlaceholderText('Search client email...'), {
      target: { value: 'ali' },
    });
    fireEvent.click(screen.getAllByText('By Live Speed')[0]);
    fireEvent.click(screen.getByRole('switch'));
    await waitFor(() => {
      expect(
        requestedUrls.some(
          (u) =>
            u.includes('/panel/api/traffic/clients?') &&
            u.includes('search=ali') &&
            u.includes('sort=speed') &&
            u.includes('activeOnly=true'),
        ),
      ).toBe(true);
    });

    // Push live WS speeds and verify total live speed updates in place (5 MB/s total).
    act(() => {
      for (const fn of wsListeners.get('traffic') ?? []) {
        fn({
          liveTotalSpeed: { up: 1024 * 1024 * 1, down: 1024 * 1024 * 4 },
          liveUserSpeeds: { bob: { up: 1024 * 1024 * 1, down: 1024 * 1024 * 4 } },
          liveNodeSpeeds: { '1': { up: 1024 * 1024 * 1, down: 1024 * 1024 * 4 } },
          onlineClients: ['bob'],
        });
      }
    });
    await waitFor(() => {
      expect(screen.getByText('5.00 MB/s')).toBeTruthy();
    });

    // Click node row to open Node Detail Drawer, verify unattributed row, then jump to client drawer.
    fireEvent.click(screen.getByTestId('traffic-node-row-1'));
    await waitFor(() => {
      expect(screen.getByText('Other / Unattributed')).toBeTruthy();
    });
    fireEvent.click(screen.getByTestId('drawer-node-client-alice'));
    await waitFor(() => {
      expect(requestedUrls.some((u) => u.includes('/panel/api/traffic/client/alice'))).toBe(true);
    });
  });

  it('shows cold-start notice and hourly-expired banner when flagged by backend', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.startsWith('/panel/api/traffic/overview')) {
        return new Msg(
          true,
          '',
          makeOverview({
            summary: {
              ...makeOverview().summary,
              hasHistory: false,
              hourlyExpired: true,
              total: 0,
            },
          }),
        );
      }
      return new Msg(true, '', {
        total: 0,
        page: 1,
        pageSize: 25,
        periodTotal: 0,
        items: [],
      });
    });

    renderTrafficPage();

    await waitFor(() => {
      expect(screen.getByText(/Traffic history collection starts from now/i)).toBeTruthy();
      expect(screen.getByText(/Hourly data is retained for the last 90 days/i)).toBeTruthy();
    });
  });
});
