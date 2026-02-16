import { useEffect, useMemo, useState } from 'react';
import { RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn, formatTime } from '../lib/utils';
import type { OpsAuditLogEntry } from '../types';

export function OpsAuditLogsPage() {
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<OpsAuditLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .listAuditLogs({ query: q, limit: 200 })
      .then(setRows)
      .catch((e: unknown) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e instanceof Error ? e.message : 'Failed to load audit log');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  const actionTone = (action: string) => {
    const a = (action || '').toLowerCase();
    if (a.includes('suspend') || a.includes('delete') || a.includes('role')) return 'text-status-warning';
    if (a.includes('failed') || a.includes('error')) return 'text-status-error';
    return 'text-content-primary';
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-3xl font-semibold text-content-primary mt-1">Audit Log</h1>
          <p className="text-sm text-content-secondary mt-1">Who did what, across the admin surface.</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-border-default/60">
          <div className="text-sm font-semibold text-content-primary">Events</div>
          <div className="relative w-full md:w-[420px]">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
            <input
              type="text"
              placeholder="Search action, email, workspace, resource..."
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
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                <Skeleton className="w-28 h-4" />
                <Skeleton className="w-40 h-4" />
                <Skeleton className="w-56 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-24 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : rows.length === 0 ? (
          <div className="p-10 text-center text-sm text-content-tertiary">No audit events.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-2">Time</div>
              <div className="col-span-3">Actor</div>
              <div className="col-span-3">Workspace</div>
              <div className="col-span-4">Action</div>
            </div>

            {rows.map((e) => (
              <div key={e.id} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle items-center gap-3">
                <div className="col-span-2">
                  <div className="text-xs text-content-tertiary font-mono">{formatTime(e.created_at)}</div>
                </div>
                <div className="col-span-3 min-w-0">
                  <div className="text-sm text-content-secondary truncate">{e.actor_email || e.user_id?.slice(0, 8) || '-'}</div>
                </div>
                <div className="col-span-3 min-w-0">
                  <div className="text-sm text-content-secondary truncate">{e.workspace_name || e.workspace_id?.slice(0, 8) || '-'}</div>
                </div>
                <div className="col-span-4 min-w-0">
                  <div className={cn('text-sm font-semibold truncate', actionTone(e.action))}>
                    {e.action}
                  </div>
                  <div className="text-xs text-content-tertiary truncate">
                    {e.resource_type}{e.resource_id ? ` · ${String(e.resource_id).slice(0, 8)}` : ''}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

