import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import dayjs, { type Dayjs } from 'dayjs';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  DatePicker,
  Empty,
  Input,
  Layout,
  Pagination,
  Progress,
  Row,
  Segmented,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from 'antd';
import {
  ClusterOutlined,
  LeftOutlined,
  LineChartOutlined,
  RightOutlined,
  TeamOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';

import { useTrafficClientsQuery, useTrafficOverviewQuery } from '@/api/queries/useTrafficQueries';
import { useLiveTrafficSpeeds } from '@/hooks/useLiveTrafficSpeeds';
import { useTheme } from '@/hooks/useTheme';
import AppSidebar from '@/layouts/AppSidebar';
import TrafficTrendChart from '@/components/viz/TrafficTrendChart';
import { ClientSpeedTag, isActiveSpeed } from '@/components/clients/ClientSpeedTag';
import { SizeFormatter } from '@/utils';
import type { LiveSpeed, TrafficPeriod, TrafficSort } from '@/schemas/traffic';
import TrafficDetailDrawer, { type TrafficDrawerTarget } from './TrafficDetailDrawer';
import './TrafficPage.css';

function shiftAnchor(anchor: string, period: TrafficPeriod, delta: number): string {
  const base = dayjs(anchor);
  if (!base.isValid()) return anchor;
  if (period === 'day') return base.add(delta, 'day').format('YYYY-MM-DD');
  if (period === 'week') return base.add(delta * 7, 'day').format('YYYY-MM-DD');
  return base.startOf('month').add(delta, 'month').format('YYYY-MM-DD');
}

function formatChangeText(changePct: number | null | undefined): string {
  if (changePct == null) return '—';
  const sign = changePct > 0 ? '+' : '';
  return `${sign}${changePct}%`;
}

export default function TrafficPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();

  const [period, setPeriod] = useState<TrafficPeriod>('day');
  const [anchor, setAnchor] = useState<string>('');
  const [searchInput, setSearchInput] = useState('');
  const [search, setSearch] = useState('');
  const [clientSort, setClientSort] = useState<TrafficSort>('traffic');
  const [nodeSort, setNodeSort] = useState<TrafficSort>('traffic');
  const [activeOnly, setActiveOnly] = useState(false);
  const [page, setPage] = useState(1);
  const [drawerTarget, setDrawerTarget] = useState<TrafficDrawerTarget>(null);

  const live = useLiveTrafficSpeeds();

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setSearch(searchInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  const overviewQuery = useTrafficOverviewQuery(period, anchor, true);
  const overview = overviewQuery.data;
  const effectiveAnchor = anchor || overview?.anchor || '';
  const today = overview?.today || '';
  const isViewingCurrent =
    !anchor ||
    !today ||
    (overview ? today >= overview.rangeStart && today <= overview.rangeEnd : true);

  const clientsQuery = useTrafficClientsQuery(
    {
      period,
      anchor: effectiveAnchor,
      search,
      sort: clientSort,
      activeOnly,
      page,
      pageSize: 25,
    },
    isViewingCurrent,
  );

  // When sorted by live speed, throttle lightweight server re-sorts on live WS updates.
  const lastSpeedRefetchRef = useRef(0);
  useEffect(() => {
    if (clientSort !== 'speed' || !live.receivedLive || !live.lastUpdatedAt) return;
    if (live.lastUpdatedAt - lastSpeedRefetchRef.current < 5000) return;
    lastSpeedRefetchRef.current = live.lastUpdatedAt;
    void clientsQuery.refetch();
  }, [clientSort, live.lastUpdatedAt, live.receivedLive, clientsQuery]);

  const totalLiveSpeed: LiveSpeed = live.receivedLive
    ? live.totalSpeed
    : {
        up: overview?.summary.liveUpSpeed ?? 0,
        down: overview?.summary.liveDownSpeed ?? 0,
      };
  const totalLiveCombined = totalLiveSpeed.up + totalLiveSpeed.down;

  const sortedNodes = useMemo(() => {
    const base = (overview?.nodes ?? []).map((n) => {
      const wsSpeed = live.receivedLive ? live.nodeSpeeds[n.nodeId] : undefined;
      const upSpeed = live.receivedLive ? (wsSpeed?.up ?? 0) : n.upSpeed;
      const downSpeed = live.receivedLive ? (wsSpeed?.down ?? 0) : n.downSpeed;
      return { ...n, upSpeed, downSpeed };
    });
    return [...base].sort((a, b) => {
      if (nodeSort === 'speed') {
        const diff = b.upSpeed + b.downSpeed - (a.upSpeed + a.downSpeed);
        if (diff !== 0) return diff;
      }
      if (b.total !== a.total) return b.total - a.total;
      return a.nodeId - b.nodeId;
    });
  }, [overview?.nodes, live.receivedLive, live.nodeSpeeds, nodeSort]);

  const clientItems = useMemo(() => {
    return (clientsQuery.data?.items ?? []).map((item) => {
      const wsSpeed = live.receivedLive ? live.userSpeeds[item.email] : undefined;
      const upSpeed = live.receivedLive ? (wsSpeed?.up ?? 0) : item.upSpeed;
      const downSpeed = live.receivedLive ? (wsSpeed?.down ?? 0) : item.downSpeed;
      const online = live.onlineEmails ? live.onlineEmails.has(item.email) : item.online;
      return { ...item, upSpeed, downSpeed, online };
    });
  }, [clientsQuery.data?.items, live.receivedLive, live.userSpeeds, live.onlineEmails]);

  const handleShift = (delta: number) => {
    const base = effectiveAnchor || dayjs().format('YYYY-MM-DD');
    setAnchor(shiftAnchor(base, period, delta));
    setPage(1);
  };

  const handleDatePick = (val: Dayjs | null) => {
    if (!val) {
      setAnchor('');
    } else {
      setAnchor(val.format('YYYY-MM-DD'));
    }
    setPage(1);
  };

  const handlePeriodChange = (next: TrafficPeriod) => {
    setPeriod(next);
    setPage(1);
  };

  const pickerMode = period === 'month' ? 'month' : period === 'week' ? 'week' : 'date';
  const pickerValue = effectiveAnchor ? dayjs(effectiveAnchor) : null;
  const summary = overview?.summary;

  const pageClass = [
    'traffic-page',
    'traffic-layout',
    isDark ? 'is-dark' : '',
    isDark && isUltra ? 'is-ultra' : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout.Content className="traffic-content">
          <div className="traffic-header">
            <div className="traffic-header-left">
              <Typography.Title level={4} style={{ margin: 0 }}>
                <LineChartOutlined aria-hidden style={{ marginRight: 8 }} />
                {t('pages.traffic.title')}
              </Typography.Title>
              <span className="traffic-live-pill">
                <span
                  className={`traffic-status-dot ${live.connected ? 'is-online' : 'is-offline'}`}
                />
                {live.connected
                  ? t('pages.traffic.liveConnected')
                  : t('pages.traffic.liveDisconnected')}
              </span>
              {overview?.timezone && <Tag>{overview.timezone}</Tag>}
            </div>

            <Space wrap size="small">
              <Segmented<TrafficPeriod>
                value={period}
                onChange={handlePeriodChange}
                options={[
                  { value: 'day', label: t('pages.traffic.periodDay') },
                  { value: 'week', label: t('pages.traffic.periodWeek') },
                  { value: 'month', label: t('pages.traffic.periodMonth') },
                ]}
              />
              <Space.Compact>
                <Button
                  icon={<LeftOutlined aria-hidden />}
                  aria-label={t('pages.traffic.prevPeriod')}
                  onClick={() => handleShift(-1)}
                />
                <DatePicker
                  picker={pickerMode}
                  value={pickerValue}
                  allowClear={false}
                  aria-label={t('pages.traffic.trendTitle')}
                  onChange={handleDatePick}
                />
                <Button
                  icon={<RightOutlined aria-hidden />}
                  aria-label={t('pages.traffic.nextPeriod')}
                  disabled={isViewingCurrent}
                  onClick={() => handleShift(1)}
                />
              </Space.Compact>
              {!isViewingCurrent && (
                <Button type="link" onClick={() => setAnchor('')}>
                  {t('pages.traffic.backToCurrent')}
                </Button>
              )}
            </Space>
          </div>

          {summary && !summary.hasHistory && (
            <Alert
              type="info"
              showIcon
              className="traffic-banner"
              title={t('pages.traffic.coldStartHint')}
            />
          )}

          {summary?.hourlyExpired && (
            <Alert
              type="warning"
              showIcon
              className="traffic-banner"
              title={t('pages.traffic.hourlyExpiredHint')}
              action={
                <Button size="small" onClick={() => handlePeriodChange('week')}>
                  {t('pages.traffic.switchToWeek')}
                </Button>
              }
            />
          )}

          <Row gutter={[16, 16]} className="traffic-kpi-row">
            <Col xs={24} sm={12} xl={6}>
              <Card size="small" className="traffic-kpi-card">
                <div className="traffic-kpi-label">
                  <ThunderboltOutlined aria-hidden /> {t('pages.traffic.liveSpeedCard')}
                </div>
                <div className="traffic-kpi-value">
                  {SizeFormatter.speedFormat(totalLiveCombined)}
                </div>
                <div className="traffic-kpi-sub">
                  ↑ {SizeFormatter.speedFormat(totalLiveSpeed.up)} / ↓{' '}
                  {SizeFormatter.speedFormat(totalLiveSpeed.down)}
                </div>
              </Card>
            </Col>

            <Col xs={24} sm={12} xl={6}>
              <Card size="small" className="traffic-kpi-card">
                <div className="traffic-kpi-label">
                  <span>{t('pages.traffic.periodTotalCard')}</span>
                  {summary && (
                    <Tag
                      color={
                        summary.changePct == null
                          ? 'default'
                          : summary.changePct > 0
                            ? 'green'
                            : summary.changePct < 0
                              ? 'volcano'
                              : 'default'
                      }
                    >
                      {formatChangeText(summary.changePct)}
                    </Tag>
                  )}
                </div>
                <div className="traffic-kpi-value">
                  {SizeFormatter.sizeFormat(summary?.total ?? 0)}
                </div>
                <div className="traffic-kpi-sub">
                  ↑ {SizeFormatter.sizeFormat(summary?.totalUp ?? 0)} / ↓{' '}
                  {SizeFormatter.sizeFormat(summary?.totalDown ?? 0)} ·{' '}
                  {t('pages.traffic.vsPrevPeriod')}{' '}
                  {SizeFormatter.sizeFormat(summary?.prevTotal ?? 0)}
                </div>
              </Card>
            </Col>

            <Col xs={24} sm={12} xl={6}>
              <Card size="small" className="traffic-kpi-card">
                <div className="traffic-kpi-label">
                  <TeamOutlined aria-hidden /> {t('pages.traffic.activeClientsCard')}
                </div>
                <div className="traffic-kpi-value">
                  {summary?.activeClients ?? 0} / {summary?.totalClients ?? 0}
                </div>
                <div className="traffic-kpi-sub">
                  {summary?.topClientEmail
                    ? `${t('pages.traffic.topLabel')}: ${summary.topClientEmail} (${SizeFormatter.sizeFormat(summary.topClientTotal)})`
                    : '—'}
                </div>
              </Card>
            </Col>

            <Col xs={24} sm={12} xl={6}>
              <Card size="small" className="traffic-kpi-card">
                <div className="traffic-kpi-label">
                  <ClusterOutlined aria-hidden /> {t('pages.traffic.onlineNodesCard')}
                </div>
                <div className="traffic-kpi-value">
                  {summary?.onlineNodes ?? 0} / {summary?.totalNodes ?? 0}
                </div>
                <div className="traffic-kpi-sub">
                  {summary?.topNodeTotal
                    ? `${t('pages.traffic.topLabel')}: ${
                        summary.topNodeId === 0 ? t('pages.traffic.localNode') : summary.topNodeName
                      } (${SizeFormatter.sizeFormat(summary.topNodeTotal)})`
                    : '—'}
                </div>
              </Card>
            </Col>
          </Row>

          <Card
            size="small"
            className="traffic-chart-card"
            title={
              <div className="traffic-card-title-row">
                <span>{t('pages.traffic.trendTitle')}</span>
                {overview && (
                  <Typography.Text type="secondary" className="traffic-range-label">
                    {overview.rangeStart === overview.rangeEnd
                      ? overview.rangeStart
                      : `${overview.rangeStart} ~ ${overview.rangeEnd}`}
                  </Typography.Text>
                )}
              </div>
            }
          >
            {overviewQuery.isLoading && !overview ? (
              <div className="traffic-chart-loading">
                <Spin />
              </div>
            ) : (
              <TrafficTrendChart
                series={overview?.series ?? []}
                height={230}
                ariaLabel={t('pages.traffic.trendTitle')}
              />
            )}
          </Card>

          <Row gutter={[16, 16]}>
            <Col xs={24} xl={12}>
              <Card
                size="small"
                className="traffic-panel-card"
                title={
                  <div className="traffic-card-title-row">
                    <span>
                      <TeamOutlined aria-hidden style={{ marginRight: 6 }} />
                      {t('pages.traffic.clientsRanking')}
                    </span>
                    <Typography.Text type="secondary">
                      {clientsQuery.data?.total ?? 0}
                    </Typography.Text>
                  </div>
                }
              >
                <div className="traffic-toolbar">
                  <Input.Search
                    allowClear
                    size="small"
                    aria-label={t('pages.traffic.searchPlaceholder')}
                    value={searchInput}
                    onChange={(e) => setSearchInput(e.target.value)}
                    placeholder={t('pages.traffic.searchPlaceholder')}
                    className="traffic-search-input"
                  />
                  <Space size="small" wrap>
                    <Segmented<TrafficSort>
                      size="small"
                      value={clientSort}
                      onChange={(v) => {
                        setClientSort(v);
                        setPage(1);
                      }}
                      options={[
                        { value: 'traffic', label: t('pages.traffic.sortTraffic') },
                        { value: 'speed', label: t('pages.traffic.sortSpeed') },
                      ]}
                    />
                    <label className="traffic-active-toggle">
                      <Switch
                        size="small"
                        checked={activeOnly}
                        onChange={(checked) => {
                          setActiveOnly(checked);
                          setPage(1);
                        }}
                      />
                      <span>{t('pages.traffic.activeOnly')}</span>
                    </label>
                  </Space>
                </div>

                {clientsQuery.isLoading && !clientsQuery.data ? (
                  <div className="traffic-drawer-loading">
                    <Spin />
                  </div>
                ) : clientItems.length === 0 ? (
                  <Empty
                    description={t('pages.traffic.emptyClients')}
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                  />
                ) : (
                  <>
                    <div className="traffic-rank-list">
                      {clientItems.map((item) => {
                        const speed = { up: item.upSpeed, down: item.downSpeed };
                        return (
                          <button
                            key={item.email}
                            type="button"
                            data-testid={`traffic-client-row-${item.email}`}
                            className="traffic-rank-row is-clickable"
                            onClick={() => setDrawerTarget({ kind: 'client', email: item.email })}
                          >
                            <div className="traffic-rank-row-top">
                              <span className="traffic-rank-name">
                                <span
                                  className={`traffic-status-dot ${item.online ? 'is-online' : 'is-offline'}`}
                                />
                                <span className="traffic-rank-label">{item.email}</span>
                                {item.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
                              </span>
                              <span className="traffic-rank-speed">
                                {isActiveSpeed(speed) ? (
                                  <ClientSpeedTag speed={speed} />
                                ) : (
                                  <Typography.Text type="secondary" className="traffic-idle-text">
                                    {t('pages.traffic.idle')}
                                  </Typography.Text>
                                )}
                              </span>
                            </div>
                            <div className="traffic-rank-row-sub">
                              <span>
                                <strong>{SizeFormatter.sizeFormat(item.total)}</strong>
                                {' · '}↑ {SizeFormatter.sizeFormat(item.up)} / ↓{' '}
                                {SizeFormatter.sizeFormat(item.down)}
                              </span>
                              <span>{item.sharePct}%</span>
                            </div>
                            <Progress percent={item.sharePct} showInfo={false} size="small" />
                          </button>
                        );
                      })}
                    </div>

                    {(clientsQuery.data?.total ?? 0) > 25 && (
                      <div className="traffic-pagination">
                        <Pagination
                          size="small"
                          current={page}
                          pageSize={25}
                          total={clientsQuery.data?.total ?? 0}
                          showSizeChanger={false}
                          onChange={(p) => setPage(p)}
                        />
                      </div>
                    )}
                  </>
                )}
              </Card>
            </Col>

            <Col xs={24} xl={12}>
              <Card
                size="small"
                className="traffic-panel-card"
                title={
                  <div className="traffic-card-title-row">
                    <span>
                      <ClusterOutlined aria-hidden style={{ marginRight: 6 }} />
                      {t('pages.traffic.nodesRanking')}
                    </span>
                    <Segmented<TrafficSort>
                      size="small"
                      value={nodeSort}
                      onChange={setNodeSort}
                      options={[
                        { value: 'traffic', label: t('pages.traffic.sortTraffic') },
                        { value: 'speed', label: t('pages.traffic.sortSpeed') },
                      ]}
                    />
                  </div>
                }
              >
                {overviewQuery.isLoading && !overview ? (
                  <div className="traffic-drawer-loading">
                    <Spin />
                  </div>
                ) : sortedNodes.length === 0 ? (
                  <Empty
                    description={t('pages.traffic.emptyNodes')}
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                  />
                ) : (
                  <div className="traffic-rank-list">
                    {sortedNodes.map((node) => {
                      const speed = { up: node.upSpeed, down: node.downSpeed };
                      const label =
                        node.nodeId === 0 ? t('pages.traffic.localNode') : node.nodeName;
                      return (
                        <button
                          key={node.nodeId}
                          type="button"
                          data-testid={`traffic-node-row-${node.nodeId}`}
                          className="traffic-rank-row is-clickable"
                          onClick={() => setDrawerTarget({ kind: 'node', nodeId: node.nodeId })}
                        >
                          <div className="traffic-rank-row-top">
                            <span className="traffic-rank-name">
                              <span className={`traffic-status-dot is-${node.status}`} />
                              <span className="traffic-rank-label">{label}</span>
                              {node.deleted && <Tag>{t('pages.traffic.deleted')}</Tag>}
                            </span>
                            <span className="traffic-rank-speed">
                              {isActiveSpeed(speed) ? (
                                <ClientSpeedTag speed={speed} />
                              ) : (
                                <Typography.Text type="secondary" className="traffic-idle-text">
                                  {t('pages.traffic.idle')}
                                </Typography.Text>
                              )}
                            </span>
                          </div>
                          <div className="traffic-rank-row-sub">
                            <span>
                              <strong>{SizeFormatter.sizeFormat(node.total)}</strong>
                              {' · '}↑ {SizeFormatter.sizeFormat(node.up)} / ↓{' '}
                              {SizeFormatter.sizeFormat(node.down)}
                            </span>
                            <span>{node.sharePct}%</span>
                          </div>
                          <Progress percent={node.sharePct} showInfo={false} size="small" />
                        </button>
                      );
                    })}
                  </div>
                )}
              </Card>
            </Col>
          </Row>

          <TrafficDetailDrawer
            target={drawerTarget}
            period={period}
            anchor={effectiveAnchor}
            liveUserSpeeds={live.userSpeeds}
            liveNodeSpeeds={live.nodeSpeeds}
            receivedLive={live.receivedLive}
            onClose={() => setDrawerTarget(null)}
            onSelectClient={(email) => setDrawerTarget({ kind: 'client', email })}
          />
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
