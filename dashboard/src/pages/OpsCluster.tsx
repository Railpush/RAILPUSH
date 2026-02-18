import { useEffect, useState } from 'react';
import { Network, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn } from '../lib/utils';
import type { OpsClusterSummary } from '../types';

function fmtAge(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.min((value / max) * 100, 100) : 0;
  return (
    <div className="w-full h-2 rounded-full bg-surface-secondary overflow-hidden">
      <div className={cn('h-full rounded-full', color)} style={{ width: `${pct}%` }} />
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const s = status.toLowerCase();
  const cls =
    s === 'ready' || s === 'running' || s === 'bound'
      ? 'border-status-success/30 bg-status-success/10 text-status-success'
      : s === 'pending'
        ? 'border-status-warning/30 bg-status-warning/10 text-status-warning'
        : 'border-status-error/30 bg-status-error/10 text-status-error';
  return (
    <span className={cn('inline-flex px-2 py-0.5 rounded-full border text-xs', cls)}>
      {status}
    </span>
  );
}

function StatCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <Card className="p-4">
      <p className="text-xs uppercase tracking-[0.12em] text-content-tertiary">{label}</p>
      <p className="text-2xl font-semibold text-content-primary mt-1 tabular-nums">{value}</p>
      {sub && <p className="text-xs text-content-tertiary mt-0.5">{sub}</p>}
    </Card>
  );
}

