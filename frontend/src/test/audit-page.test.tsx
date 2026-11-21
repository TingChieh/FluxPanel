import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import AuditPage from '@/pages/audit/AuditPage';
import { HttpUtil } from '@/utils';
import { renderWithProviders } from './test-utils';

interface SentRequest {
  url: string;
  contentType: string | null;
  body: string;
}

const jsonResponse = (obj: unknown) =>
  new Response(JSON.stringify({ success: true, msg: '', obj }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });

const SOURCES = [
  { nodeId: 0, name: 'local', totalHits: 10, activeClients: 1, lastSyncAt: 0, syncError: '' },
  {
    nodeId: 3,
    name: 'hk-edge',
    totalHits: 5,
    activeClients: 1,
    lastSyncAt: 1763540000,
    syncError: '',
  },
];

const SSH_ALERT = {
  id: 7,
  createdAt: 1763540000,
  date: '2026-11-19',
  email: 'ops',
  alertType: 'high_risk_port',
  severity: 1,
  ruleName: 'port:ssh',
  target: 'box.example.net:22',
  srcIp: '198.51.100.1',
  hitCount: 3,
  detail: 'Matched rule port:ssh on box.example.net:22',
  acked: false,
};

function stubAuditBackend(
  sent: SentRequest[],
  summaryUrls: string[] = [],
  clients: Record<string, unknown>[] = [],
) {
  let trusted: string[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.endsWith('/csrf-token')) return jsonResponse('test-csrf-token');
    if (init?.method === 'POST' && url.startsWith('/panel/api/audit/')) {
      sent.push({
        url,
        contentType: new Headers(init.headers).get('Content-Type'),
        body: String(init.body),
      });
      if (url === `/panel/api/audit/alerts/trust/${SSH_ALERT.id}`) trusted = ['box.example.net'];
      return jsonResponse(null);
    }
    if (url.startsWith('/panel/api/audit/config')) {
      return jsonResponse({
        enabled: true,
        retentionDays: 30,
        maxDailySubnets: 8,
        maxConcurrentSubnets: 3,
        minSharingHits: 5,
        minConcurrentWindows: 2,
        sensitiveDomains: [],
        sensitiveKeywords: [],
        ignoreNoiseDomains: [],
        trustedTargets: trusted,
        trustedClients: [],
      });
    }
    if (url.startsWith('/panel/api/audit/alerts')) {
      return jsonResponse(trusted.length ? [] : [SSH_ALERT]);
    }
    if (url.startsWith('/panel/api/audit/telegram')) {
      return jsonResponse({ enabled: true, chatId: '4242', hasBotToken: true });
    }
    if (url.startsWith('/panel/api/audit/digest')) {
      return jsonResponse({
        enabled: true,
        sendAt: '09:00',
        lang: 'zh-CN',
        deliveryNodeId: 3,
        lastSentDate: '',
        lastAttemptAt: 0,
        lastError: '',
      });
    }
    if (url.startsWith('/panel/api/audit/summary')) {
      summaryUrls.push(url);
      const date = new URL(url, 'http://panel').searchParams.get('date') ?? '2026-11-19';
      return jsonResponse({ date, days: 1, clients, sources: SOURCES });
    }
    return jsonResponse([]);
  });
  vi.stubGlobal('fetch', fetchMock);
}

function renderAuditPage() {
  renderWithProviders(
    <MemoryRouter initialEntries={['/audit']}>
      <AuditPage />
    </MemoryRouter>,
  );
}

