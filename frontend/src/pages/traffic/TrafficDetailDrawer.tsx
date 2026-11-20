import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Drawer, Empty, Input, Pagination, Progress, Spin, Tag, Typography } from 'antd';
import { ClusterOutlined, UserOutlined } from '@ant-design/icons';

import {
  useTrafficClientDetailQuery,
  useTrafficNodeDetailQuery,
} from '@/api/queries/useTrafficQueries';
import TrafficTrendChart from '@/components/viz/TrafficTrendChart';
import { ClientSpeedTag, isActiveSpeed } from '@/components/clients/ClientSpeedTag';
import { SizeFormatter } from '@/utils';
import type { LiveSpeed, TrafficPeriod } from '@/schemas/traffic';

export type TrafficDrawerTarget =
  | { kind: 'client'; email: string }
  | { kind: 'node'; nodeId: number }
  | null;

interface TrafficDetailDrawerProps {
  target: TrafficDrawerTarget;
  period: TrafficPeriod;
  anchor: string;
  liveUserSpeeds: Record<string, LiveSpeed>;
  liveNodeSpeeds: Record<number, LiveSpeed>;
  receivedLive: boolean;
  onClose: () => void;
  onSelectClient: (email: string) => void;
}

function formatDeltaBadge(changePct: number | null | undefined) {
  if (changePct == null) return <Tag>—</Tag>;
  const sign = changePct > 0 ? '+' : '';
  const color = changePct > 0 ? 'green' : changePct < 0 ? 'volcano' : 'default';
  return (
    <Tag color={color}>
      {sign}
      {changePct}%
    </Tag>
  );
}