export function OpsClusterPage() {
  const [data, setData] = useState<OpsClusterSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getClusterSummary()
      .then(setData)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load cluster summary');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const t = window.setInterval(() => load(), 30000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const totals = data?.cluster_totals;
  const phases = data?.pod_phases;
  const phaseTotal = phases ? phases.running + phases.pending + phases.succeeded + phases.failed + phases.unknown : 0;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <Network className="w-5 h-5 text-content-tertiary" />
            Cluster
          </h1>
          <p className="text-sm text-content-secondary mt-1">At-a-glance Kubernetes cluster health and resources.</p>
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
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {[1, 2, 3, 4].map((i) => (
            <Card key={i} className="p-5">
              <Skeleton className="w-24 h-3" />
              <Skeleton className="w-16 h-7 mt-2" />
            </Card>
          ))}
        </div>
      ) : !data.enabled ? (
        <Card className="p-8 text-center text-sm text-content-secondary">
          Cluster summary is not available.
          <div className="text-xs text-content-tertiary mt-2">
            {data.error ? data.error : 'KUBERNETES_ENABLED=false'}
          </div>
        </Card>
      ) : (
        <>
          {/* Stat cards */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <StatCard label="Nodes" value={totals?.nodes ?? 0} />
            <StatCard label="Running Pods" value={totals?.running_pods ?? 0} sub={`${totals?.pods ?? 0} total`} />
            <StatCard label="Deployments" value={`${totals?.deployments_ready ?? 0}/${totals?.deployments ?? 0}`} sub="ready" />
            <StatCard label="StatefulSets" value={`${totals?.statefulsets_ready ?? 0}/${totals?.statefulsets ?? 0}`} sub="ready" />
          </div>

          {/* Nodes */}
          {(data.nodes ?? []).length > 0 && (
            <div>
              <h2 className="text-sm font-semibold text-content-primary mb-3">Nodes</h2>
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                {(data.nodes ?? []).map((n) => (
                  <Card key={n.name} className="p-4 space-y-3">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-medium text-content-primary truncate">{n.name}</span>
                      <StatusBadge status={n.status} />
                    </div>
                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-content-tertiary">
                      <span>{n.roles}</span>
                      <span>{n.kubelet_version}</span>
                      <span>{n.os}/{n.arch}</span>
                      <span>{fmtAge(n.age_seconds)} old</span>
                    </div>

                    <div className="space-y-2">
                      <div>
                        <div className="flex justify-between text-xs text-content-secondary mb-1">
                          <span>CPU</span>
                          <span className="tabular-nums">{(n.cpu_allocatable / 1000).toFixed(1)} / {(n.cpu_capacity / 1000).toFixed(1)} cores</span>
                        </div>
                        <Bar value={n.cpu_capacity - n.cpu_allocatable} max={n.cpu_capacity} color="bg-blue-500" />
                      </div>
                      <div>
                        <div className="flex justify-between text-xs text-content-secondary mb-1">
                          <span>Memory</span>
                          <span className="tabular-nums">{(n.mem_allocatable_mi / 1024).toFixed(1)} / {(n.mem_capacity_mi / 1024).toFixed(1)} Gi</span>
                        </div>
                        <Bar value={n.mem_capacity_mi - n.mem_allocatable_mi} max={n.mem_capacity_mi} color="bg-purple-500" />
                      </div>
                      <div>
                        <div className="flex justify-between text-xs text-content-secondary mb-1">
                          <span>Pods</span>
                          <span className="tabular-nums">{n.pod_count} / {n.pod_capacity}</span>
                        </div>
                        <Bar value={n.pod_count} max={n.pod_capacity} color="bg-emerald-500" />
                      </div>
                    </div>
                  </Card>
                ))}
              </div>
            </div>
          )}

          {/* Pod Phase Breakdown */}
          {phaseTotal > 0 && (
            <div>
              <h2 className="text-sm font-semibold text-content-primary mb-3">Pod Phases</h2>
              <Card className="p-4 space-y-3">
                <div className="w-full h-4 rounded-full overflow-hidden flex">
                  {phases!.running > 0 && (
                    <div className="bg-emerald-500 h-full" style={{ width: `${(phases!.running / phaseTotal) * 100}%` }} />
                  )}
                  {phases!.succeeded > 0 && (
                    <div className="bg-blue-400 h-full" style={{ width: `${(phases!.succeeded / phaseTotal) * 100}%` }} />
                  )}
                  {phases!.pending > 0 && (
                    <div className="bg-amber-400 h-full" style={{ width: `${(phases!.pending / phaseTotal) * 100}%` }} />
                  )}
                  {phases!.failed > 0 && (
                    <div className="bg-red-500 h-full" style={{ width: `${(phases!.failed / phaseTotal) * 100}%` }} />
                  )}
                  {phases!.unknown > 0 && (
                    <div className="bg-gray-400 h-full" style={{ width: `${(phases!.unknown / phaseTotal) * 100}%` }} />
                  )}
                </div>
                <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs">
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-emerald-500" />Running {phases!.running}</span>
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-blue-400" />Succeeded {phases!.succeeded}</span>
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-amber-400" />Pending {phases!.pending}</span>
                  <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-red-500" />Failed {phases!.failed}</span>
                  {phases!.unknown > 0 && (
                    <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-gray-400" />Unknown {phases!.unknown}</span>
                  )}
                </div>
              </Card>
            </div>
          )}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {/* Namespace Distribution */}
            {(data.namespaces ?? []).length > 0 && (
              <div>
                <h2 className="text-sm font-semibold text-content-primary mb-3">Namespaces</h2>
                <Card className="p-0 overflow-hidden">
                  <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                    <div className="col-span-8">Namespace</div>
                    <div className="col-span-4 text-right">Pods</div>
                  </div>
                  {[...(data.namespaces ?? [])].sort((a, b) => b.pod_count - a.pod_count).map((ns) => (
                    <div key={ns.name} className="grid grid-cols-12 px-4 py-2.5 border-b border-border-subtle">
                      <div className="col-span-8 text-sm text-content-secondary truncate">{ns.name}</div>
                      <div className="col-span-4 text-right text-sm text-content-secondary tabular-nums">{ns.pod_count}</div>
                    </div>
                  ))}
                </Card>
              </div>
            )}

            {/* PVCs */}
            {(data.pvcs ?? []).length > 0 && (
              <div>
                <h2 className="text-sm font-semibold text-content-primary mb-3">Persistent Volume Claims</h2>
                <Card className="p-0 overflow-hidden">
                  <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                    <div className="col-span-5">Name</div>
                    <div className="col-span-2">Status</div>
                    <div className="col-span-2">Capacity</div>
                    <div className="col-span-3 text-right">Storage Class</div>
                  </div>
                  {(data.pvcs ?? []).map((p) => (
                    <div key={`${p.namespace}/${p.name}`} className="grid grid-cols-12 px-4 py-2.5 border-b border-border-subtle">
                      <div className="col-span-5 text-sm text-content-secondary truncate">{p.name}</div>
                      <div className="col-span-2"><StatusBadge status={p.status} /></div>
                      <div className="col-span-2 text-sm text-content-secondary">{p.capacity || '—'}</div>
                      <div className="col-span-3 text-right text-xs text-content-tertiary truncate">{p.storage_class || '—'}</div>
                    </div>
                  ))}
                </Card>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
