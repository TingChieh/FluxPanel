import { screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { PALETTE_KEYWORDS } from '@/components/command-palette/CommandPalette';
import IndexPage from '@/pages/index/IndexPage';
import LoginPage from '@/pages/login/LoginPage';
import { PANEL_ROUTE_PATHS } from '@/routes/panelRoutes';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => null }));
vi.mock('@/components/viz', () => ({ Sparkline: () => null }));

describe('Sponsor ads removal', () => {
  const getSpy = vi.spyOn(HttpUtil, 'get');
  const postSpy = vi.spyOn(HttpUtil, 'post');
  const defaultGet = getSpy.getMockImplementation();
  const defaultPost = postSpy.getMockImplementation();

  afterEach(() => {
    if (defaultGet) getSpy.mockImplementation(defaultGet);
    if (defaultPost) postSpy.mockImplementation(defaultPost);
  });

  it('removes /sponsors from panel routes and command palette keywords', () => {
    expect(PANEL_ROUTE_PATHS).not.toContain('/sponsors');
    expect(PALETTE_KEYWORDS).not.toHaveProperty('/sponsors');
  });

  it('does not fetch or render sponsors on the login page', async () => {
    const requestedUrls: string[] = [];
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      requestedUrls.push(url);
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', false));

    const { container } = renderWithProviders(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    );

    await screen.findByRole('button', { name: 'Log In' });
    expect(requestedUrls.some((url) => url.includes('sponsor'))).toBe(false);
    expect(container.querySelector('.sponsor-card')).toBeNull();
    expect(container.querySelector('.login-sponsor')).toBeNull();
  });

  it('does not fetch or render sponsors on the dashboard page', async () => {
    const requestedUrls: string[] = [];
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      requestedUrls.push(url);
      if (url === '/panel/api/server/status') {
        return new Msg(true, '', {
          cpu: 10,
          cpuCores: 2,
          logicalPro: 2,
          cpuSpeedMhz: 2400,
          mem: { current: 512, total: 1024 },
          swap: { current: 0, total: 0 },
          disk: { current: 1000, total: 10000 },
          xray: { state: 'running', version: '26.9.8' },
          uptime: 100,
          loads: [0.1, 0.1, 0.1],
          tcpCount: 5,
          udpCount: 1,
          netIO: { up: 0, down: 0 },
          netTraffic: { sent: 0, recv: 0 },
          publicIP: { ipv4: 'N/A', ipv6: 'N/A' },
          appStats: { threads: 1, mem: 100, uptime: 100 },
        });
      }
      return new Msg(true, '', {});
    });

    const { container } = renderWithProviders(
      <MemoryRouter>
        <IndexPage />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(requestedUrls).toContain('/panel/api/server/status');
    });
    expect(requestedUrls.some((url) => url.includes('sponsor'))).toBe(false);
    expect(container.querySelector('.sponsor-card')).toBeNull();
  });
});
