import { useEffect, useMemo, useState } from 'react';
import { RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import type { OpsDeployItem } from '../types';

type StatusFilter = 'all' | 'pending' | 'building' | 'deploying' | 'live' | 'failed';

export function OpsDeploymentsPage() {
  const [status, setStatus] = useState<StatusFilter>('all');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<OpsDeployItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .listDeploys({ status: status === 'all' ? '' : status, query: q, limit: 200 })
      .then(setRows)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load deploys');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-3xl font-semibold text-content-primary mt-1">Deployments</h1>
          <p className="text-sm text-content-secondary mt-1">Platform-wide deploy stream.</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-border-default/60">
          <div className="flex items-center gap-2 text-sm">
            {([
              { key: 'all', label: 'All' },
              { key: 'pending', label: 'Pending' },
              { key: 'building', label: 'Building' },
              { key: 'deploying', label: 'Deploying' },
              { key: 'live', label: 'Live' },
              { key: 'failed', label: 'Failed' },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => setStatus(t.key)}
                className={cn(
                  'px-3 py-1.5 rounded-full border transition-colors',
                  status === t.key
                    ? 'bg-brand text-white border-brand shadow-[0_6px_16px_rgba(37,99,235,0.35)]'
                    : 'bg-surface-secondary border-brand/20 text-brand hover:text-brand-hover hover:bg-surface-tertiary/50'
                )}
              >
                {t.label}
              </button>
            ))}
          </div>

          <div className="relative w-full md:w-[360px]">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
            <input
              type="text"
              placeholder="Search service or commit message..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="app-input w-full bg-surface-secondary border border-border-default rounded-lg pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/12 transition-all shadow-[0_8px_20px_rgba(15,23,42,0.05)]"
            />
          </div>
        </div>

        {forbidden ? (
          <div className="p-8 text-center text-sm text-content-secondary">Forbidden.</div>
        ) : loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                <Skeleton className="w-20 h-5 rounded-full" />
                <Skeleton className="w-48 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-24 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : rows.length === 0 ? (
          <div className="p-10 text-center text-sm text-content-tertiary">No deploys.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-2">Status</div>
              <div className="col-span-4">Service</div>
              <div className="col-span-4">Commit</div>
              <div className="col-span-2 text-right">ID</div>
            </div>

            {rows.map((d) => (
              <div key={d.id} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                <div className="col-span-2">
                  <span className={cn(
                    'text-[11px] px-2 py-1 rounded-full border inline-flex',
                    d.status === 'failed'
                      ? 'border-status-error/30 bg-status-error/10 text-status-error'
                      : d.status === 'live'
                        ? 'border-status-success/30 bg-status-success/10 text-status-success'
                        : 'border-border-default bg-surface-tertiary text-content-tertiary'
                  )}>
                    {d.status}
                  </span>
                </div>
                <div className="col-span-4 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{d.service_name || 'Service'}</div>
                  <div className="text-xs text-content-tertiary truncate">{d.branch || 'branch?'} · {d.trigger}</div>
                </div>
                <div className="col-span-4 min-w-0">
                  <div className="text-sm text-content-secondary truncate">
                    {d.commit_sha ? d.commit_sha.slice(0, 7) : '...'} {truncate(d.commit_message || '', 90)}
                  </div>
                  {d.last_error && (
                    <div className="text-xs text-status-error mt-0.5 truncate">{truncate(d.last_error, 120)}</div>
                  )}
                </div>
                <div className="col-span-2 text-right text-xs text-content-tertiary font-mono">
                  {d.id.slice(0, 8)}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

