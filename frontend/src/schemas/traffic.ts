import { z } from 'zod';
import {
  LiveSpeedSchema,
  TrafficClientBreakdownItemSchema,
  TrafficClientDetailResponseSchema,
  TrafficClientRankItemSchema,
  TrafficClientsPageResponseSchema,
  TrafficNodeBreakdownItemSchema,
  TrafficNodeDetailResponseSchema,
  TrafficNodeRankItemSchema,
  TrafficOverviewResponseSchema,
  TrafficOverviewSummarySchema,
  TrafficSeriesPointSchema,
  TrafficSummaryMetricsSchema,
} from '@/generated/zod';

export {
  LiveSpeedSchema,
  TrafficClientBreakdownItemSchema,
  TrafficClientDetailResponseSchema,
  TrafficClientRankItemSchema,
  TrafficClientsPageResponseSchema,
  TrafficNodeBreakdownItemSchema,
  TrafficNodeDetailResponseSchema,
  TrafficNodeRankItemSchema,
  TrafficOverviewResponseSchema,
  TrafficOverviewSummarySchema,
  TrafficSeriesPointSchema,
  TrafficSummaryMetricsSchema,
};

export const TrafficPeriodSchema = z.enum(['day', 'week', 'month']);
export const TrafficSortSchema = z.enum(['traffic', 'speed']);

export type TrafficPeriod = z.infer<typeof TrafficPeriodSchema>;
export type TrafficSort = z.infer<typeof TrafficSortSchema>;
export type LiveSpeed = z.infer<typeof LiveSpeedSchema>;
export type TrafficSeriesPoint = z.infer<typeof TrafficSeriesPointSchema>;
export type TrafficSummaryMetrics = z.infer<typeof TrafficSummaryMetricsSchema>;
export type TrafficOverviewSummary = z.infer<typeof TrafficOverviewSummarySchema>;
export type TrafficClientRankItem = z.infer<typeof TrafficClientRankItemSchema>;
export type TrafficNodeRankItem = z.infer<typeof TrafficNodeRankItemSchema>;
export type TrafficOverviewResponse = z.infer<typeof TrafficOverviewResponseSchema>;
export type TrafficClientsPageResponse = z.infer<typeof TrafficClientsPageResponseSchema>;
export type TrafficNodeBreakdownItem = z.infer<typeof TrafficNodeBreakdownItemSchema>;
export type TrafficClientDetailResponse = z.infer<typeof TrafficClientDetailResponseSchema>;
export type TrafficClientBreakdownItem = z.infer<typeof TrafficClientBreakdownItemSchema>;
export type TrafficNodeDetailResponse = z.infer<typeof TrafficNodeDetailResponseSchema>;
