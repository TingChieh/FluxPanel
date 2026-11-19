import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { Suspense } from 'react';
import { MemoryRouter } from 'react-router';
import { waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { PANEL_ROUTES } from '@/routes/panelRoutes';
import { renderWithProviders } from './test-utils';

const pageShellCss = readFileSync(resolve(process.cwd(), 'src/styles/page-shell.css'), 'utf8');

// Swagger UI and uPlot's canvas cannot render under jsdom; only the shell around them is checked.
vi.mock('swagger-ui-react', () => ({ default: () => null }));
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

// Pages draw their own shell, so a page that skips it loses the sidebar and the
// page-level dark palette while every other page still looks fine.
describe('panel page shell', () => {
  for (const route of PANEL_ROUTES) {
    it(`${route.path} renders the sidebar inside a themed page root`, async () => {
      const Page = route.component;
      const { container } = renderWithProviders(
        <MemoryRouter initialEntries={[route.path]}>
          <Suspense fallback={null}>
            <Page />
          </Suspense>
        </MemoryRouter>,
      );

      await waitFor(() => expect(container.querySelector('.sider-brand')).not.toBeNull(), {
        timeout: 5000,
      });
      const root = container.querySelector('.ant-layout');
      const pageClass = root?.className.match(/\b([a-z-]+-page)\b/)?.[1];
      expect(pageClass).toBeTruthy();
      expect(root?.classList.contains('is-dark')).toBe(true);
      expect(pageShellCss).toContain(`.${pageClass}.is-dark`);
    });
  }
});
