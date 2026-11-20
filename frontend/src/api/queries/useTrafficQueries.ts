import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { keys } from '@/api/queryKeys';
import {
  TrafficClientDetailResponseSchema,
  TrafficClientsPageResponseSchema,
  TrafficNodeDetailResponseSchema,
  TrafficOverviewResponseSchema,
  type TrafficPeriod,
  type TrafficSort,
} from '@/schemas/traffic';

const LIVE_REFETCH_MS = 30_000;

export interface TrafficClientsParams {
  period: TrafficPeriod;
  anchor: string;
  search: string;
  sort: TrafficSort;
  activeOnly: boolean;
  page: number;
  pageSize: number;
}

function buildParams(entries: Record<string, string | number | boolean | undefined>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(entries)) {
    if (v === undefined || v === '') continue;
    sp.set(k, String(v));
  }
  const qs = sp.toString();
  return qs ? `?${qs}` : '';
}

export function useTrafficOverviewQuery(period: TrafficPeriod, anchor: string, autoRefresh = true) {
  return useQuery({
    queryKey: keys.traffic.overview(period, anchor),
    queryFn: async () => {
      const qs = buildParams({ period, anchor });
      const msg = await HttpUtil.get(`/panel/api/traffic/overview${qs}`);
      const parsed = parseMsg(
        msg,
        TrafficOverviewResponseSchema,
        'GET /panel/api/traffic/overview',
      );
      if (!parsed.success || !parsed.obj) {
        throw new Error(parsed.msg || 'Failed to load traffic overview');
      }
      return parsed.obj;
    },
    placeholderData: keepPreviousData,
    refetchInterval: autoRefresh ? LIVE_REFETCH_MS : false,
  });
}

export function useTrafficClientsQuery(params: TrafficClientsParams, autoRefresh = true) {
  return useQuery({
    queryKey: keys.traffic.clients(params),
    queryFn: async () => {
      const qs = buildParams({
        period: params.period,
        anchor: params.anchor,
        search: params.search || undefined,
        sort: params.sort,
        activeOnly: params.activeOnly ? 'true' : 'false',
        page: params.page,
        pageSize: params.pageSize,
      });
      const msg = await HttpUtil.get(`/panel/api/traffic/clients${qs}`);
      const parsed = parseMsg(
        msg,
        TrafficClientsPageResponseSchema,
        'GET /panel/api/traffic/clients',
      );
      if (!parsed.success || !parsed.obj) {
        throw new Error(parsed.msg || 'Failed to load traffic clients');
      }
      return parsed.obj;
    },
    placeholderData: keepPreviousData,
    refetchInterval: autoRefresh ? LIVE_REFETCH_MS : false,
  });
}

export function useTrafficClientDetailQuery(
  email: string | null,
  period: TrafficPeriod,
  anchor: string,
) {
  return useQuery({
    queryKey: keys.traffic.clientDetail(email ?? '', period, anchor),
    enabled: Boolean(email),
    queryFn: async () => {
      const qs = buildParams({ period, anchor });
      const encoded = encodeURIComponent(email ?? '');
      const msg = await HttpUtil.get(`/panel/api/traffic/client/${encoded}${qs}`);
      const parsed = parseMsg(
        msg,
        TrafficClientDetailResponseSchema,
        'GET /panel/api/traffic/client',
      );
      if (!parsed.success || !parsed.obj) {
        throw new Error(parsed.msg || 'Failed to load client traffic detail');
      }
      return parsed.obj;
    },
    placeholderData: keepPreviousData,
  });
}

export function useTrafficNodeDetailQuery(
  nodeId: number | null,
  period: TrafficPeriod,
  anchor: string,
  search: string,
  page: number,
  pageSize: number,
) {
  return useQuery({
    queryKey: keys.traffic.nodeDetail(nodeId ?? -1, period, anchor, search, page, pageSize),
    enabled: nodeId !== null && nodeId >= 0,
    queryFn: async () => {
      const qs = buildParams({
        period,
        anchor,
        search: search || undefined,
        page,
        pageSize,
      });
      const msg = await HttpUtil.get(`/panel/api/traffic/node/${nodeId}${qs}`);
      const parsed = parseMsg(msg, TrafficNodeDetailResponseSchema, 'GET /panel/api/traffic/node');
      if (!parsed.success || !parsed.obj) {
        throw new Error(parsed.msg || 'Failed to load node traffic detail');
      }
      return parsed.obj;
    },
    placeholderData: keepPreviousData,
  });
}
