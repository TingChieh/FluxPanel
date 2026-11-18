export const appName = 'FluxPanel';
export const appTagline = 'Lightweight multi-node web panel for managing Xray-core servers';

export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

// The FluxPanel product repository — used for the navbar GitHub link,
// build-time star/release stats, and install commands.
export const productRepo = {
  user: 'TingChieh',
  repo: 'FluxPanel',
  branch: 'main',
};

// Where these docs live in the FluxPanel monorepo — used for "Edit on GitHub" links.
export const gitConfig = {
  user: 'TingChieh',
  repo: 'FluxPanel',
  branch: 'main',
  docsDir: 'docs/content/docs',
};

export const productRepoUrl = `https://github.com/${productRepo.user}/${productRepo.repo}`;

// AI-generated interactive wiki of the FluxPanel codebase.
export const deepWikiUrl = `https://deepwiki.com/${productRepo.user}/${productRepo.repo}`;

// Project updates and issue tracker live on GitHub.
export const telegramChannel = '';
export const telegramChannelUrl = productRepoUrl;

export const donateUrl = '';

// Public site origin, used for metadataBase / canonical URLs / OG images.
export const siteUrl = process.env.NEXT_PUBLIC_SITE_URL || productRepoUrl;
