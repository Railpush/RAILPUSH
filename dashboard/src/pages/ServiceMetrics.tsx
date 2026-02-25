import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useParams } from 'react-router-dom';
import { Activity, Cpu, HardDrive, Network, RefreshCw } from 'lucide-react';
import { CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { Card } from '../components/ui/Card';

const BASE = '/api/v1';

type MetricsPeriod = '1h' | '6h' | '24h' | '7d';

const periodOptions: Array<{ key: MetricsPeriod; label: string }> = [
  { key: '1h', label: '1H' },
  { key: '6h', label: '6H' },
  { key: '24h', label: '24H' },
  { key: '7d', label: '7D' },
];

interface MetricsData {
  cpu_percent: string;
  memory_used: string;
  memory_total: string;
  memory_percent: string;
  network_in: string;
  network_out: string;
  pids: string;
  status: string;
  container_id: string;
  timestamp: string;
  source?: string;
  error?: string;
}

interface MetricPoint {
  timestamp: string;
  value: number;
}

interface MetricsHistory {
  service_id: string;
  status: string;
  source: string;
  period: string;
  start: string;
  end: string;
  step_seconds: number;
  series: {
    cpu_percent: MetricPoint[];
    memory_mb: MetricPoint[];
    network_in_bps: MetricPoint[];
    network_out_bps: MetricPoint[];
  };
  current: {
    cpu_percent: number;
    memory_mb: number;
    memory_percent: number;
    network_in_bps: number;
    network_out_bps: number;
  };
  error?: string;
}

function parseNumeric(input: unknown): number {
  if (typeof input === 'number') {
    return Number.isFinite(input) ? input : 0;
  }
  if (typeof input === 'string') {
    const cleaned = input.replace(/[^0-9.-]/g, '');
    const parsed = Number.parseFloat(cleaned);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`;
}

function formatMemoryMB(value: number): string {
  if (value <= 0) return '0 MiB';
  if (value >= 1024) return `${(value / 1024).toFixed(2)} GiB`;
  return `${value.toFixed(0)} MiB`;
}

function formatBytesPerSecond(value: number): string {
  if (value <= 0) return '0 B/s';
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s', 'TiB/s'];
  let current = value;
  let idx = 0;
  while (current >= 1024 && idx < units.length - 1) {
    current /= 1024;
    idx += 1;
  }
  return `${idx === 0 ? current.toFixed(0) : current.toFixed(2)} ${units[idx]}`;
}

export function ServiceMetrics() {
  const { serviceId } = useParams<{ serviceId: string }>();

  const [metrics, setMetrics] = useState<MetricsData | null>(null);
  const [history, setHistory] = useState<MetricsHistory | null>(null);
  const [loading, setLoading] = useState(true);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [period, setPeriod] = useState<MetricsPeriod>('1h');

  const fetchMetrics = async () => {
    if (!serviceId) return;
    try {
      const res = await fetch(`${BASE}/services/${serviceId}/metrics`, {
        credentials: 'include',
      });
      if (res.ok) {
        const data = await res.json();
        setMetrics(data);
      }
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  const fetchHistory = async () => {
    if (!serviceId) return;
    try {
      const res = await fetch(`${BASE}/services/${serviceId}/metrics/history?period=${period}`, {
        credentials: 'include',
      });
      if (res.ok) {
        const data = await res.json();
        setHistory(data);
      }
    } catch {
      // ignore
    } finally {
      setHistoryLoading(false);
    }
  };

  useEffect(() => {
    setLoading(true);
    setHistoryLoading(true);
    fetchMetrics();
    fetchHistory();
  }, [serviceId, period]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => {
      fetchMetrics();
      fetchHistory();
    }, 30000);
    return () => clearInterval(interval);
  }, [serviceId, period, autoRefresh]);

  const networkChartData = useMemo(() => {
    const byTimestamp = new Map<string, { timestamp: string; network_in_bps: number; network_out_bps: number }>();
    const inbound = history?.series?.network_in_bps ?? [];
    const outbound = history?.series?.network_out_bps ?? [];

    for (const point of inbound) {
      byTimestamp.set(point.timestamp, {
        timestamp: point.timestamp,
        network_in_bps: point.value,
        network_out_bps: byTimestamp.get(point.timestamp)?.network_out_bps ?? 0,
      });
    }
    for (const point of outbound) {
      byTimestamp.set(point.timestamp, {
        timestamp: point.timestamp,
        network_in_bps: byTimestamp.get(point.timestamp)?.network_in_bps ?? 0,
        network_out_bps: point.value,
      });
    }

    return Array.from(byTimestamp.values()).sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
  }, [history]);

  const formatTimeTick = (isoTimestamp: string) => {
    const dt = new Date(isoTimestamp);
    if (period === '7d') {
      return dt.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
    }
    return dt.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  };

  const cpuCurrent = history?.current?.cpu_percent ?? parseNumeric(metrics?.cpu_percent);
  const memoryCurrentMB = history?.current?.memory_mb ?? 0;
  const memoryCurrentPercent = history?.current?.memory_percent ?? parseNumeric(metrics?.memory_percent);
  const networkInCurrentBPS = history?.current?.network_in_bps ?? 0;
  const networkOutCurrentBPS = history?.current?.network_out_bps ?? 0;

  const hasHistoryData = (
    (history?.series?.cpu_percent?.length ?? 0) > 0
    || (history?.series?.memory_mb?.length ?? 0) > 0
    || (history?.series?.network_in_bps?.length ?? 0) > 0
    || (history?.series?.network_out_bps?.length ?? 0) > 0
  );

  const statCard = (icon: ReactNode, label: string, value: string, sub?: string) => (
    <Card>
      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-lg bg-brand/10 flex items-center justify-center flex-shrink-0">
          {icon}
        </div>
        <div>
          <div className="text-xs text-content-tertiary mb-0.5">{label}</div>
          <div className="text-xl font-semibold text-content-primary">{value}</div>
          {sub && <div className="text-xs text-content-secondary mt-0.5">{sub}</div>}
        </div>
      </div>
    </Card>
  );

  const chartCard = (title: string, content: ReactNode, subtitle?: string) => (
    <Card>
      <div className="mb-4">
        <div className="text-sm font-medium text-content-primary">{title}</div>
        {subtitle && <div className="text-xs text-content-tertiary mt-1">{subtitle}</div>}
      </div>
      <div className="h-64">
        {content}
      </div>
    </Card>
  );

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Metrics</h1>

        <div className="flex items-center gap-3">
          <div className="inline-flex rounded-md border border-border-default bg-surface-secondary p-0.5">
            {periodOptions.map((opt) => (
              <button
                key={opt.key}
                onClick={() => setPeriod(opt.key)}
                className={`px-2.5 py-1 text-xs rounded-sm transition-colors ${
                  period === opt.key
                    ? 'bg-brand/10 text-brand'
                    : 'text-content-secondary hover:text-content-primary'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>

          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
              autoRefresh
                ? 'bg-status-success-bg text-status-success border border-status-success/20'
                : 'bg-surface-tertiary text-content-secondary border border-border-default'
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${autoRefresh ? 'bg-status-success animate-pulse-dot' : 'bg-content-tertiary'}`} />
            Auto-refresh
          </button>

          <button
            onClick={() => {
              setLoading(true);
              setHistoryLoading(true);
              fetchMetrics();
              fetchHistory();
            }}
            className="p-2 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${(loading || historyLoading) ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {!metrics || (metrics.status !== 'live' && !hasHistoryData) ? (
        <Card>
          <div className="text-center py-8 text-content-secondary">
            <Activity className="w-8 h-8 mx-auto mb-3 opacity-40" />
            <p className="text-sm">
              {metrics?.status === 'suspended'
                ? 'Service is suspended. Resume it to see metrics.'
                : 'No metrics available. Deploy and start the service to see metrics.'}
            </p>
          </div>
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
            {statCard(
              <Cpu className="w-5 h-5 text-brand" />,
              'CPU Usage',
              formatPercent(cpuCurrent),
            )}
            {statCard(
              <HardDrive className="w-5 h-5 text-brand" />,
              'Memory',
              formatMemoryMB(memoryCurrentMB),
              `Utilization ${formatPercent(memoryCurrentPercent)}`,
            )}
            {statCard(
              <Network className="w-5 h-5 text-brand" />,
              'Network In',
              formatBytesPerSecond(networkInCurrentBPS),
            )}
            {statCard(
              <Network className="w-5 h-5 text-brand" />,
              'Network Out',
              formatBytesPerSecond(networkOutCurrentBPS),
            )}
          </div>

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
            {chartCard(
              'CPU Trend',
              (history?.series?.cpu_percent?.length ?? 0) > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={history?.series?.cpu_percent ?? []} margin={{ left: 8, right: 8, top: 8, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                    <XAxis dataKey="timestamp" tickFormatter={formatTimeTick} minTickGap={24} />
                    <YAxis tickFormatter={(v) => `${Number(v).toFixed(0)}%`} width={56} />
                    <Tooltip
                      labelFormatter={(v) => new Date(String(v)).toLocaleString()}
                      formatter={(value) => [formatPercent(parseNumeric(value)), 'CPU']}
                    />
                    <Line type="monotone" dataKey="value" name="CPU" stroke="#2563eb" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-sm text-content-tertiary">No CPU history yet</div>
              ),
              'Prometheus time-series',
            )}

            {chartCard(
              'Memory Trend',
              (history?.series?.memory_mb?.length ?? 0) > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={history?.series?.memory_mb ?? []} margin={{ left: 8, right: 8, top: 8, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                    <XAxis dataKey="timestamp" tickFormatter={formatTimeTick} minTickGap={24} />
                    <YAxis tickFormatter={(v) => formatMemoryMB(Number(v))} width={72} />
                    <Tooltip
                      labelFormatter={(v) => new Date(String(v)).toLocaleString()}
                      formatter={(value) => [formatMemoryMB(parseNumeric(value)), 'Memory']}
                    />
                    <Line type="monotone" dataKey="value" name="Memory" stroke="#16a34a" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-sm text-content-tertiary">No memory history yet</div>
              ),
              'Working set (MiB)',
            )}

            {chartCard(
              'Network Throughput',
              networkChartData.length > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={networkChartData} margin={{ left: 8, right: 8, top: 8, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                    <XAxis dataKey="timestamp" tickFormatter={formatTimeTick} minTickGap={24} />
                    <YAxis tickFormatter={(v) => formatBytesPerSecond(Number(v))} width={92} />
                    <Tooltip
                      labelFormatter={(v) => new Date(String(v)).toLocaleString()}
                      formatter={(value, key) => [formatBytesPerSecond(parseNumeric(value)), String(key) === 'network_in_bps' ? 'Inbound' : 'Outbound']}
                    />
                    <Legend />
                    <Line type="monotone" dataKey="network_in_bps" name="Inbound" stroke="#0891b2" strokeWidth={2} dot={false} />
                    <Line type="monotone" dataKey="network_out_bps" name="Outbound" stroke="#f97316" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-sm text-content-tertiary">No network history yet</div>
              ),
              'Bytes per second',
            )}

            <Card>
              <div className="text-xs text-content-tertiary mb-1">Container</div>
              <div className="text-sm font-mono text-content-primary break-all">{metrics?.container_id || '-'}</div>
              {history?.source && (
                <div className="mt-3 text-xs text-content-secondary">Metrics source: {history.source}</div>
              )}
              {history?.error && (
                <div className="mt-3 text-xs text-status-warning">{history.error}</div>
              )}
            </Card>
          </div>

          {(history?.end || metrics?.timestamp) && (
            <div className="mt-4 text-xs text-content-tertiary text-right">
              Last updated: {new Date(history?.end || metrics?.timestamp || '').toLocaleTimeString()}
            </div>
          )}
        </>
      )}
    </div>
  );
}
