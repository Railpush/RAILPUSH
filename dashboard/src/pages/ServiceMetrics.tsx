import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Activity, Cpu, HardDrive, Network, RefreshCw } from 'lucide-react';
import { Card } from '../components/ui/Card';

const BASE = '/api/v1';

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
  error?: string;
}

export function ServiceMetrics() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [metrics, setMetrics] = useState<MetricsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);

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
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchMetrics();
  }, [serviceId]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(fetchMetrics, 5000);
    return () => clearInterval(interval);
  }, [serviceId, autoRefresh]);

  const statCard = (icon: React.ReactNode, label: string, value: string, sub?: string) => (
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

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Metrics</h1>
        <div className="flex items-center gap-3">
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
            onClick={fetchMetrics}
            className="p-2 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {!metrics || metrics.status !== 'live' ? (
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
              metrics.cpu_percent || '0%',
            )}
            {statCard(
              <HardDrive className="w-5 h-5 text-brand" />,
              'Memory',
              metrics.memory_used || '0',
              metrics.memory_total ? `of ${metrics.memory_total} (${metrics.memory_percent}%)` : undefined,
            )}
            {statCard(
              <Network className="w-5 h-5 text-brand" />,
              'Network In',
              metrics.network_in || '0B',
            )}
            {statCard(
              <Network className="w-5 h-5 text-brand" />,
              'Network Out',
              metrics.network_out || '0B',
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <Card>
              <div className="text-xs text-content-tertiary mb-1">Processes</div>
              <div className="text-lg font-semibold text-content-primary">{metrics.pids || '0'}</div>
            </Card>
            <Card>
              <div className="text-xs text-content-tertiary mb-1">Container</div>
              <div className="text-lg font-mono text-content-primary">{metrics.container_id || '-'}</div>
            </Card>
          </div>

          {metrics.timestamp && (
            <div className="mt-4 text-xs text-content-tertiary text-right">
              Last updated: {new Date(metrics.timestamp).toLocaleTimeString()}
            </div>
          )}
        </>
      )}
    </div>
  );
}
