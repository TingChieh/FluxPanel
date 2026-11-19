import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Collapse, Modal, Spin, Tag } from 'antd';
import { HttpUtil } from '@/utils';
import { isPostQuantumLink } from '@/lib/xray/inbound-link';
import { LinkTags, linkMetaText, parseLinkParts } from '@/lib/xray/link-label';
import { QrPanel } from '@/pages/inbounds/qr';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import { formatTunnelConfigMeta } from '@/lib/inbounds/label';
import {
  buildWireguardClientConfig,
  findWireguardInbounds,
  isWireguardClient,
} from './wireguardConfig';
import { tunnelConfigEndpoints, tunnelEndpointLabel } from './tunnelEndpoints';
import type { HostRecord } from '@/schemas/api/host';

interface SubSettings {
  enable: boolean;
  subURI: string;
  subJsonURI: string;
  subJsonEnable: boolean;
  publicHost?: string;
}

interface ClientQrModalProps {
  open: boolean;
  client: ClientRecord | null;
  inboundsById: Record<number, InboundOption>;
  tunnelAllowedIPs?: Record<number, string>;
  subSettings?: SubSettings;
  hosts?: HostRecord[];
  onOpenChange: (open: boolean) => void;
}

const NO_HOSTS: HostRecord[] = [];

interface ApiMsg<T = unknown> {
  success?: boolean;
  obj?: T;
}

const DEFAULT_SUB: SubSettings = {
  enable: false,
  subURI: '',
  subJsonURI: '',
  subJsonEnable: false,
  publicHost: '',
};

export default function ClientQrModal({
  open,
  client,
  inboundsById,
  tunnelAllowedIPs,
  subSettings = DEFAULT_SUB,
  hosts = NO_HOSTS,
  onOpenChange,
}: ClientQrModalProps) {
  const { t } = useTranslation();
  const [links, setLinks] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);

  const subId = client?.subId;
  const subEnabled = !!subSettings?.enable;
  const subLink = subId && subEnabled && subSettings?.subURI ? subSettings.subURI + subId : '';
  const subJsonLink =
    subId && subEnabled && subSettings?.subJsonEnable && subSettings?.subJsonURI
      ? subSettings.subJsonURI + subId
      : '';

  const wgInbounds = useMemo(
    () => findWireguardInbounds(client, inboundsById),
    [client, inboundsById],
  );
  const wgConfigs = useMemo(() => {
    if (!client || !isWireguardClient(client)) return [];
    const host = window.location.hostname;
    const publicHost = subSettings.publicHost ?? '';
    return wgInbounds
      .flatMap((ib) => {
        const address = tunnelAllowedIPs?.[ib.id] ?? '';
        return tunnelConfigEndpoints(ib, hosts, host, publicHost).map((ep) => ({
          inbound: ib,
          endpoint: tunnelEndpointLabel(ep),
          text: buildWireguardClientConfig(client, ib, host, publicHost, address, ep),
        }));
      })
      .filter((c) => !!c.text);
  }, [client, wgInbounds, tunnelAllowedIPs, subSettings.publicHost, hosts]);

  const hasAnything = !!subLink || !!subJsonLink || wgConfigs.length > 0 || links.length > 0;

  // The reset runs during render so the effect only carries the request.
  const openSubId = open ? (client?.subId ?? '') : '';
  const [syncedSubId, setSyncedSubId] = useState(openSubId);
  if (openSubId !== syncedSubId) {
    setSyncedSubId(openSubId);
    setLinks([]);
    setLoading(!!openSubId);
  }

  useEffect(() => {
    if (!open || !client?.subId) return;
    let cancelled = false;
    (async () => {
      try {
        const msg = (await HttpUtil.get(
          `/panel/api/clients/subLinks/${encodeURIComponent(client.subId!)}`,
        )) as ApiMsg<string[]>;
        if (!cancelled) {
          setLinks(msg?.success && Array.isArray(msg.obj) ? msg.obj : []);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [open, client?.subId]);

  const [activeKey, setActiveKey] = useState<string[]>([]);

  const items = useMemo(() => {
    const out: { key: string; label: React.ReactNode; children: React.ReactNode }[] = [];
    if (subLink) {
      out.push({
        key: 'sub',
        label: t('subscription.title'),
        children: (
          <QrPanel value={subLink} remark={`${client?.email || ''} — ${t('subscription.title')}`} />
        ),
      });
    }
    if (subJsonLink) {
      out.push({
        key: 'subJson',
        label: `${t('subscription.title')} (JSON)`,
        children: <QrPanel value={subJsonLink} remark={`${client?.email || ''} — JSON`} />,
      });
    }
    links.forEach((link, idx) => {
      const parts = parseLinkParts(link);
      const meta = parts ? linkMetaText(parts) : '';
      const label: React.ReactNode = parts ? (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
          <LinkTags parts={parts} />
          {meta && <span style={{ opacity: 0.6, fontSize: 12 }}>({meta})</span>}
        </span>
      ) : (
        `${t('pages.clients.link')} ${idx + 1}`
      );
      out.push({
        key: `l${idx}`,
        label,
        children: (
          <QrPanel
            value={link}
            remark={parts?.remark || `${client?.email || ''} #${idx + 1}`}
            showQr={!isPostQuantumLink(link)}
          />
        ),
      });
    });
    wgConfigs.forEach(({ inbound, endpoint, text }) => {
      const meta = formatTunnelConfigMeta(inbound, client?.email, wgConfigs.length, endpoint);
      const label = (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Tag color="cyan" style={{ margin: 0 }}>
            {t('pages.clients.wireguardConfig')}
          </Tag>
          {meta.label && <span style={{ opacity: 0.85, fontSize: 12 }}>{meta.label}</span>}
        </span>
      );
      out.push({
        key: `wg-config-${inbound.id}-${endpoint}`,
        label,
        children: <QrPanel value={text} remark={meta.qrRemark} downloadName={meta.fileName} />,
      });
    });
    return out;
  }, [subLink, subJsonLink, wgConfigs, links, client?.email, t]);

  // Expanding the first panel is a render-time adjustment, not a side effect.
  const firstKey = open && items.length > 0 ? items[0].key : null;
  const [syncedFirstKey, setSyncedFirstKey] = useState<string | null>(null);
  if (firstKey !== syncedFirstKey) {
    setSyncedFirstKey(firstKey);
    setActiveKey(firstKey ? [firstKey] : []);
  }

  return (
    <Modal
      open={open}
      title={client ? `${t('qrCode')} — ${client.email}` : t('qrCode')}
      footer={null}
      width={520}
      centered
      onCancel={() => onOpenChange(false)}
    >
      <Spin aria-live="off" spinning={loading}>
        {!client?.subId && !loading && (
          <div style={{ padding: 24, textAlign: 'center', opacity: 0.6 }}>
            {t('pages.clients.noSubId')}
          </div>
        )}
        {client?.subId && !hasAnything && !loading && (
          <div style={{ padding: 24, textAlign: 'center', opacity: 0.6 }}>
            {t('pages.clients.noLinks')}
          </div>
        )}
        {hasAnything && (
          <Collapse
            activeKey={activeKey}
            onChange={(keys) =>
              setActiveKey(typeof keys === 'string' ? [keys] : (keys as string[]))
            }
            items={items}
          />
        )}
      </Spin>
    </Modal>
  );
}
