import { useEffect, useState } from 'react';
import { BarChart3, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import type { OpsPerformanceSummary } from '../types';

function fmtSeconds(v?: number) {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—';
  if (v < 60) return `${v.toFixed(1)}s`;
  if (v < 3600) return `${(v / 60).toFixed(1)}m`;
  return `${(v / 3600).toFixed(2)}h`;
}

export function OpsPerformancePage() {
  const [windowHours, setWindowHours] = useState(24);
  const [data, setData] = useState<OpsPerformanceSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getPerformanceSummary({ window_hours: windowHours })
      .then(setData)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load performance');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowHours]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <BarChart3 className="w-5 h-5 text-content-tertiary" />
            Performance
          </h1>
          <p className="text-sm text-content-secondary mt-1">Deploy throughput and latency from the control-plane DB.</p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={windowHours}
            onChange={(e) => setWindowHours(parseInt(e.target.value, 10) || 24)}
            className="h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
          >
            <option value={1}>1h</option>
            <option value={6}>6h</option>
            <option value={24}>24h</option>
            <option value={72}>72h</option>
            <option value={168}>7d</option>
          </select>
          <Button variant="secondary" onClick={load} loading={loading}>
            <RefreshCcw className="w-4 h-4" />
            Refresh
          </Button>
        </div>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">Forbidden.</Card>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : loading || !data ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Card className="p-6">
            <Skeleton className="w-40 h-4" />
            <Skeleton className="w-full h-20 mt-3" />
          </Card>
          <Card className="p-6">
            <Skeleton className="w-40 h-4" />
            <Skeleton className="w-full h-20 mt-3" />
          </Card>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Card className="p-5">
              <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Deploys</div>
              <div className="text-2xl font-semibold text-content-primary mt-2">{data.deploys.total}</div>
              <div className="text-xs text-content-tertiary mt-2">
                live {data.deploys.live} · failed {data.deploys.failed} · pending {data.deploys.pending}
              </div>
            </Card>
            <Card className="p-5">
              <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Queue Wait</div>
              <div className="text-sm text-content-secondary mt-2">p50 {fmtSeconds(data.queue_wait_seconds.p50)}</div>
              <div className="text-sm text-content-secondary mt-1">p95 {fmtSeconds(data.queue_wait_seconds.p95)}</div>
              <div className="text-xs text-content-tertiary mt-2">avg {fmtSeconds(data.queue_wait_seconds.avg)}</div>
            </Card>
            <Card className="p-5">
              <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Deploy Duration</div>
              <div className="text-sm text-content-secondary mt-2">p50 {fmtSeconds(data.deploy_duration_seconds.p50)}</div>
              <div className="text-sm text-content-secondary mt-1">p95 {fmtSeconds(data.deploy_duration_seconds.p95)}</div>
              <div className="text-xs text-content-tertiary mt-2">avg {fmtSeconds(data.deploy_duration_seconds.avg)}</div>
            </Card>
          </div>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">Top Failures</div>
            {data.top_failures.length === 0 ? (
              <div className="p-8 text-center text-sm text-content-tertiary">No failures in window.</div>
            ) : (
              <div className="overflow-hidden">
                <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                  <div className="col-span-7">Service</div>
                  <div className="col-span-3">ID</div>
                  <div className="col-span-2 text-right">Failures</div>
                </div>
                {data.top_failures.map((f) => (
                  <div key={f.service_id} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                    <div className="col-span-7 text-sm text-content-secondary truncate">{f.service_name || 'Service'}</div>
                    <div className="col-span-3 text-xs text-content-tertiary font-mono truncate">{f.service_id}</div>
                    <div className="col-span-2 text-right text-sm text-content-secondary tabular-nums">{f.failures}</div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

