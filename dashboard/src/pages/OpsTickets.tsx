import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { LifeBuoy, RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import type { OpsTicketItem, TicketCategory } from '../types';

type StatusFilter = 'all' | 'open' | 'pending' | 'solved' | 'closed';
type CategoryFilter = 'all' | 'support' | 'feature_request' | 'bug_report';

const categoryLabels: Record<TicketCategory, string> = {
  support: 'Support',
  feature_request: 'Feature Request',
  bug_report: 'Bug Report',
};

export function OpsTicketsPage() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<StatusFilter>('open');
  const [categoryFilter, setCategoryFilter] = useState<CategoryFilter>('all');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<OpsTicketItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .listTickets({ status: status === 'all' ? '' : status, category: categoryFilter === 'all' ? '' : categoryFilter, query: q, limit: 200 })
      .then(setRows)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load tickets');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, categoryFilter, q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <LifeBuoy className="w-5 h-5 text-content-tertiary" />
            Tickets
          </h1>
          <p className="text-sm text-content-secondary mt-1">Customer support queue.</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-border-default/60">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            {([
              { key: 'open', label: 'Open' },
              { key: 'pending', label: 'Pending' },
              { key: 'solved', label: 'Solved' },
              { key: 'closed', label: 'Closed' },
              { key: 'all', label: 'All' },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => setStatus(t.key)}
                className={cn(
                  'px-3 py-1.5 rounded-full border transition-colors',
                  status === t.key
                    ? 'bg-surface-tertiary text-content-primary border-border-default'
                    : 'bg-surface-secondary border-border-default text-content-secondary hover:bg-surface-tertiary'
                )}
              >
                {t.label}
              </button>
            ))}
            <div className="w-px h-5 bg-border-default mx-1" />
            {([
              { key: 'all', label: 'All Types' },
              { key: 'support', label: 'Support' },
              { key: 'feature_request', label: 'Feature Req' },
              { key: 'bug_report', label: 'Bug Report' },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => setCategoryFilter(t.key)}
                className={cn(
                  'px-3 py-1.5 rounded-full border transition-colors',
                  categoryFilter === t.key
                    ? 'bg-surface-tertiary text-content-primary border-border-default'
                    : 'bg-surface-secondary border-border-default text-content-secondary hover:bg-surface-tertiary'
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
              placeholder="Search subject, workspace, email..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="app-input w-full bg-surface-secondary border border-border-default rounded-md pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/12 transition-all"
            />
          </div>
        </div>

        {forbidden ? (
          <div className="p-8 text-center text-sm text-content-secondary">Forbidden.</div>
        ) : loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-md p-3">
                <Skeleton className="w-24 h-4" />
                <Skeleton className="w-72 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-20 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : rows.length === 0 ? (
          <div className="p-10 text-center text-sm text-content-tertiary">No tickets.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-4">Ticket</div>
              <div className="col-span-2">Customer</div>
              <div className="col-span-2">Category</div>
              <div className="col-span-2">Status</div>
              <div className="col-span-2 text-right">Updated</div>
            </div>
            {rows.map((t) => (
              <button
                key={t.id}
                onClick={() => navigate(`/ops/tickets/${encodeURIComponent(t.id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-4 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{t.subject}</div>
                  <div className="text-xs text-content-tertiary truncate">{t.workspace_name || truncate(t.workspace_id, 10) || '—'} · {truncate(t.id, 10)}</div>
                </div>
                <div className="col-span-2 min-w-0">
                  <div className="text-sm text-content-secondary truncate">{t.created_by_email || t.created_by_username || truncate(t.created_by, 10)}</div>
                  <div className="text-xs text-content-tertiary truncate">priority: {t.priority || 'normal'}</div>
                </div>
                <div className="col-span-2 text-sm">
                  <span className={cn('text-xs px-2 py-0.5 rounded-full border inline-flex',
                    t.category === 'feature_request' ? 'border-purple-400/30 bg-purple-500/10 text-purple-300' :
                    t.category === 'bug_report' ? 'border-red-400/30 bg-red-500/10 text-red-300' :
                    'border-border-default bg-surface-secondary text-content-secondary'
                  )}>
                    {categoryLabels[t.category as TicketCategory] || t.category || 'Support'}
                  </span>
                </div>
                <div className="col-span-2 text-sm text-content-secondary">{t.status}</div>
                <div className="col-span-2 text-right text-xs text-content-tertiary">
                  {new Date(t.updated_at).toLocaleString()}
                </div>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