describe('AuditPage', () => {
  // setup.components.ts stubs HttpUtil globally; the real encoder is what this test pins.
  beforeAll(() => {
    vi.mocked(HttpUtil.get).mockRestore();
    vi.mocked(HttpUtil.post).mockRestore();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  // The Go handler binds with ShouldBindJSON; HttpUtil form-encodes unless told otherwise.
  it('saves rules as the JSON envelope the audit config handler binds', async () => {
    const sent: SentRequest[] = [];
    stubAuditBackend(sent);

    renderAuditPage();
    fireEvent.click(await screen.findByRole('tab', { name: 'Audit Rules' }));
    fireEvent.change(await screen.findByLabelText('Daily Distinct Networks Threshold'), {
      target: { value: '0' },
    });
    fireEvent.change(await screen.findByLabelText('Min Hits per Network'), {
      target: { value: '10' },
    });
    fireEvent.change(await screen.findByLabelText('Sustained 10m Windows'), {
      target: { value: '3' },
    });
    fireEvent.change(
      await screen.findByLabelText('Sensitive / Restricted Domains (one per line)'),
      {
        target: { value: 'restricted.example\n  news.blocked.test  \n' },
      },
    );
    fireEvent.click(screen.getByRole('button', { name: /Save & Re-evaluate History/ }));

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].url).toBe('/panel/api/audit/config');
    expect(sent[0].contentType).toMatch(/^application\/json/);
    expect(JSON.parse(sent[0].body)).toEqual({
      config: {
        enabled: true,
        retentionDays: 30,
        maxDailySubnets: 0,
        maxConcurrentSubnets: 3,
        minSharingHits: 10,
        minConcurrentWindows: 3,
        sensitiveDomains: ['restricted.example', 'news.blocked.test'],
        sensitiveKeywords: [],
        ignoreNoiseDomains: [],
        trustedTargets: [],
        trustedClients: [],
      },
      reevaluate: true,
    });
  });

  // Audit days are bucketed in the panel's zone, which can be a calendar day behind the browser.
  it('takes "today" from the server and derives "yesterday" from it', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date(2026, 10, 20, 1, 30));
    const summaryUrls: string[] = [];
    stubAuditBackend([], summaryUrls);

    renderAuditPage();
    await waitFor(() => expect(summaryUrls).toHaveLength(1));
    expect(new URL(summaryUrls[0], 'http://panel').searchParams.get('date')).toBeNull();

    fireEvent.click(await screen.findByText('Yesterday'));
    await waitFor(() => expect(summaryUrls).toHaveLength(2));
    expect(new URL(summaryUrls[1], 'http://panel').searchParams.get('date')).toBe('2026-11-18');
  });

  // The token is write-only: an untouched field must send "" so the server keeps
  // the stored token, and the body must be JSON for ShouldBindJSON.
  it('saves Telegram settings as JSON without resending the stored token', async () => {
    const sent: SentRequest[] = [];
    stubAuditBackend(sent);

    renderAuditPage();
    fireEvent.click(await screen.findByRole('tab', { name: 'Notifications' }));
    const chat = await screen.findByLabelText('Chat ID');
    await waitFor(() => expect((chat as HTMLInputElement).value).toBe('4242'));
    fireEvent.change(chat, { target: { value: '777' } });
    fireEvent.click(screen.getAllByRole('button', { name: /^Save$/ })[0]);

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].url).toBe('/panel/api/audit/telegram');
    expect(sent[0].contentType).toMatch(/^application\/json/);
    expect(JSON.parse(sent[0].body)).toEqual({ enabled: true, chatId: '777', botToken: '' });
  });

  it('narrows the overview to the selected source node', async () => {
    const summaryUrls: string[] = [];
    stubAuditBackend([], summaryUrls);

    renderAuditPage();
    const filter = await screen.findByRole('combobox', { name: 'Source' });
    await waitFor(() => expect(summaryUrls.length).toBeGreaterThan(0));
    expect(new URL(summaryUrls[0], 'http://panel').searchParams.get('nodeId')).toBeNull();

    fireEvent.mouseDown(filter.closest('.ant-select') ?? filter);
    const option = await waitFor(() => {
      const found = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
        (item) => item.textContent === 'hk-edge',
      );
      if (!found) throw new Error('hk-edge option not rendered');
      return found;
    });
    const before = summaryUrls.length;
    fireEvent.click(option);

    await waitFor(() => expect(summaryUrls.length).toBeGreaterThan(before));
    expect(
      new URL(summaryUrls[summaryUrls.length - 1], 'http://panel').searchParams.get('nodeId'),
    ).toBe('3');
  });

  // A one-click trust must reach the handler as JSON and show up in the rules
  // the admin edits, or the next "Save rules" would silently drop it again.
  it('trusts a host from an alert row and reloads the trusted targets', async () => {
    const sent: SentRequest[] = [];
    stubAuditBackend(sent);

    renderAuditPage();
    fireEvent.click(await screen.findByRole('tab', { name: /Security Alerts/ }));
    fireEvent.click(await screen.findByRole('button', { name: /Trust/ }));
    fireEvent.click(await screen.findByText('Trust this host on any port'));

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].url).toBe('/panel/api/audit/alerts/trust/7');
    expect(sent[0].contentType).toMatch(/^application\/json/);
    expect(JSON.parse(sent[0].body)).toEqual({ scope: 'host' });

    fireEvent.click(screen.getByRole('tab', { name: 'Audit Rules' }));
    const trustedTargets = await screen.findByLabelText('Trusted targets (one per line)');
    await waitFor(() =>
      expect((trustedTargets as HTMLTextAreaElement).value).toBe('box.example.net'),
    );
  });

  // Overview badges and metric highlights must separate high-risk concurrent
  // sharing from daily-only warnings and never mislabel trusted clients.
  it('distinguishes concurrent sharing, daily multi-network warning, and normal rows', async () => {
    stubAuditBackend(
      [],
      [],
      [
        {
          email: 'trusted-roamer',
          totalHits: 120,
          uniqueRootDomains: 5,
          uniqueIps: 4,
          uniqueSubnets: 3,
          maxConcurrentSubnets: 3,
          sharingAlert: false,
          concurrentSharingAlert: false,
          dailySharingAlert: false,
          highRiskHits: 0,
          warnRiskHits: 0,
          unackedAlerts: 0,
          topDomains: ['github.com'],
          lastSeen: 1763540000,
          nodeIds: [0],
        },
        {
          email: 'daily-roamer',
          totalHits: 180,
          uniqueRootDomains: 7,
          uniqueIps: 9,
          uniqueSubnets: 8,
          maxConcurrentSubnets: 1,
          sharingAlert: true,
          concurrentSharingAlert: false,
          dailySharingAlert: true,
          highRiskHits: 0,
          warnRiskHits: 0,
          unackedAlerts: 1,
          topDomains: ['openai.com'],
          lastSeen: 1763540000,
          nodeIds: [0],
        },
        {
          email: 'real-sharer',
          totalHits: 240,
          uniqueRootDomains: 8,
          uniqueIps: 5,
          uniqueSubnets: 4,
          maxConcurrentSubnets: 3,
          sharingAlert: true,
          concurrentSharingAlert: true,
          dailySharingAlert: false,
          highRiskHits: 0,
          warnRiskHits: 0,
          unackedAlerts: 1,
          topDomains: ['youtube.com'],
          lastSeen: 1763540000,
          nodeIds: [0],
        },
      ],
    );

    renderAuditPage();
    const trustedRow = (await screen.findByText('trusted-roamer')).closest('tr');
    const dailyRow = (await screen.findByText('daily-roamer')).closest('tr');
    const sharerRow = (await screen.findByText('real-sharer')).closest('tr');

    expect(trustedRow?.textContent).toContain('Normal');
    expect(trustedRow?.textContent).not.toContain('Concurrent Sharing');
    expect(trustedRow?.textContent).not.toContain('Daily Multi-Network');
    expect(trustedRow?.querySelector('.ant-tag-red')).toBeNull();

    expect(dailyRow?.textContent).toContain('Daily Multi-Network');
    expect(dailyRow?.textContent).not.toContain('Concurrent Sharing');
    expect(dailyRow?.querySelector('.ant-tag-red')).toBeNull();
    expect(dailyRow?.querySelectorAll('.ant-tag-gold').length).toBeGreaterThanOrEqual(2);

    expect(sharerRow?.textContent).toContain('Concurrent Sharing');
    expect(sharerRow?.querySelector('.ant-tag-red')?.textContent).toBe('3');
  });

  // maxDailySubnets=1 is rejected rather than silently rewritten to 2.
  it('blocks saving when maxDailySubnets is set to 1 and shows validation help', async () => {
    const sent: SentRequest[] = [];
    stubAuditBackend(sent);

    renderAuditPage();
    fireEvent.click(await screen.findByRole('tab', { name: 'Audit Rules' }));
    fireEvent.change(await screen.findByLabelText('Daily Distinct Networks Threshold'), {
      target: { value: '1' },
    });

    const help = await screen.findByText('Enter 0 (disabled) or an integer between 2 and 100.');
    expect(help.textContent).toContain('between 2 and 100');
    const saveBtn = screen.getByRole('button', { name: /Save Rules/ }) as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);
    fireEvent.click(saveBtn);
    expect(sent).toHaveLength(0);
  });
});
