import { useEffect, useState } from 'react';

import { getSharedWebSocketClient } from '@/api/websocket';
import type { LiveSpeed } from '@/schemas/traffic';

export interface LiveTrafficSpeedsState {
  connected: boolean;
  receivedLive: boolean;
  lastUpdatedAt: number | null;
  totalSpeed: LiveSpeed;
  userSpeeds: Record<string, LiveSpeed>;
  nodeSpeeds: Record<number, LiveSpeed>;
  onlineEmails: Set<string> | null;
}

interface TrafficWsPayload {
  liveTotalSpeed?: { up?: number; down?: number };
  liveUserSpeeds?: Record<string, { up?: number; down?: number }>;
  liveNodeSpeeds?: Record<string, { up?: number; down?: number }>;
  onlineClients?: string[];
}

function toSpeed(raw: { up?: number; down?: number } | undefined): LiveSpeed {
  return {
    up: Math.max(0, Number(raw?.up) || 0),
    down: Math.max(0, Number(raw?.down) || 0),
  };
}

export function useLiveTrafficSpeeds(): LiveTrafficSpeedsState {
  const [state, setState] = useState<LiveTrafficSpeedsState>(() => ({
    connected: getSharedWebSocketClient().isConnected,
    receivedLive: false,
    lastUpdatedAt: null,
    totalSpeed: { up: 0, down: 0 },
    userSpeeds: {},
    nodeSpeeds: {},
    onlineEmails: null,
  }));

  useEffect(() => {
    const client = getSharedWebSocketClient();

    const onOpen = () => {
      setState((prev) => (prev.connected ? prev : { ...prev, connected: true }));
    };
    const onClose = () => {
      setState((prev) => (!prev.connected ? prev : { ...prev, connected: false }));
    };
    const onTraffic = (raw: unknown) => {
      if (!raw || typeof raw !== 'object') return;
      const p = raw as TrafficWsPayload;
      if (!p.liveTotalSpeed && !p.liveUserSpeeds && !p.liveNodeSpeeds) return;

      const userSpeeds: Record<string, LiveSpeed> = {};
      if (p.liveUserSpeeds && typeof p.liveUserSpeeds === 'object') {
        for (const [email, sp] of Object.entries(p.liveUserSpeeds)) {
          const parsed = toSpeed(sp);
          if (parsed.up > 0 || parsed.down > 0) {
            userSpeeds[email] = parsed;
          }
        }
      }

      const nodeSpeeds: Record<number, LiveSpeed> = {};
      if (p.liveNodeSpeeds && typeof p.liveNodeSpeeds === 'object') {
        for (const [idStr, sp] of Object.entries(p.liveNodeSpeeds)) {
          const id = Number(idStr);
          if (!Number.isInteger(id) || id < 0) continue;
          const parsed = toSpeed(sp);
          if (parsed.up > 0 || parsed.down > 0) {
            nodeSpeeds[id] = parsed;
          }
        }
      }

      const onlineEmails = Array.isArray(p.onlineClients)
        ? new Set(p.onlineClients.filter((e): e is string => typeof e === 'string'))
        : null;

      setState({
        connected: true,
        receivedLive: true,
        lastUpdatedAt: Date.now(),
        totalSpeed: toSpeed(p.liveTotalSpeed),
        userSpeeds,
        nodeSpeeds,
        onlineEmails,
      });
    };

    client.on('open', onOpen);
    client.on('close', onClose);
    client.on('traffic', onTraffic);
    client.connect();

    return () => {
      client.off('open', onOpen);
      client.off('close', onClose);
      client.off('traffic', onTraffic);
    };
  }, []);

  return state;
}