export default function TrafficDetailDrawer({
  target,
  period,
  anchor,
  liveUserSpeeds,
  liveNodeSpeeds,
  receivedLive,
  onClose,
  onSelectClient,
}: TrafficDetailDrawerProps) {
  const { t } = useTranslation();
  const clientEmail = target?.kind === 'client' ? target.email : null;
  const nodeId = target?.kind === 'node' ? target.nodeId : null;
  const scopeKey = `${nodeId ?? 'none'}:${period}:${anchor}`;

  const [nodeFilter, setNodeFilter] = useState({
    scope: scopeKey,
    input: '',
    query: '',
    page: 1,
  });
  const nodeSearchInput = nodeFilter.scope === scopeKey ? nodeFilter.input : '';
  const nodeSearch = nodeFilter.scope === scopeKey ? nodeFilter.query : '';
  const nodePage = nodeFilter.scope === scopeKey ? nodeFilter.page : 1;

  useEffect(() => {
    const timer = window.setTimeout(() => {
      const trimmed = nodeSearchInput.trim();
      setNodeFilter((prev) =>
        prev.scope === scopeKey && prev.query === trimmed
          ? prev
          : { scope: scopeKey, input: nodeSearchInput, query: trimmed, page: 1 },
      );
    }, 250);
    return () => window.clearTimeout(timer);
  }, [nodeSearchInput, scopeKey]);

  const clientQuery = useTrafficClientDetailQuery(clientEmail, period, anchor);
  const nodeQuery = useTrafficNodeDetailQuery(nodeId, period, anchor, nodeSearch, nodePage, 20);

  const isClient = target?.kind === 'client';
  const clientData = clientQuery.data;
  const nodeData = nodeQuery.data;

  const clientLiveSpeed: LiveSpeed =
    clientEmail && receivedLive
      ? (liveUserSpeeds[clientEmail] ?? { up: 0, down: 0 })
      : { up: clientData?.upSpeed ?? 0, down: clientData?.downSpeed ?? 0 };

  const nodeLiveSpeed: LiveSpeed =
    nodeId !== null && receivedLive
      ? (liveNodeSpeeds[nodeId] ?? { up: 0, down: 0 })
      : { up: nodeData?.upSpeed ?? 0, down: nodeData?.downSpeed ?? 0 };

  const drawerTitle = isClient ? (
    <span className="traffic-drawer-title">
      <UserOutlined aria-hidden />
      <span>{clientEmail}</span>
      {clientData?.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
    </span>
  ) : target?.kind === 'node' ? (
    <span className="traffic-drawer-title">
      <ClusterOutlined aria-hidden />
      <span>
        {target.nodeId === 0
          ? t('pages.traffic.localNode')
          : (nodeData?.nodeName ?? `Node #${target.nodeId}`)}
      </span>
      {nodeData?.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
    </span>
  ) : (
    t('pages.traffic.title')
  );

  const loading = isClient ? clientQuery.isLoading : nodeQuery.isLoading;

  return (
    <Drawer
      open={target !== null}
      onClose={onClose}
      title={drawerTitle}
      size="large"
      destroyOnHidden
      aria-label={
        isClient ? t('pages.traffic.clientDetailTitle') : t('pages.traffic.nodeDetailTitle')
      }
    >
      {loading ? (
        <div className="traffic-drawer-loading">
          <Spin />
        </div>
      ) : isClient && clientData ? (
        <div className="traffic-drawer-body">
          <div className="traffic-drawer-summary">
            <div className="traffic-drawer-metric">
              <Typography.Text type="secondary">{t('pages.traffic.total')}</Typography.Text>
              <div className="traffic-drawer-metric-main">
                <strong>{SizeFormatter.sizeFormat(clientData.summary.total)}</strong>
                {formatDeltaBadge(clientData.summary.changePct)}
              </div>
              <Typography.Text type="secondary" className="traffic-sub-line">
                ↑ {SizeFormatter.sizeFormat(clientData.summary.totalUp)} / ↓{' '}
                {SizeFormatter.sizeFormat(clientData.summary.totalDown)}
              </Typography.Text>
            </div>
            <div className="traffic-drawer-metric">
              <Typography.Text type="secondary">{t('pages.traffic.liveSpeedCard')}</Typography.Text>
              <div className="traffic-drawer-metric-main">
                {isActiveSpeed(clientLiveSpeed) ? (
                  <ClientSpeedTag speed={clientLiveSpeed} />
                ) : (
                  <Typography.Text type="secondary">{t('pages.traffic.idle')}</Typography.Text>
                )}
              </div>
            </div>
          </div>

          <div className="traffic-drawer-section">
            <Typography.Title level={5}>{t('pages.traffic.trendTitle')}</Typography.Title>
            <TrafficTrendChart series={clientData.series} height={200} />
          </div>

          <div className="traffic-drawer-section">
            <Typography.Title level={5}>{t('pages.traffic.nodeBreakdown')}</Typography.Title>
            {clientData.nodes.length === 0 ? (
              <Empty
                description={t('pages.traffic.noBreakdownData')}
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ) : (
              <div className="traffic-rank-list">
                {clientData.nodes.map((item) => (
                  <div key={item.nodeId} className="traffic-rank-row is-static">
                    <div className="traffic-rank-row-top">
                      <span className="traffic-rank-name">
                        <span className={`traffic-status-dot is-${item.status}`} />
                        <span>
                          {item.nodeId === 0 ? t('pages.traffic.localNode') : item.nodeName}
                        </span>
                        {item.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
                      </span>
                      <span className="traffic-rank-total">
                        <strong>{SizeFormatter.sizeFormat(item.total)}</strong>
                        <span className="traffic-rank-share">
                          {t('pages.traffic.share')} {item.sharePct}%
                        </span>
                      </span>
                    </div>
                    <div className="traffic-rank-row-sub">
                      <span>
                        ↑ {SizeFormatter.sizeFormat(item.up)} / ↓{' '}
                        {SizeFormatter.sizeFormat(item.down)}
                      </span>
                    </div>
                    <Progress percent={item.sharePct} showInfo={false} size="small" />
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : !isClient && nodeData ? (
        <div className="traffic-drawer-body">
          <div className="traffic-drawer-summary">
            <div className="traffic-drawer-metric">
              <Typography.Text type="secondary">{t('pages.traffic.total')}</Typography.Text>
              <div className="traffic-drawer-metric-main">
                <strong>{SizeFormatter.sizeFormat(nodeData.summary.total)}</strong>
                {formatDeltaBadge(nodeData.summary.changePct)}
              </div>
              <Typography.Text type="secondary" className="traffic-sub-line">
                ↑ {SizeFormatter.sizeFormat(nodeData.summary.totalUp)} / ↓{' '}
                {SizeFormatter.sizeFormat(nodeData.summary.totalDown)}
              </Typography.Text>
            </div>
            <div className="traffic-drawer-metric">
              <Typography.Text type="secondary">{t('pages.traffic.liveSpeedCard')}</Typography.Text>
              <div className="traffic-drawer-metric-main">
                {isActiveSpeed(nodeLiveSpeed) ? (
                  <ClientSpeedTag speed={nodeLiveSpeed} />
                ) : (
                  <Typography.Text type="secondary">{t('pages.traffic.idle')}</Typography.Text>
                )}
              </div>
            </div>
          </div>

          <div className="traffic-drawer-section">
            <Typography.Title level={5}>{t('pages.traffic.trendTitle')}</Typography.Title>
            <TrafficTrendChart series={nodeData.series} height={200} />
          </div>

          <div className="traffic-drawer-section">
            <div className="traffic-drawer-section-head">
              <Typography.Title level={5} style={{ margin: 0 }}>
                {t('pages.traffic.clientBreakdown')}
              </Typography.Title>
              <Input.Search
                allowClear
                size="small"
                aria-label={t('pages.traffic.searchPlaceholder')}
                value={nodeSearchInput}
                onChange={(e) =>
                  setNodeFilter({
                    scope: scopeKey,
                    input: e.target.value,
                    query: nodeSearch,
                    page: 1,
                  })
                }
                placeholder={t('pages.traffic.searchPlaceholder')}
                style={{ width: 200 }}
              />
            </div>

            {nodeData.clients.length === 0 ? (
              <Empty
                description={t('pages.traffic.noBreakdownData')}
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ) : (
              <>
                <div className="traffic-rank-list">
                  {nodeData.clients.map((item) => {
                    const key = item.unattributed ? '__unattributed__' : item.email;
                    const clickable = !item.unattributed;
                    return (
                      <button
                        key={key}
                        type="button"
                        disabled={!clickable}
                        data-testid={
                          item.unattributed
                            ? 'drawer-node-client-unattributed'
                            : `drawer-node-client-${item.email}`
                        }
                        className={`traffic-rank-row ${clickable ? 'is-clickable' : 'is-static'}`}
                        onClick={() => clickable && onSelectClient(item.email)}
                      >
                        <div className="traffic-rank-row-top">
                          <span className="traffic-rank-name">
                            {!item.unattributed && (
                              <span
                                className={`traffic-status-dot ${item.online ? 'is-online' : 'is-offline'}`}
                              />
                            )}
                            <span>
                              {item.unattributed ? t('pages.traffic.unattributed') : item.email}
                            </span>
                            {item.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
                          </span>
                          <span className="traffic-rank-total">
                            <strong>{SizeFormatter.sizeFormat(item.total)}</strong>
                            <span className="traffic-rank-share">{item.sharePct}%</span>
                          </span>
                        </div>
                        <div className="traffic-rank-row-sub">
                          <span>
                            ↑ {SizeFormatter.sizeFormat(item.up)} / ↓{' '}
                            {SizeFormatter.sizeFormat(item.down)}
                          </span>
                        </div>
                        <Progress percent={item.sharePct} showInfo={false} size="small" />
                      </button>
                    );
                  })}
                </div>
                {nodeData.clientsTotal > 20 && (
                  <div className="traffic-pagination">
                    <Pagination
                      size="small"
                      current={nodePage}
                      pageSize={20}
                      total={nodeData.clientsTotal}
                      showSizeChanger={false}
                      onChange={(p) =>
                        setNodeFilter({
                          scope: scopeKey,
                          input: nodeSearchInput,
                          query: nodeSearch,
                          page: p,
                        })
                      }
                    />
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}
