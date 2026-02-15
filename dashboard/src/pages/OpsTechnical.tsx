import { useEffect, useState } from 'react';
import { Activity, RefreshCcw, Server } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn } from '../lib/utils';
import type { OpsKubeSummary } from '../types';

function fmtAge(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

export function OpsTechnicalPage() {
  const [data, setData] = useState<OpsKubeSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getKubeSummary()
      .then(setData)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load technical summary');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const t = window.setInterval(() => load(), 30000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <Server className="w-5 h-5 text-content-tertiary" />
            Technical
          </h1>
          <p className="text-sm text-content-secondary mt-1">Namespace inventory from the in-cluster Kubernetes client.</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">Forbidden.</Card>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : loading || !data ? (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card className="p-5">
            <Skeleton className="w-40 h-4" />
            <Skeleton className="w-full h-24 mt-3" />
          </Card>
          <Card className="p-5">
            <Skeleton className="w-40 h-4" />
            <Skeleton className="w-full h-24 mt-3" />
          </Card>
        </div>
      ) : !data.enabled ? (
        <Card className="p-8 text-center text-sm text-content-secondary">
          Kubernetes summary is not available.
          <div className="text-xs text-content-tertiary mt-2">
            {data.error ? data.error : 'KUBERNETES_ENABLED=false'}
          </div>
        </Card>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary flex items-center gap-2">
              <Activity className="w-4 h-4 text-content-tertiary" />
              Deployments
              <span className="ml-auto text-xs text-content-tertiary">{data.namespace}</span>
            </div>
            <div className="overflow-hidden">
              <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                <div className="col-span-6">Name</div>
                <div className="col-span-4">Ready</div>
                <div className="col-span-2 text-right">Age</div>
              </div>
              {(data.deployments || []).map((d) => (
                <div key={d.name} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                  <div className="col-span-6 text-sm text-content-secondary truncate">{d.name}</div>
                  <div className="col-span-4 text-sm text-content-secondary tabular-nums">
                    <span
                      className={cn(
                        'inline-flex px-2 py-0.5 rounded-full border text-xs',
                        d.ready_replicas >= d.desired_replicas
                          ? 'border-status-success/30 bg-status-success/10 text-status-success'
                          : 'border-status-warning/30 bg-status-warning/10 text-status-warning'
                      )}
                    >
                      {d.ready_replicas}/{d.desired_replicas}
                    </span>
                  </div>
                  <div className="col-span-2 text-right text-xs text-content-tertiary">{fmtAge(d.age_seconds)}</div>
                </div>
              ))}
              {(data.deployments || []).length === 0 && (
                <div className="p-8 text-center text-sm text-content-tertiary">No deployments.</div>
              )}
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">Pods</div>
            <div className="overflow-hidden">
              <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                <div className="col-span-6">Name</div>
                <div className="col-span-2">Ready</div>
                <div className="col-span-2">Restarts</div>
                <div className="col-span-2 text-right">Age</div>
              </div>
              {(data.pods || []).map((p) => (
                <div key={p.name} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                  <div className="col-span-6 text-sm text-content-secondary truncate">{p.name}</div>
                  <div className="col-span-2 text-sm">
                    <span
                      className={cn(
                        'text-xs px-2 py-0.5 rounded-full border inline-flex',
                        p.ready ? 'border-status-success/30 bg-status-success/10 text-status-success' : 'border-status-warning/30 bg-status-warning/10 text-status-warning'
                      )}
                    >
                      {p.ready ? 'ready' : p.phase}
                    </span>
                  </div>
                  <div className="col-span-2 text-sm text-content-secondary tabular-nums">{p.restarts}</div>
                  <div className="col-span-2 text-right text-xs text-content-tertiary">{fmtAge(p.age_seconds)}</div>
                </div>
              ))}
              {(data.pods || []).length === 0 && <div className="p-8 text-center text-sm text-content-tertiary">No pods.</div>}
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}

