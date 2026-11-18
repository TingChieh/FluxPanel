import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Empty, Modal, Space, Spin, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import {
  NodeTrafficBreakdownSchema,
  type NodeClientTrafficItem,
  type NodeRecord,
  type NodeTrafficBreakdown,
} from '@/schemas/node';
import { HttpUtil, SizeFormatter } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';

interface NodeTrafficModalProps {
  node: NodeRecord | null;
  onClose: () => void;
}

export default function NodeTrafficModal({ node, onClose }: NodeTrafficModalProps) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [data, setData] = useState<NodeTrafficBreakdown | null>(null);

  const nodeId = node?.id ?? null;
  const [syncedId, setSyncedId] = useState<number | null>(nodeId);
  if (nodeId !== syncedId) {
    setSyncedId(nodeId);
    setData(null);
    setError('');
    setLoading(Boolean(node && node.id > 0));
  }

  useEffect(() => {
    if (!node || node.id <= 0) return;
    let cancelled = false;
    (async () => {
      try {
        const msg = await HttpUtil.get(`/panel/api/nodes/trafficBreakdown/${node.id}`, undefined, {
          silent: true,
        });
        if (cancelled) return;
        if (!msg?.success) {
          setError(msg?.msg || t('somethingWentWrong'));
          return;
        }
        const parsed = parseMsg(msg, NodeTrafficBreakdownSchema, 'nodes/trafficBreakdown');
        setData(parsed.obj ?? null);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('somethingWentWrong'));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [node, t]);

  const columns: ColumnsType<NodeClientTrafficItem> = [
    {
      title: t('pages.clients.email'),
      dataIndex: 'email',
      key: 'email',
      ellipsis: true,
    },
    {
      title: t('pages.index.upload'),
      dataIndex: 'up',
      key: 'up',
      align: 'right',
      render: (v: number) => SizeFormatter.sizeFormat(v),
    },
    {
      title: t('pages.index.download'),
      dataIndex: 'down',
      key: 'down',
      align: 'right',
      render: (v: number) => SizeFormatter.sizeFormat(v),
    },
    {
      title: t('pages.inbounds.totalUsage'),
      dataIndex: 'total',
      key: 'total',
      align: 'right',
      render: (v: number) => <Tag>{SizeFormatter.sizeFormat(v)}</Tag>,
    },
  ];

  const totalUp = data?.totalUp ?? node?.totalUp ?? 0;
  const totalDown = data?.totalDown ?? node?.totalDown ?? 0;
  const totalTraffic = data?.totalTraffic ?? node?.totalTraffic ?? totalUp + totalDown;

  return (
    <Modal
      open={!!node}
      title={node ? `${t('pages.nodes.trafficBreakdown')} — ${node.name || ''}` : ''}
      footer={null}
      width={620}
      onCancel={onClose}
    >
      <Space wrap style={{ marginBottom: 12 }}>
        <Tag color="blue">↑ {SizeFormatter.sizeFormat(totalUp)}</Tag>
        <Tag color="cyan">↓ {SizeFormatter.sizeFormat(totalDown)}</Tag>
        <Tag color="purple">{SizeFormatter.sizeFormat(totalTraffic)}</Tag>
      </Space>

      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}

      <Spin aria-live="off" spinning={loading}>
        {!loading &&
        !error &&
        (!data || !Array.isArray(data.clients) || data.clients.length === 0) ? (
          <Empty description={t('noData')} />
        ) : (
          <Table<NodeClientTrafficItem>
            rowKey="email"
            size="small"
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            dataSource={Array.isArray(data?.clients) ? data.clients : []}
            columns={columns}
          />
        )}
      </Spin>
    </Modal>
  );
}
