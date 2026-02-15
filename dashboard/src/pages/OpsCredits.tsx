import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Coins, RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { truncate } from '../lib/utils';
import type { OpsWorkspaceCreditItem } from '../types';

export function OpsCreditsPage() {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<OpsWorkspaceCreditItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .listCreditsWorkspaces({ query: q, limit: 200 })
      .then(setRows)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load credits');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <Coins className="w-5 h-5 text-content-tertiary" />
            Credits
          </h1>
          <p className="text-sm text-content-secondary mt-1">Workspace credit balances (ledger-backed).</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-border-default/60">
          <div className="relative w-full md:w-[420px]">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
            <input
              type="text"
              placeholder="Search workspace or owner email..."
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
                <Skeleton className="w-64 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-28 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : rows.length === 0 ? (
          <div className="p-10 text-center text-sm text-content-tertiary">No workspaces.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-6">Workspace</div>
              <div className="col-span-4">Owner</div>
              <div className="col-span-2 text-right">Balance</div>
            </div>
            {rows.map((ws) => (
              <button
                key={ws.workspace_id}
                onClick={() => navigate(`/ops/credits/${encodeURIComponent(ws.workspace_id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-6 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{ws.workspace_name || truncate(ws.workspace_id, 10)}</div>
                  <div className="text-xs text-content-tertiary font-mono">{truncate(ws.workspace_id, 14)}</div>
                </div>
                <div className="col-span-4 text-sm text-content-secondary truncate">{ws.owner_email || '—'}</div>
                <div className="col-span-2 text-right text-sm text-content-secondary tabular-nums">
                  ${(ws.balance_cents / 100).toFixed(2)}
                </div>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

