export const keys = {
  server: {
    status: () => ['server', 'status'] as const,
    fail2banStatus: () => ['server', 'fail2banStatus'] as const,
  },
  nodes: {
    root: () => ['nodes'] as const,
    list: () => ['nodes', 'list'] as const,
  },
  hosts: {
    root: () => ['hosts'] as const,
    list: () => ['hosts', 'list'] as const,
    raw: () => ['hosts', 'raw'] as const,
    byInbound: (inboundId: number) => ['hosts', 'byInbound', inboundId] as const,
    tags: () => ['hosts', 'tags'] as const,
  },
  subBalancers: {
    root: () => ['sub-balancers'] as const,
    list: () => ['sub-balancers', 'list'] as const,
  },
  settings: {
    root: () => ['settings'] as const,
    all: () => ['settings', 'all'] as const,
    defaults: () => ['settings', 'defaults'] as const,
    factoryDefaults: () => ['settings', 'factoryDefaults'] as const,
  },
  inbounds: {
    root: () => ['inbounds'] as const,
    slim: () => ['inbounds', 'slim'] as const,
    options: () => ['inbounds', 'options'] as const,
  },
  clients: {
    root: () => ['clients'] as const,
    list: (params: unknown) => ['clients', 'list', params] as const,
    all: () => ['clients', 'all'] as const,
    onlines: () => ['clients', 'onlines'] as const,
    onlinesByGuid: () => ['clients', 'onlinesByGuid'] as const,
    activeInbounds: () => ['clients', 'activeInbounds'] as const,
    lastOnline: () => ['clients', 'lastOnline'] as const,
    groups: () => ['clients', 'groups'] as const,
  },
  traffic: {
    root: () => ['traffic'] as const,
    overview: (period: string, anchor: string) => ['traffic', 'overview', period, anchor] as const,
    clients: (params: unknown) => ['traffic', 'clients', params] as const,
    clientDetail: (email: string, period: string, anchor: string) =>
      ['traffic', 'clientDetail', email, period, anchor] as const,
    nodeDetail: (
      nodeId: number,
      period: string,
      anchor: string,
      search: string,
      page: number,
      pageSize: number,
    ) => ['traffic', 'nodeDetail', nodeId, period, anchor, search, page, pageSize] as const,
  },
  xray: {
    root: () => ['xray'] as const,
    config: () => ['xray', 'config'] as const,
    outboundsTraffic: () => ['xray', 'outboundsTraffic'] as const,
    geodata: {
      root: () => ['xray', 'geodata'] as const,
      files: () => ['xray', 'geodata', 'files'] as const,
      categories: (file: string, query: string) =>
        ['xray', 'geodata', 'categories', file, query] as const,
      entries: (file: string, code: string, query: string, offset: number, limit: number) =>
        ['xray', 'geodata', 'entries', file, code, query, offset, limit] as const,
    },
  },
} as const;
