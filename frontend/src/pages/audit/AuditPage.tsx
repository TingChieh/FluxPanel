import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  Drawer,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Layout,
  Row,
  Segmented,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckOutlined,
  EyeOutlined,
  ReloadOutlined,
  SafetyOutlined,
  SaveOutlined,
  SyncOutlined,
} from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { useTheme } from '@/hooks/useTheme';
import AppSidebar from '@/layouts/AppSidebar';
import { setMessageInstance } from '@/utils/messageBus';
import type {
  AccessAuditAlert,
  AuditClientDetailResponse,
  AuditClientIPView,
  AuditClientSummary,
  AuditConfig,
  AuditDomainGroup,
  AuditIngestResult,
  AuditSourceSummary,
  AuditSummaryResponse,
} from '@/generated/types';
import AuditNotifications from './AuditNotifications';

const ALL_SOURCES = -1;

type DatePreset = 'today' | 'yesterday' | '7d' | '30d';

function formatLocalDate(): string {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function previousDay(isoDate: string): string {
  const d = new Date(`${isoDate}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() - 1);
  return d.toISOString().slice(0, 10);
}

function formatTs(sec?: number): string {
  if (!sec) return '-';
  const d = new Date(sec * 1000);
  if (Number.isNaN(d.getTime())) return '-';
  return d.toLocaleString();
}

function splitLines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);
}

function categoryLabel(t: TFunction, cat?: string): string {
  switch (cat) {
    case 'sensitive':
      return t('pages.audit.categories.sensitive');
    case 'risk':
      return t('pages.audit.categories.risk');
    case 'ai_dev':
      return t('pages.audit.categories.ai_dev');
    case 'video':
      return t('pages.audit.categories.video');
    case 'social':
      return t('pages.audit.categories.social');
    case 'gaming':
      return t('pages.audit.categories.gaming');
    case 'shopping':
      return t('pages.audit.categories.shopping');
    case 'noise':
      return t('pages.audit.categories.noise');
    default:
      return t('pages.audit.categories.general');
  }
}

function categoryColor(cat?: string): string {
  switch (cat) {
    case 'sensitive':
      return 'red';
    case 'risk':
      return 'volcano';
    case 'ai_dev':
      return 'blue';
    case 'video':
      return 'purple';
    case 'social':
      return 'cyan';
    case 'gaming':
      return 'geekblue';
    case 'shopping':
      return 'gold';
    case 'noise':
      return 'default';
    default:
      return 'default';
  }
}

function alertTypeLabel(t: TFunction, alertType?: string): string {
  switch (alertType) {
    case 'sensitive_domain':
      return t('pages.audit.alertTypes.sensitive_domain');
    case 'account_sharing':
      return t('pages.audit.alertTypes.account_sharing');
    default:
      return t('pages.audit.alertTypes.high_risk_port');
  }
}

function HourlyHeatmap({ mask }: { mask: number }) {
  return (
    <div style={{ display: 'flex', gap: 2, alignItems: 'center' }}>
      {Array.from({ length: 24 }, (_, hour) => {
        const active = (mask & (1 << hour)) !== 0;
        return (
          <span
            key={hour}
            title={`${String(hour).padStart(2, '0')}:00`}
            style={{
              width: 10,
              height: 14,
              borderRadius: 2,
              backgroundColor: active
                ? 'var(--ant-color-primary)'
                : 'var(--ant-color-fill-secondary)',
              display: 'inline-block',
            }}
          />
        );
      })}
    </div>
  );
}

export default function AuditPage() {
  const { t } = useTranslation();
  const uid = useId();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  const pageClass = useMemo(() => {
    const classes = ['audit-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);
  const [preset, setPreset] = useState<DatePreset>('today');
  const [source, setSource] = useState<number>(ALL_SOURCES);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [ingesting, setIngesting] = useState(false);
  const [summary, setSummary] = useState<AuditSummaryResponse | null>(null);
  const [alerts, setAlerts] = useState<AccessAuditAlert[]>([]);
  const [onlyUnacked, setOnlyUnacked] = useState(true);

  const [selectedEmail, setSelectedEmail] = useState<string | null>(null);
  const [hideNoise, setHideNoise] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<AuditClientDetailResponse | null>(null);

  const [config, setConfig] = useState<AuditConfig>({
    enabled: true,
    retentionDays: 30,
    maxDailySubnets: 8,
    maxConcurrentSubnets: 3,
    minSharingHits: 5,
    minConcurrentWindows: 2,
    sensitiveDomains: [],
    sensitiveKeywords: [],
    ignoreNoiseDomains: [],
    trustedTargets: [],
    trustedClients: [],
  });
  const [domainsText, setDomainsText] = useState('');
  const [keywordsText, setKeywordsText] = useState('');
  const [noiseText, setNoiseText] = useState('');
  const [trustedTargetsText, setTrustedTargetsText] = useState('');
  const [trustedClientsText, setTrustedClientsText] = useState('');
  const [savingConfig, setSavingConfig] = useState(false);
  const dailyThresholdInvalid =
    config.maxDailySubnets < 0 || config.maxDailySubnets === 1 || config.maxDailySubnets > 100;

  // The server buckets days in the panel's timeLocation, so "today" is whatever date it reports.
  const serverTodayRef = useRef<string | null>(null);

  const queryWindow = useCallback(() => {
    const days = preset === '7d' ? 7 : preset === '30d' ? 30 : 1;
    const query = new URLSearchParams({ days: String(days) });
    if (preset === 'yesterday') {
      query.set('date', previousDay(serverTodayRef.current ?? formatLocalDate()));
    }
    if (source !== ALL_SOURCES) query.set('nodeId', String(source));
    return query;
  }, [preset, source]);

  const acceptSummary = useCallback(
    (next: AuditSummaryResponse) => {
      if (preset !== 'yesterday') serverTodayRef.current = next.date;
      setSummary(next);
    },
    [preset],
  );

  const fetchData = useCallback(async () => {
    const [sumRes, alertRes] = await Promise.all([
      HttpUtil.get<AuditSummaryResponse>(`/panel/api/audit/summary?${queryWindow()}`),
      HttpUtil.get<AccessAuditAlert[]>(
        `/panel/api/audit/alerts?onlyUnacked=${onlyUnacked ? 'true' : 'false'}&limit=200`,
      ),
    ]);
    return { sumRes, alertRes };
  }, [queryWindow, onlyUnacked]);

  const refreshData = useCallback(async () => {
    setLoading(true);
    try {
      const { sumRes, alertRes } = await fetchData();
      if (sumRes?.success && sumRes.obj) {
        setLoadError(null);
        acceptSummary(sumRes.obj);
      } else if (sumRes && !sumRes.success) {
        setLoadError(sumRes.msg || t('somethingWentWrong'));
      }
      if (alertRes?.success && Array.isArray(alertRes.obj)) setAlerts(alertRes.obj);
    } finally {
      setLoading(false);
    }
  }, [fetchData, acceptSummary, t]);

  const refreshDetail = useCallback(
    async (email: string, hide: boolean) => {
      setDetailLoading(true);
      try {
        const query = queryWindow();
        query.set('hideNoise', hide ? 'true' : 'false');
        const res = await HttpUtil.get<AuditClientDetailResponse>(
          `/panel/api/audit/client/${encodeURIComponent(email)}?${query}`,
        );
        if (res?.success && res.obj) setDetail(res.obj);
      } finally {
        setDetailLoading(false);
      }
    },
    [queryWindow],
  );

  useEffect(() => {
    let active = true;
    void fetchData().then(({ sumRes, alertRes }) => {
      if (!active) return;
      if (sumRes?.success && sumRes.obj) {
        setLoadError(null);
        acceptSummary(sumRes.obj);
      } else if (sumRes && !sumRes.success) {
        setLoadError(sumRes.msg || t('somethingWentWrong'));
      }
      if (alertRes?.success && Array.isArray(alertRes.obj)) setAlerts(alertRes.obj);
    });
    return () => {
      active = false;
    };
  }, [fetchData, acceptSummary, t]);

  const applyConfig = useCallback((next: AuditConfig) => {
    setConfig(next);
    setDomainsText((next.sensitiveDomains || []).join('\n'));
    setKeywordsText((next.sensitiveKeywords || []).join('\n'));
    setNoiseText((next.ignoreNoiseDomains || []).join('\n'));
    setTrustedTargetsText((next.trustedTargets || []).join('\n'));
    setTrustedClientsText((next.trustedClients || []).join('\n'));
  }, []);

  useEffect(() => {
    let active = true;
    void HttpUtil.get<AuditConfig>('/panel/api/audit/config').then((res) => {
      if (active && res?.success && res.obj) applyConfig(res.obj);
    });
    return () => {
      active = false;
    };
  }, [applyConfig]);

  useEffect(() => {
    if (!selectedEmail) return;
    let active = true;
    const query = queryWindow();
    query.set('hideNoise', hideNoise ? 'true' : 'false');
    void HttpUtil.get<AuditClientDetailResponse>(
      `/panel/api/audit/client/${encodeURIComponent(selectedEmail)}?${query}`,
    ).then((res) => {
      if (active && res?.success && res.obj) setDetail(res.obj);
    });
    return () => {
      active = false;
    };
  }, [selectedEmail, hideNoise, queryWindow]);

  const handleIngestNow = async () => {
    setIngesting(true);
    try {
      const res = await HttpUtil.post<AuditIngestResult>('/panel/api/audit/ingest');
      if (res?.success) {
        const count = res.obj?.linesProcessed ?? 0;
        const synced = res.obj?.nodesSynced ?? 0;
        const failed = res.obj?.nodesFailed ?? 0;
        if (synced + failed > 0) {
          messageApi[failed > 0 ? 'warning' : 'success'](
            t('pages.audit.ingestedWithNodes', { count, synced, failed }),
          );
        } else {
          messageApi.success(t('pages.audit.ingestedCount', { count }));
        }
        await refreshData();
        if (selectedEmail) await refreshDetail(selectedEmail, hideNoise);
      }
    } finally {
      setIngesting(false);
    }
  };

  const handleAckAlert = async (id: number) => {
    const res = await HttpUtil.post(`/panel/api/audit/alerts/ack/${id}`);
    if (res?.success) {
      await refreshData();
      if (selectedEmail) await refreshDetail(selectedEmail, hideNoise);
    }
  };

  const handleTrustAlert = async (id: number, scope: 'target' | 'host' | 'client') => {
    const res = await HttpUtil.post(
      `/panel/api/audit/alerts/trust/${id}`,
      { scope },
      { headers: { 'Content-Type': 'application/json' } },
    );
    if (!res?.success) return;
    const cfgRes = await HttpUtil.get<AuditConfig>('/panel/api/audit/config');
    if (cfgRes?.success && cfgRes.obj) applyConfig(cfgRes.obj);
    await refreshData();
    if (selectedEmail) await refreshDetail(selectedEmail, hideNoise);
  };

  const handleAckAll = async () => {
    const res = await HttpUtil.post('/panel/api/audit/alerts/ackAll');
    if (res?.success) {
      await refreshData();
    }
  };

  const handleSaveConfig = async (reevaluate: boolean) => {
    if (dailyThresholdInvalid) return;
    setSavingConfig(true);
    try {
      const nextCfg: AuditConfig = {
        ...config,
        sensitiveDomains: splitLines(domainsText),
        sensitiveKeywords: splitLines(keywordsText),
        ignoreNoiseDomains: splitLines(noiseText),
        trustedTargets: splitLines(trustedTargetsText),
        trustedClients: splitLines(trustedClientsText),
      };
      const res = await HttpUtil.post(
        '/panel/api/audit/config',
        { config: nextCfg, reevaluate },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (res?.success) {
        setConfig(nextCfg);
        await refreshData();
        if (selectedEmail) await refreshDetail(selectedEmail, hideNoise);
      }
    } finally {
      setSavingConfig(false);
    }
  };

  const sources: AuditSourceSummary[] = summary?.sources ?? [];
  const sourceName = (nodeId: number) => {
    if (nodeId === 0) return t('pages.audit.sources.local');
    return sources.find((s) => s.nodeId === nodeId)?.name ?? `#${nodeId}`;
  };
  const sourceTags = (ids: number[] | undefined) => (
    <Space size={[4, 4]} wrap>
      {(ids ?? []).map((id) => (
        <Tag key={id} color={id === 0 ? 'blue' : 'geekblue'}>
          {sourceName(id)}
        </Tag>
      ))}
    </Space>
  );

  const clientColumns: ColumnsType<AuditClientSummary> = [
    {
      title: t('pages.audit.columns.client'),
      dataIndex: 'email',
      key: 'email',
      render: (email: string, row) => (
        <Space orientation="vertical" size={2}>
          <Typography.Text strong>{email}</Typography.Text>
          {sourceTags(row.nodeIds)}
        </Space>
      ),
    },
    {
      title: t('pages.audit.columns.riskStatus'),
      key: 'risk',
      render: (_, row) => {
        const hasConcurrent =
          row.concurrentSharingAlert || (row.sharingAlert && !row.dailySharingAlert);
        const hasDaily = row.dailySharingAlert;
        return (
          <Space wrap size={[4, 4]}>
            {row.highRiskHits > 0 && (
              <Tag color="red">{t('pages.audit.risk.highHits', { count: row.highRiskHits })}</Tag>
            )}
            {hasConcurrent && <Tag color="volcano">{t('pages.audit.risk.sharingConcurrent')}</Tag>}
            {hasDaily && <Tag color="gold">{t('pages.audit.risk.sharingDaily')}</Tag>}
            {row.highRiskHits === 0 && row.warnRiskHits === 0 && !hasConcurrent && !hasDaily && (
              <Tag color="green">{t('pages.audit.risk.normal')}</Tag>
            )}
          </Space>
        );
      },
    },
    {
      title: t('pages.audit.columns.topDomains'),
      dataIndex: 'topDomains',
      key: 'topDomains',
      render: (domains: string[]) => (
        <Space wrap size={[4, 4]}>
          {(domains || []).map((d) => (
            <Tag key={d}>{d}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: t('pages.audit.columns.hitsAndDomains'),
      key: 'hits',
      render: (_, row) => `${row.totalHits} / ${row.uniqueRootDomains}`,
    },
    {
      title: t('pages.audit.columns.sourceSubnets'),
      key: 'subnets',
      render: (_, row) => {
        const hasConcurrent =
          row.concurrentSharingAlert || (row.sharingAlert && !row.dailySharingAlert);
        return (
          <Space size={4}>
            <span>{row.uniqueIps}</span>
            <span>/</span>
            {row.dailySharingAlert ? (
              <Tag color="gold" style={{ marginInlineEnd: 0 }}>
                {row.uniqueSubnets}
              </Tag>
            ) : (
              <span>{row.uniqueSubnets}</span>
            )}
            <span>/</span>
            <Tag color={hasConcurrent ? 'red' : 'default'}>{row.maxConcurrentSubnets}</Tag>
          </Space>
        );
      },
    },
    {
      title: t('pages.audit.columns.lastSeen'),
      dataIndex: 'lastSeen',
      key: 'lastSeen',
      render: (ts: number) => formatTs(ts),
    },
    {
      title: t('pages.audit.columns.actions'),
      key: 'actions',
      render: (_, row) => (
        <Button
          size="small"
          icon={<EyeOutlined aria-hidden />}
          onClick={() => setSelectedEmail(row.email)}
        >
          {t('pages.audit.columns.inspect')}
        </Button>
      ),
    },
  ];

  const alertColumns: ColumnsType<AccessAuditAlert> = [
    {
      title: t('pages.audit.columns.time'),
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 170,
      render: (ts: number) => formatTs(ts),
    },
    {
      title: t('pages.audit.columns.client'),
      dataIndex: 'email',
      key: 'email',
      width: 130,
      render: (email: string) => (
        <Button
          type="link"
          size="small"
          style={{ padding: 0 }}
          onClick={() => setSelectedEmail(email)}
        >
          {email}
        </Button>
      ),
    },
    {
      title: t('pages.audit.columns.type'),
      dataIndex: 'alertType',
      key: 'alertType',
      width: 140,
      render: (type: string) => (
        <Tag
          color={
            type === 'sensitive_domain' ? 'red' : type === 'account_sharing' ? 'volcano' : 'orange'
          }
        >
          {alertTypeLabel(t, type)}
        </Tag>
      ),
    },
    {
      title: t('pages.audit.columns.severity'),
      dataIndex: 'severity',
      key: 'severity',
      width: 100,
      render: (sev: number) => (
        <Tag color={sev >= 2 ? 'red' : 'gold'}>
          {sev >= 2 ? t('pages.audit.risk.high') : t('pages.audit.risk.warn')}
        </Tag>
      ),
    },
    {
      title: t('pages.audit.columns.rule'),
      dataIndex: 'ruleName',
      key: 'ruleName',
      width: 160,
      render: (rule: string) => <Typography.Text code>{rule}</Typography.Text>,
    },
    {
      title: t('pages.audit.columns.targetAndDetail'),
      key: 'detail',
      render: (_, row) => (
        <div>
          <Typography.Text strong>{row.target}</Typography.Text>
          {row.detail && (
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {row.detail}
              </Typography.Text>
            </div>
          )}
        </div>
      ),
    },
    {
      title: t('pages.audit.columns.hits'),
      dataIndex: 'hitCount',
      key: 'hitCount',
      width: 80,
    },
    {
      title: t('pages.audit.columns.actions'),
      key: 'ack',
      width: 210,
      render: (_, row) => {
        const trustItems =
          row.alertType === 'account_sharing'
            ? [{ key: 'client', label: t('pages.audit.trust.client', { email: row.email }) }]
            : [
                { key: 'target', label: t('pages.audit.trust.target', { target: row.target }) },
                { key: 'host', label: t('pages.audit.trust.host') },
              ];
        return (
          <Space size={4} wrap>
            {row.acked ? (
              <Tag>{t('pages.audit.columns.acked')}</Tag>
            ) : (
              <Button
                size="small"
                icon={<CheckOutlined aria-hidden />}
                onClick={() => void handleAckAlert(row.id)}
              >
                {t('pages.audit.columns.ack')}
              </Button>
            )}
            <Dropdown
              trigger={['click']}
              menu={{
                items: trustItems,
                onClick: ({ key }) =>
                  void handleTrustAlert(row.id, key as 'target' | 'host' | 'client'),
              }}
            >
              <Button size="small" icon={<SafetyOutlined aria-hidden />}>
                {t('pages.audit.trust.action')}
              </Button>
            </Dropdown>
          </Space>
        );
      },
    },
  ];

  const domainColumns: ColumnsType<AuditDomainGroup> = [
    {
      title: t('pages.audit.columns.domain'),
      dataIndex: 'rootDomain',
      key: 'rootDomain',
      render: (domain: string, row) => (
        <Space>
          <Typography.Text
            strong={row.riskLevel > 0}
            type={row.riskLevel >= 2 ? 'danger' : undefined}
          >
            {domain}
          </Typography.Text>
          {row.ruleHit && <Tag color="red">{row.ruleHit}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.audit.columns.category'),
      dataIndex: 'category',
      key: 'category',
      width: 130,
      render: (cat: string) => <Tag color={categoryColor(cat)}>{categoryLabel(t, cat)}</Tag>,
    },
    {
      title: t('pages.audit.columns.hits'),
      dataIndex: 'hitCount',
      key: 'hitCount',
      width: 90,
    },
    {
      title: t('pages.audit.columns.lastSeen'),
      dataIndex: 'lastSeen',
      key: 'lastSeen',
      width: 170,
      render: (ts: number) => formatTs(ts),
    },
  ];

  const ipColumns: ColumnsType<AuditClientIPView> = [
    {
      title: t('pages.audit.columns.srcIp'),
      dataIndex: 'srcIp',
      key: 'srcIp',
      width: 150,
    },
    {
      title: t('pages.audit.columns.source'),
      dataIndex: 'nodeId',
      key: 'nodeId',
      width: 120,
      render: (id: number) => sourceTags([id]),
    },
    {
      title: t('pages.audit.columns.subnet'),
      dataIndex: 'subnet',
      key: 'subnet',
      width: 150,
      render: (subnet: string) => <Tag>{subnet}</Tag>,
    },
    {
      title: t('pages.audit.columns.hits'),
      dataIndex: 'hitCount',
      key: 'hitCount',
      width: 80,
    },
    {
      title: t('pages.audit.columns.hourlyActivity'),
      dataIndex: 'activeHours',
      key: 'activeHours',
      render: (mask: number) => <HourlyHeatmap mask={mask} />,
    },
  ];

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Row
              justify="space-between"
              align="middle"
              gutter={[12, 12]}
              style={{ marginBottom: 16 }}
            >
              <Col>
                <Space orientation="vertical" size={0}>
                  <Typography.Title level={4} style={{ margin: 0 }}>
                    <SafetyOutlined aria-hidden style={{ marginRight: 8 }} />
                    {t('pages.audit.title')}
                  </Typography.Title>
                  <Typography.Text type="secondary">{t('pages.audit.subtitle')}</Typography.Text>
                </Space>
              </Col>
              <Col>
                <Space wrap>
                  <Select<number>
                    aria-label={t('pages.audit.sources.filter')}
                    style={{ minWidth: 150 }}
                    value={source}
                    onChange={setSource}
                    options={[
                      { value: ALL_SOURCES, label: t('pages.audit.sources.all') },
                      ...sources.map((s) => ({ value: s.nodeId, label: sourceName(s.nodeId) })),
                    ]}
                  />
                  <Segmented<DatePreset>
                    value={preset}
                    onChange={(val) => setPreset(val)}
                    options={[
                      { label: t('pages.audit.today'), value: 'today' },
                      { label: t('pages.audit.yesterday'), value: 'yesterday' },
                      { label: t('pages.audit.last7Days'), value: '7d' },
                      { label: t('pages.audit.last30Days'), value: '30d' },
                    ]}
                  />
                  <Button
                    icon={<SyncOutlined aria-hidden spin={ingesting} />}
                    loading={ingesting}
                    onClick={() => void handleIngestNow()}
                  >
                    {t('pages.audit.ingestNow')}
                  </Button>
                  <Button
                    aria-label={t('refresh')}
                    icon={<ReloadOutlined aria-hidden />}
                    loading={loading}
                    onClick={() => void refreshData()}
                  />
                </Space>
              </Col>
            </Row>

            <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
              <Col xs={12} md={6}>
                <Card size="small">
                  <Statistic
                    title={t('pages.audit.stats.activeClients')}
                    value={summary?.activeClients ?? 0}
                  />
                </Card>
              </Col>
              <Col xs={12} md={6}>
                <Card size="small">
                  <Statistic
                    title={t('pages.audit.stats.highRiskAlerts')}
                    value={summary?.highRiskAlerts ?? 0}
                    styles={{
                      content: {
                        color:
                          (summary?.highRiskAlerts ?? 0) > 0 ? 'var(--ant-color-error)' : undefined,
                      },
                    }}
                  />
                </Card>
              </Col>
              <Col xs={12} md={6}>
                <Card size="small">
                  <Statistic
                    title={t('pages.audit.stats.sharingClients')}
                    value={summary?.sharingClients ?? 0}
                    styles={{
                      content: {
                        color:
                          (summary?.sharingClients ?? 0) > 0
                            ? 'var(--ant-color-warning)'
                            : undefined,
                      },
                    }}
                  />
                </Card>
              </Col>
              <Col xs={12} md={6}>
                <Card size="small">
                  <Statistic
                    title={t('pages.audit.stats.unackedAlerts')}
                    value={summary?.unackedAlerts ?? 0}
                  />
                </Card>
              </Col>
            </Row>

            {sources.length > 1 && (
              <Space wrap style={{ marginBottom: 12 }}>
                {sources.map((s) => (
                  <Tag
                    key={s.nodeId}
                    color={s.syncError ? 'error' : s.nodeId === 0 ? 'blue' : 'geekblue'}
                  >
                    {sourceName(s.nodeId)} · {s.totalHits}
                    {s.nodeId > 0 &&
                      ` · ${s.lastSyncAt ? t('pages.audit.sources.syncedAt', { time: formatTs(s.lastSyncAt) }) : t('pages.audit.sources.neverSynced')}`}
                  </Tag>
                ))}
              </Space>
            )}
            {loadError && (
              <Alert type="error" showIcon style={{ marginBottom: 12 }} message={loadError} />
            )}
            {sources
              .filter((s) => s.syncError)
              .map((s) => (
                <Alert
                  key={s.nodeId}
                  type="warning"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message={t('pages.audit.sources.syncFailed', {
                    name: s.name,
                    error: s.syncError,
                  })}
                />
              ))}

            <Card size="small">
              <Tabs
                items={[
                  {
                    key: 'overview',
                    label: t('pages.audit.tabs.overview'),
                    children: (
                      <Table<AuditClientSummary>
                        rowKey="email"
                        size="small"
                        loading={loading}
                        columns={clientColumns}
                        dataSource={summary?.clients ?? []}
                        pagination={{ pageSize: 20 }}
                        scroll={{ x: 860 }}
                      />
                    ),
                  },
                  {
                    key: 'alerts',
                    label: `${t('pages.audit.tabs.alerts')} (${summary?.unackedAlerts ?? 0})`,
                    children: (
                      <div>
                        <Space style={{ marginBottom: 12 }}>
                          <Switch
                            aria-label={t('pages.audit.alerts.onlyUnacked')}
                            checked={onlyUnacked}
                            onChange={setOnlyUnacked}
                          />
                          <span>{t('pages.audit.alerts.onlyUnacked')}</span>
                          <Button
                            size="small"
                            icon={<CheckOutlined aria-hidden />}
                            onClick={() => void handleAckAll()}
                          >
                            {t('pages.audit.alerts.ackAll')}
                          </Button>
                        </Space>
                        <Table<AccessAuditAlert>
                          rowKey="id"
                          size="small"
                          loading={loading}
                          columns={alertColumns}
                          dataSource={alerts}
                          pagination={{ pageSize: 20 }}
                          scroll={{ x: 960 }}
                        />
                      </div>
                    ),
                  },
                  {
                    key: 'rules',
                    label: t('pages.audit.tabs.rules'),
                    children: (
                      <Form layout="vertical" style={{ maxWidth: 720 }}>
                        <Row gutter={16}>
                          <Col xs={24} sm={8}>
                            <Form.Item
                              htmlFor={`${uid}-enabled`}
                              label={t('pages.audit.rules.enabled')}
                            >
                              <Switch
                                id={`${uid}-enabled`}
                                checked={config.enabled}
                                onChange={(checked) =>
                                  setConfig((prev) => ({ ...prev, enabled: checked }))
                                }
                              />
                            </Form.Item>
                          </Col>
                          <Col xs={24} sm={8}>
                            <Form.Item
                              htmlFor={`${uid}-retention`}
                              label={t('pages.audit.rules.retentionDays')}
                            >
                              <InputNumber
                                id={`${uid}-retention`}
                                min={1}
                                max={365}
                                value={config.retentionDays}
                                onChange={(v) =>
                                  setConfig((prev) => ({ ...prev, retentionDays: Number(v || 30) }))
                                }
                                style={{ width: '100%' }}
                              />
                            </Form.Item>
                          </Col>
                        </Row>
                        <Row gutter={16}>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              htmlFor={`${uid}-concurrent`}
                              label={t('pages.audit.rules.maxConcurrentSubnets')}
                              help={t('pages.audit.rules.maxConcurrentSubnetsHelp')}
                            >
                              <InputNumber
                                id={`${uid}-concurrent`}
                                min={2}
                                max={50}
                                value={config.maxConcurrentSubnets}
                                onChange={(v) =>
                                  setConfig((prev) => ({
                                    ...prev,
                                    maxConcurrentSubnets: v == null ? 3 : Number(v),
                                  }))
                                }
                                style={{ width: '100%' }}
                              />
                            </Form.Item>
                          </Col>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              htmlFor={`${uid}-daily`}
                              label={t('pages.audit.rules.maxDailySubnets')}
                              validateStatus={dailyThresholdInvalid ? 'error' : undefined}
                              help={
                                dailyThresholdInvalid
                                  ? t('pages.audit.rules.maxDailySubnetsInvalid')
                                  : t('pages.audit.rules.maxDailySubnetsHelp')
                              }
                            >
                              <InputNumber
                                id={`${uid}-daily`}
                                min={0}
                                max={100}
                                value={config.maxDailySubnets}
                                onChange={(v) =>
                                  setConfig((prev) => ({
                                    ...prev,
                                    maxDailySubnets: v == null ? 8 : Number(v),
                                  }))
                                }
                                style={{ width: '100%' }}
                              />
                            </Form.Item>
                          </Col>
                        </Row>
                        <Row gutter={16}>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              htmlFor={`${uid}-min-hits`}
                              label={t('pages.audit.rules.minSharingHits')}
                              help={t('pages.audit.rules.minSharingHitsHelp')}
                            >
                              <InputNumber
                                id={`${uid}-min-hits`}
                                min={1}
                                max={1000}
                                value={config.minSharingHits}
                                onChange={(v) =>
                                  setConfig((prev) => ({
                                    ...prev,
                                    minSharingHits: v == null ? 5 : Number(v),
                                  }))
                                }
                                style={{ width: '100%' }}
                              />
                            </Form.Item>
                          </Col>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              htmlFor={`${uid}-min-windows`}
                              label={t('pages.audit.rules.minConcurrentWindows')}
                              help={t('pages.audit.rules.minConcurrentWindowsHelp')}
                            >
                              <InputNumber
                                id={`${uid}-min-windows`}
                                min={1}
                                max={144}
                                value={config.minConcurrentWindows}
                                onChange={(v) =>
                                  setConfig((prev) => ({
                                    ...prev,
                                    minConcurrentWindows: v == null ? 2 : Number(v),
                                  }))
                                }
                                style={{ width: '100%' }}
                              />
                            </Form.Item>
                          </Col>
                        </Row>
                        <Form.Item
                          htmlFor={`${uid}-domains`}
                          label={t('pages.audit.rules.sensitiveDomains')}
                        >
                          <Input.TextArea
                            id={`${uid}-domains`}
                            rows={5}
                            value={domainsText}
                            onChange={(e) => setDomainsText(e.target.value)}
                            placeholder={t('pages.audit.rules.sensitiveDomainsPlaceholder')}
                          />
                        </Form.Item>
                        <Form.Item
                          htmlFor={`${uid}-keywords`}
                          label={t('pages.audit.rules.sensitiveKeywords')}
                        >
                          <Input.TextArea
                            id={`${uid}-keywords`}
                            rows={4}
                            value={keywordsText}
                            onChange={(e) => setKeywordsText(e.target.value)}
                            placeholder={t('pages.audit.rules.sensitiveKeywordsPlaceholder')}
                          />
                        </Form.Item>
                        <Form.Item
                          htmlFor={`${uid}-noise`}
                          label={t('pages.audit.rules.ignoreNoiseDomains')}
                        >
                          <Input.TextArea
                            id={`${uid}-noise`}
                            rows={3}
                            value={noiseText}
                            onChange={(e) => setNoiseText(e.target.value)}
                            placeholder={t('pages.audit.rules.ignoreNoiseDomainsPlaceholder')}
                          />
                        </Form.Item>
                        <Form.Item
                          htmlFor={`${uid}-trusted-targets`}
                          label={t('pages.audit.rules.trustedTargets')}
                          help={t('pages.audit.rules.trustedTargetsHelp')}
                        >
                          <Input.TextArea
                            id={`${uid}-trusted-targets`}
                            rows={4}
                            value={trustedTargetsText}
                            onChange={(e) => setTrustedTargetsText(e.target.value)}
                            placeholder={t('pages.audit.rules.trustedTargetsPlaceholder')}
                          />
                        </Form.Item>
                        <Form.Item
                          htmlFor={`${uid}-trusted-clients`}
                          label={t('pages.audit.rules.trustedClients')}
                          help={t('pages.audit.rules.trustedClientsHelp')}
                        >
                          <Input.TextArea
                            id={`${uid}-trusted-clients`}
                            rows={3}
                            value={trustedClientsText}
                            onChange={(e) => setTrustedClientsText(e.target.value)}
                            placeholder={t('pages.audit.rules.trustedClientsPlaceholder')}
                          />
                        </Form.Item>
                        <Space wrap>
                          <Button
                            type="primary"
                            icon={<SaveOutlined aria-hidden />}
                            loading={savingConfig}
                            disabled={dailyThresholdInvalid}
                            onClick={() => void handleSaveConfig(false)}
                          >
                            {t('pages.audit.rules.save')}
                          </Button>
                          <Button
                            icon={<SyncOutlined aria-hidden />}
                            loading={savingConfig}
                            disabled={dailyThresholdInvalid}
                            onClick={() => void handleSaveConfig(true)}
                          >
                            {t('pages.audit.rules.saveAndReevaluate')}
                          </Button>
                        </Space>
                      </Form>
                    ),
                  },
                  {
                    key: 'notifications',
                    label: t('pages.audit.tabs.notifications'),
                    children: <AuditNotifications sources={sources} />,
                  },
                ]}
              />
            </Card>

            <Drawer
              open={!!selectedEmail}
              onClose={() => setSelectedEmail(null)}
              size={880}
              title={t('pages.audit.detail.title', { email: selectedEmail || '' })}
              extra={
                <Space>
                  <Switch
                    aria-label={t('pages.audit.detail.hideNoise')}
                    checked={hideNoise}
                    onChange={setHideNoise}
                  />
                  <span>{t('pages.audit.detail.hideNoise')}</span>
                  {detail && detail.noiseHits > 0 && (
                    <Tag>{t('pages.audit.detail.noiseFiltered', { count: detail.noiseHits })}</Tag>
                  )}
                </Space>
              }
            >
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Space wrap>
                  {(detail?.categories || []).map((c) => (
                    <Tag key={c.category} color={categoryColor(c.category)}>
                      {categoryLabel(t, c.category)}: {c.hitCount} ({c.domainCount})
                    </Tag>
                  ))}
                </Space>

                <div>
                  <Typography.Title level={5}>
                    {t('pages.audit.detail.domainsSection')}
                  </Typography.Title>
                  <Table<AuditDomainGroup>
                    rowKey="rootDomain"
                    size="small"
                    loading={detailLoading}
                    columns={domainColumns}
                    dataSource={detail?.domains ?? []}
                    pagination={{ pageSize: 15 }}
                    expandable={{
                      expandedRowRender: (group) => (
                        <Table
                          rowKey={(r) => `${r.domain}:${r.port}`}
                          size="small"
                          pagination={false}
                          dataSource={group.subdomains}
                          columns={[
                            {
                              title: t('pages.audit.columns.domain'),
                              dataIndex: 'domain',
                              key: 'domain',
                            },
                            {
                              title: t('pages.audit.columns.port'),
                              dataIndex: 'port',
                              key: 'port',
                              width: 80,
                            },
                            {
                              title: t('pages.audit.columns.outbound'),
                              dataIndex: 'outbound',
                              key: 'outbound',
                              width: 120,
                            },
                            {
                              title: t('pages.audit.columns.source'),
                              dataIndex: 'nodeIds',
                              key: 'nodeIds',
                              width: 140,
                              render: (ids: number[]) => sourceTags(ids),
                            },
                            {
                              title: t('pages.audit.columns.hits'),
                              dataIndex: 'hitCount',
                              key: 'hitCount',
                              width: 80,
                            },
                            {
                              title: t('pages.audit.columns.lastSeen'),
                              dataIndex: 'lastSeen',
                              key: 'lastSeen',
                              width: 170,
                              render: (ts: number) => formatTs(ts),
                            },
                          ]}
                        />
                      ),
                    }}
                  />
                </div>

                <div>
                  <Typography.Title level={5}>
                    {t('pages.audit.detail.ipsSection')}
                  </Typography.Title>
                  <Table<AuditClientIPView>
                    rowKey={(r) => `${r.srcIp}#${r.nodeId}`}
                    size="small"
                    loading={detailLoading}
                    columns={ipColumns}
                    dataSource={detail?.sourceIps ?? []}
                    pagination={false}
                    scroll={{ x: 640 }}
                  />
                </div>
              </Space>
            </Drawer>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
