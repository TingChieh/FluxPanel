import { useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import dayjs from 'dayjs';
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  TimePicker,
  Typography,
} from 'antd';
import { EyeOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import type {
  AuditDigestConfig,
  AuditDigestPreview,
  AuditSourceSummary,
  AuditTelegramConfig,
} from '@/generated/types';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

function telegramHtmlToText(html: string): string {
  return html
    .replace(/<\/?[a-z]+>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, '&');
}

interface AuditNotificationsProps {
  sources: AuditSourceSummary[];
}

export default function AuditNotifications({ sources }: AuditNotificationsProps) {
  const { t } = useTranslation();
  const uid = useId();

  const [telegram, setTelegram] = useState<AuditTelegramConfig>({
    enabled: false,
    chatId: '',
    hasBotToken: false,
  });
  const [botToken, setBotToken] = useState('');
  const [savingTelegram, setSavingTelegram] = useState(false);
  const [testingTelegram, setTestingTelegram] = useState(false);

  const [digest, setDigest] = useState<AuditDigestConfig>({
    enabled: false,
    sendAt: '09:00',
    lang: 'zh-CN',
    deliveryNodeId: 0,
    lastSentDate: '',
    lastAttemptAt: 0,
    lastError: '',
  });
  const [savingDigest, setSavingDigest] = useState(false);
  const [sendingDigest, setSendingDigest] = useState(false);
  const [preview, setPreview] = useState<AuditDigestPreview | null>(null);

  const loadDigest = async () => {
    const res = await HttpUtil.get<AuditDigestConfig>('/panel/api/audit/digest');
    if (res?.success && res.obj) setDigest(res.obj);
  };

  useEffect(() => {
    let active = true;
    void Promise.all([
      HttpUtil.get<AuditTelegramConfig>('/panel/api/audit/telegram'),
      HttpUtil.get<AuditDigestConfig>('/panel/api/audit/digest'),
    ]).then(([tg, dg]) => {
      if (!active) return;
      if (tg?.success && tg.obj) setTelegram(tg.obj);
      if (dg?.success && dg.obj) setDigest(dg.obj);
    });
    return () => {
      active = false;
    };
  }, []);

  const saveTelegram = async () => {
    setSavingTelegram(true);
    try {
      const res = await HttpUtil.post(
        '/panel/api/audit/telegram',
        { enabled: telegram.enabled, chatId: telegram.chatId, botToken },
        JSON_HEADERS,
      );
      if (res?.success) {
        setTelegram((prev) => ({ ...prev, hasBotToken: prev.hasBotToken || botToken !== '' }));
        setBotToken('');
      }
    } finally {
      setSavingTelegram(false);
    }
  };

  const testTelegram = async () => {
    setTestingTelegram(true);
    try {
      await HttpUtil.post('/panel/api/audit/telegram/test');
    } finally {
      setTestingTelegram(false);
    }
  };

  const saveDigest = async () => {
    setSavingDigest(true);
    try {
      const res = await HttpUtil.post(
        '/panel/api/audit/digest',
        {
          enabled: digest.enabled,
          sendAt: digest.sendAt,
          lang: digest.lang,
          deliveryNodeId: digest.deliveryNodeId,
        },
        JSON_HEADERS,
      );
      if (res?.success) await loadDigest();
    } finally {
      setSavingDigest(false);
    }
  };

  const previewDigest = async () => {
    const res = await HttpUtil.get<AuditDigestPreview>('/panel/api/audit/digest/preview');
    if (res?.success && res.obj) setPreview(res.obj);
  };

  const sendDigest = async () => {
    setSendingDigest(true);
    try {
      await HttpUtil.post('/panel/api/audit/digest/send');
    } finally {
      setSendingDigest(false);
    }
  };

  const [hour, minute] = digest.sendAt.split(':').map((v) => Number(v) || 0);
  const deliveryOptions = [
    { value: 0, label: t('pages.audit.notify.deliveryLocal') },
    ...sources
      .filter((s) => s.nodeId > 0)
      .map((s) => ({
        value: s.nodeId,
        label: t('pages.audit.notify.deliveryNode', { name: s.name }),
      })),
  ];

  return (
    <Row gutter={[16, 16]}>
      <Col xs={24} lg={12}>
        <Card size="small" title={t('pages.audit.notify.telegramTitle')}>
          <Form layout="vertical">
            <Typography.Paragraph type="secondary">
              {t('pages.audit.notify.telegramHelp')}
            </Typography.Paragraph>
            <Form.Item htmlFor={`${uid}-tg-enabled`} label={t('pages.audit.notify.enabled')}>
              <Switch
                id={`${uid}-tg-enabled`}
                checked={telegram.enabled}
                onChange={(checked) => setTelegram((prev) => ({ ...prev, enabled: checked }))}
              />
            </Form.Item>
            <Form.Item htmlFor={`${uid}-tg-chat`} label={t('pages.audit.notify.chatId')}>
              <Input
                id={`${uid}-tg-chat`}
                value={telegram.chatId}
                onChange={(e) => setTelegram((prev) => ({ ...prev, chatId: e.target.value }))}
              />
            </Form.Item>
            <Form.Item htmlFor={`${uid}-tg-token`} label={t('pages.audit.notify.botToken')}>
              <Input.Password
                id={`${uid}-tg-token`}
                autoComplete="new-password"
                value={botToken}
                onChange={(e) => setBotToken(e.target.value)}
                placeholder={
                  telegram.hasBotToken
                    ? t('pages.audit.notify.botTokenKept')
                    : t('pages.audit.notify.botTokenMissing')
                }
              />
            </Form.Item>
            <Space wrap>
              <Button
                type="primary"
                icon={<SaveOutlined aria-hidden />}
                loading={savingTelegram}
                onClick={() => void saveTelegram()}
              >
                {t('pages.audit.notify.saveTelegram')}
              </Button>
              <Button
                icon={<SendOutlined aria-hidden />}
                loading={testingTelegram}
                disabled={!telegram.hasBotToken}
                onClick={() => void testTelegram()}
              >
                {t('pages.audit.notify.testTelegram')}
              </Button>
            </Space>
          </Form>
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card size="small" title={t('pages.audit.notify.digestTitle')}>
          <Form layout="vertical">
            {digest.lastError && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 12 }}
                message={t('pages.audit.notify.lastError', { error: digest.lastError })}
              />
            )}
            <Form.Item htmlFor={`${uid}-dg-enabled`} label={t('pages.audit.notify.enabled')}>
              <Switch
                id={`${uid}-dg-enabled`}
                checked={digest.enabled}
                onChange={(checked) => setDigest((prev) => ({ ...prev, enabled: checked }))}
              />
            </Form.Item>
            <Row gutter={12}>
              <Col xs={24} sm={12}>
                <Form.Item htmlFor={`${uid}-dg-time`} label={t('pages.audit.notify.sendAt')}>
                  <TimePicker
                    id={`${uid}-dg-time`}
                    format="HH:mm"
                    allowClear={false}
                    value={dayjs().hour(hour).minute(minute).second(0)}
                    onChange={(v) =>
                      v && setDigest((prev) => ({ ...prev, sendAt: v.format('HH:mm') }))
                    }
                    style={{ width: '100%' }}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={12}>
                <Form.Item htmlFor={`${uid}-dg-lang`} label={t('pages.audit.notify.lang')}>
                  <Select
                    id={`${uid}-dg-lang`}
                    value={digest.lang}
                    onChange={(lang) => setDigest((prev) => ({ ...prev, lang }))}
                    options={[
                      { value: 'zh-CN', label: '简体中文' },
                      { value: 'en-US', label: 'English' },
                    ]}
                  />
                </Form.Item>
              </Col>
            </Row>
            <Form.Item
              htmlFor={`${uid}-dg-delivery`}
              label={t('pages.audit.notify.delivery')}
              help={t('pages.audit.notify.deliveryHelp')}
            >
              <Select
                id={`${uid}-dg-delivery`}
                value={digest.deliveryNodeId}
                onChange={(deliveryNodeId) => setDigest((prev) => ({ ...prev, deliveryNodeId }))}
                options={deliveryOptions}
              />
            </Form.Item>
            {digest.lastSentDate && (
              <Typography.Paragraph type="secondary">
                {t('pages.audit.notify.lastSent', { date: digest.lastSentDate })}
              </Typography.Paragraph>
            )}
            <Space wrap>
              <Button
                type="primary"
                icon={<SaveOutlined aria-hidden />}
                loading={savingDigest}
                onClick={() => void saveDigest()}
              >
                {t('pages.audit.notify.saveDigest')}
              </Button>
              <Button icon={<EyeOutlined aria-hidden />} onClick={() => void previewDigest()}>
                {t('pages.audit.notify.preview')}
              </Button>
              <Button
                icon={<SendOutlined aria-hidden />}
                loading={sendingDigest}
                onClick={() => void sendDigest()}
              >
                {t('pages.audit.notify.sendNow')}
              </Button>
            </Space>
          </Form>
        </Card>
      </Col>

      <Modal
        open={!!preview}
        onCancel={() => setPreview(null)}
        footer={null}
        title={t('pages.audit.notify.previewTitle', { date: preview?.date ?? '' })}
      >
        <Typography.Paragraph>
          <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
            {telegramHtmlToText(preview?.text ?? '')}
          </pre>
        </Typography.Paragraph>
      </Modal>
    </Row>
  );
}
