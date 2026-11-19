import { fireEvent, screen, waitFor } from '@testing-library/react';
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

function stubAuditBackend(sent: SentRequest[], summaryUrls: string[] = []) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.endsWith('/csrf-token')) return jsonResponse('test-csrf-token');
    if (init?.method === 'POST') {
      sent.push({
        url,
        contentType: new Headers(init.headers).get('Content-Type'),
        body: String(init.body),
      });
      return jsonResponse(null);
    }
    if (url.startsWith('/panel/api/audit/config')) {
      return jsonResponse({
        enabled: true,
        retentionDays: 30,
        maxDailySubnets: 4,
        maxConcurrentSubnets: 3,
        sensitiveDomains: [],
        sensitiveKeywords: [],
        ignoreNoiseDomains: [],
      });
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
      return jsonResponse({ date, days: 1, clients: [], sources: SOURCES });
    }
    return jsonResponse([]);
  });
  vi.stubGlobal('fetch', fetchMock);
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

    renderWithProviders(<AuditPage />);
    fireEvent.click(await screen.findByRole('tab', { name: 'Audit Rules' }));
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
        maxDailySubnets: 4,
        maxConcurrentSubnets: 3,
        sensitiveDomains: ['restricted.example', 'news.blocked.test'],
        sensitiveKeywords: [],
        ignoreNoiseDomains: [],
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

    renderWithProviders(<AuditPage />);
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

    renderWithProviders(<AuditPage />);
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

    renderWithProviders(<AuditPage />);
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
});
