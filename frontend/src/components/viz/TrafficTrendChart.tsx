import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

import { useTheme } from '@/hooks/useTheme';
import { SizeFormatter } from '@/utils';
import type { TrafficSeriesPoint } from '@/schemas/traffic';
import './TrafficTrendChart.css';

interface TrafficTrendChartProps {
  series: TrafficSeriesPoint[];
  height?: number;
  ariaLabel?: string;
}

const DOWN_COLOR = '#1677ff';
const UP_COLOR = '#13c2c2';

interface HoverState {
  idx: number;
  left: number;
  width: number;
}

export default function TrafficTrendChart({
  series,
  height = 240,
  ariaLabel,
}: TrafficTrendChartProps) {
  const { t } = useTranslation();
  const { isDark } = useTheme();
  const hostRef = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);
  const [hover, setHover] = useState<HoverState | null>(null);

  const chartData = useMemo<uPlot.AlignedData>(() => {
    const xs = series.map((_, i) => i);
    const downs = series.map((pt) => (pt.future ? null : pt.down));
    const ups = series.map((pt) => (pt.future ? null : pt.up));
    return [xs, downs, ups];
  }, [series]);

  const maxVal = useMemo(() => {
    let m = 0;
    for (const pt of series) {
      if (pt.future) continue;
      if (pt.down > m) m = pt.down;
      if (pt.up > m) m = pt.up;
    }
    return m;
  }, [series]);

  useEffect(() => {
    const el = hostRef.current;
    if (!el || series.length === 0) return;

    const axisStroke = isDark ? 'rgba(255, 255, 255, 0.58)' : 'rgba(0, 0, 0, 0.55)';
    const gridStroke = isDark ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.08)';
    const fillGradient = (u: uPlot) => {
      const ctx = u.ctx;
      const g = ctx.createLinearGradient(0, u.bbox.top, 0, u.bbox.top + u.bbox.height);
      g.addColorStop(0, isDark ? 'rgba(22, 119, 255, 0.34)' : 'rgba(22, 119, 255, 0.22)');
      g.addColorStop(1, 'rgba(22, 119, 255, 0.01)');
      return g;
    };

    const width = Math.max(el.clientWidth, 280);
    const step = series.length > 16 ? Math.ceil(series.length / 8) : 1;

    const opts: uPlot.Options = {
      width,
      height,
      padding: [12, 12, 0, 4],
      legend: { show: false },
      cursor: {
        drag: { x: false, y: false },
        points: { size: 7, width: 2 },
      },
      scales: {
        x: { time: false, range: [0, Math.max(series.length - 1, 1)] },
        y: {
          auto: false,
          range: () => [0, maxVal > 0 ? maxVal * 1.15 : 1024],
        },
      },
      axes: [
        {
          stroke: axisStroke,
          grid: { show: false },
          ticks: { show: false },
          font: '11px system-ui, -apple-system, sans-serif',
          splits: () => {
            const ticks: number[] = [];
            for (let i = 0; i < series.length; i += step) ticks.push(i);
            const last = series.length - 1;
            if (last > 0 && ticks[ticks.length - 1] !== last) ticks.push(last);
            return ticks;
          },
          values: (_u, splits) => splits.map((idx) => series[idx]?.label ?? ''),
        },
        {
          stroke: axisStroke,
          grid: { stroke: gridStroke, width: 1 },
          ticks: { show: false },
          size: 64,
          font: '11px system-ui, -apple-system, sans-serif',
          values: (_u, splits) => splits.map((v) => (v <= 0 ? '0' : SizeFormatter.sizeFormat(v))),
        },
      ],
      series: [
        {},
        {
          label: t('pages.traffic.download'),
          stroke: DOWN_COLOR,
          width: 2,
          fill: fillGradient,
          spanGaps: false,
          paths: uPlot.paths?.spline?.(),
        },
        {
          label: t('pages.traffic.upload'),
          stroke: UP_COLOR,
          width: 2,
          spanGaps: false,
          paths: uPlot.paths?.spline?.(),
        },
      ],
      hooks: {
        setCursor: [
          (u) => {
            const idx = u.cursor.idx;
            const left = u.cursor.left;
            if (idx == null || left == null || left < 0 || !series[idx]) {
              setHover(null);
              return;
            }
            setHover({ idx, left, width: u.width || width });
          },
        ],
      },
    };

    const plot = new uPlot(opts, chartData, el);
    plotRef.current = plot;

    let ro: ResizeObserver | null = null;
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver((entries) => {
        const entry = entries[0];
        if (!entry || !plotRef.current) return;
        const nextW = Math.floor(entry.contentRect.width);
        if (nextW > 0) {
          plotRef.current.setSize({ width: nextW, height });
        }
      });
      ro.observe(el);
    }

    return () => {
      ro?.disconnect();
      plot.destroy();
      plotRef.current = null;
      setHover(null);
    };
  }, [chartData, height, isDark, maxVal, series, t]);

  const hoveredPoint = hover ? series[hover.idx] : null;
  const hostWidth = hover?.width ?? 600;
  const tooltipOnLeft = hover ? hover.left > hostWidth * 0.62 : false;

  return (
    <div className="traffic-trend-chart" role="img" aria-label={ariaLabel}>
      <div className="traffic-trend-legend">
        <span className="traffic-trend-legend-item">
          <i className="traffic-trend-swatch" style={{ background: DOWN_COLOR }} />
          {t('pages.traffic.download')}
        </span>
        <span className="traffic-trend-legend-item">
          <i className="traffic-trend-swatch" style={{ background: UP_COLOR }} />
          {t('pages.traffic.upload')}
        </span>
      </div>
      <div className="traffic-trend-canvas-wrap">
        <div ref={hostRef} className="traffic-trend-host" style={{ minHeight: height }} />
        {hover && hoveredPoint && !hoveredPoint.future && (
          <div
            className={`traffic-trend-tooltip ${tooltipOnLeft ? 'is-left' : ''}`}
            style={{ left: Math.max(8, Math.min(hover.left + 12, hostWidth - 180)) }}
          >
            <div className="traffic-trend-tooltip-title">{hoveredPoint.label}</div>
            <div className="traffic-trend-tooltip-row">
              <span>{t('pages.traffic.total')}</span>
              <strong>{SizeFormatter.sizeFormat(hoveredPoint.total)}</strong>
            </div>
            <div className="traffic-trend-tooltip-row">
              <span>
                <i className="traffic-trend-swatch" style={{ background: DOWN_COLOR }} />
                {t('pages.traffic.download')}
              </span>
              <span>{SizeFormatter.sizeFormat(hoveredPoint.down)}</span>
            </div>
            <div className="traffic-trend-tooltip-row">
              <span>
                <i className="traffic-trend-swatch" style={{ background: UP_COLOR }} />
                {t('pages.traffic.upload')}
              </span>
              <span>{SizeFormatter.sizeFormat(hoveredPoint.up)}</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
