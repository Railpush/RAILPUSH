import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ChevronRight, RefreshCcw, Search, Siren } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { EmptyState } from '../components/ui/EmptyState';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { IncidentStatusBadge, SeverityBadge } from '../components/ui/IncidentBadge';
import { cn, timeAgo, truncate } from '../lib/utils';
import { ApiError, ops } from '../lib/api';
import type { Incident } from '../types';

type StateFilter = 'active' | 'resolved' | 'all';

export function Incidents() {
  const navigate = useNavigate();
  const [state, setState] = useState<StateFilter>('active');
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [forbidden, setForbidden] = useState(false);

  const load = () => {
    setLoading(true);
    setError(null);
    setForbidden(false);
    ops
      .listIncidents({ state, limit: 200 })
      .then((rows) => setIncidents(rows))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load incidents');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state]);

  // Keep "Active" reasonably fresh without needing a manual refresh.
  useEffect(() => {
    if (state !== 'active') return;
    const t = window.setInterval(() => load(), 30000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return incidents;
    return incidents.filter((i) => {
      return (
        i.alertname.toLowerCase().includes(q) ||
        (i.summary || '').toLowerCase().includes(q) ||
        (i.namespace || '').toLowerCase().includes(q) ||
        (i.severity || '').toLowerCase().includes(q)
      );
    });
  }, [incidents, search]);

  const counts = useMemo(() => {
    const active = incidents.filter((i) => (i.status || '').toLowerCase() === 'firing').length;
    const resolved = incidents.filter((i) => (i.status || '').toLowerCase() === 'resolved').length;
    return { active, resolved, all: incidents.length };
  }, [incidents]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-3xl font-semibold text-content-primary mt-1">Incidents</h1>
          <p className="text-sm text-content-secondary mt-1">Alertmanager incidents captured from your cluster.</p>
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
              { key: 'active', label: `Active (${counts.active})` },
              { key: 'resolved', label: `Resolved (${counts.resolved})` },
              { key: 'all', label: `All (${counts.all})` },
            ] as const).map((t) => (
              <button
                key={t.key}
                onClick={() => setState(t.key)}
                className={cn(
                  'px-3 py-1.5 rounded-full border transition-colors',
                  state === t.key
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
              placeholder="Search incidents..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="app-input w-full bg-surface-secondary border border-border-default rounded-lg pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/12 transition-all shadow-[0_8px_20px_rgba(15,23,42,0.05)]"
            />
          </div>
        </div>

        {forbidden ? (
          <div className="p-8 text-center text-sm text-content-secondary">
            Your account doesn’t have access to ops incidents.
            <div className="text-xs text-content-tertiary mt-2">
              Ask an admin to set your user role to <code className="font-mono">admin</code>.
            </div>
          </div>
        ) : loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                <Skeleton className="w-24 h-5 rounded-full" />
                <Skeleton className="w-24 h-5 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="w-48 h-4" />
                  <Skeleton className="w-80 h-3" />
                </div>
                <Skeleton className="w-16 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={<Siren className="w-6 h-6" />}
            title="No incidents"
            description={search ? 'No incidents match your search.' : 'You’re all clear. No incidents in this view.'}
          />
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-3">Status</div>
              <div className="col-span-3">Alert</div>
              <div className="col-span-4">Summary</div>
              <div className="col-span-2 text-right">Last Seen</div>
            </div>

            {filtered.map((inc) => (
              <button
                key={inc.id}
                onClick={() => navigate(`/incidents/${encodeURIComponent(inc.id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-3 flex items-center gap-2">
                  <IncidentStatusBadge status={inc.status} size="sm" />
                  <SeverityBadge severity={inc.severity} size="sm" />
                  {inc.acknowledged_at && (
                    <span className="text-[11px] px-2 py-0.5 rounded-full border border-border-default bg-surface-tertiary text-content-tertiary">
                      Ack
                    </span>
                  )}
                  {inc.silenced_until && (
                    <span className="text-[11px] px-2 py-0.5 rounded-full border border-status-warning/30 bg-status-warning/10 text-status-warning">
                      Silenced
                    </span>
                  )}
                </div>
                <div className="col-span-3 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{inc.alertname || 'Alert'}</div>
                  <div className="text-xs text-content-tertiary truncate">{inc.namespace || 'default'}</div>
                </div>
                <div className="col-span-4 min-w-0">
                  <div className="text-sm text-content-secondary truncate">{truncate(inc.summary || inc.description || '', 120) || 'No summary'}</div>
                  <div className="text-xs text-content-tertiary mt-0.5">
                    {inc.alerts_count ? `${inc.alerts_count} alert(s)` : '0 alerts'} · {inc.event_count} event(s)
                  </div>
                </div>
                <div className="col-span-2 flex items-center justify-end gap-2 text-xs text-content-tertiary">
                  <span>{timeAgo(inc.last_seen_at || inc.latest_received_at)}</span>
                  <ChevronRight className="w-4 h-4 opacity-60" />
                </div>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
