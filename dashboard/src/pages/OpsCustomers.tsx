import { useEffect, useMemo, useState } from 'react';
import { Search, Users } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import type { OpsUserItem, OpsWorkspaceItem } from '../types';

export function OpsCustomersPage() {
  const [query, setQuery] = useState('');
  const [users, setUsers] = useState<OpsUserItem[]>([]);
  const [workspaces, setWorkspaces] = useState<OpsWorkspaceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setForbidden(false);
    setError(null);

    const t = window.setTimeout(() => {
      Promise.all([
        ops.listUsers({ query: q, limit: 50 }),
        ops.listWorkspaces({ query: q, limit: 50 }),
      ])
        .then(([u, w]) => {
          if (!alive) return;
          setUsers(u);
          setWorkspaces(w);
        })
        .catch((e) => {
          if (!alive) return;
          if (e instanceof ApiError && e.status === 403) {
            setForbidden(true);
            return;
          }
          setError(e?.message || 'Failed to load customers');
        })
        .finally(() => {
          if (!alive) return;
          setLoading(false);
        });
    }, 250);

    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-3xl font-semibold text-content-primary mt-1">Customers</h1>
          <p className="text-sm text-content-secondary mt-1">Search users and workspaces across the platform.</p>
        </div>

        <div className="relative w-full sm:w-[420px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search by email, username, workspace..."
            className="app-input w-full bg-surface-secondary border border-border-default rounded-lg pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/12 transition-all shadow-[0_8px_20px_rgba(15,23,42,0.05)]"
          />
        </div>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">
          Your account doesn’t have access to ops customer data.
        </Card>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 flex items-center justify-between">
              <div className="text-sm font-semibold text-content-primary flex items-center gap-2">
                <Users className="w-4 h-4 text-brand" />
                Users
              </div>
              <div className="text-xs text-content-tertiary">{loading ? '...' : `${users.length}`}</div>
            </div>
            <div className="p-4 space-y-2">
              {loading ? (
                Array.from({ length: 6 }).map((_, i) => (
                  <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                    <Skeleton className="w-8 h-8 rounded-full" />
                    <div className="flex-1 space-y-2">
                      <Skeleton className="w-40 h-4" />
                      <Skeleton className="w-64 h-3" />
                    </div>
                    <Skeleton className="w-16 h-4" />
                  </div>
                ))
              ) : users.length === 0 ? (
                <div className="text-sm text-content-tertiary text-center py-10">No users found.</div>
              ) : (
                users.map((u) => (
                  <div key={u.id} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                    <div className="w-8 h-8 rounded-full bg-brand/10 flex items-center justify-center text-xs font-semibold text-brand">
                      {(u.username || u.email || 'U').slice(0, 1).toUpperCase()}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-semibold text-content-primary truncate">{u.username || 'User'}</div>
                      <div className="text-xs text-content-tertiary truncate">{u.email || '(no email)'} · {u.role}</div>
                    </div>
                    <div className="text-[11px] px-2 py-1 rounded-full border border-border-default bg-surface-tertiary text-content-tertiary">
                      {u.id.slice(0, 8)}
                    </div>
                  </div>
                ))
              )}
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 flex items-center justify-between">
              <div className="text-sm font-semibold text-content-primary">Workspaces</div>
              <div className="text-xs text-content-tertiary">{loading ? '...' : `${workspaces.length}`}</div>
            </div>
            <div className="p-4 space-y-2">
              {loading ? (
                Array.from({ length: 6 }).map((_, i) => (
                  <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-xl p-3">
                    <Skeleton className="w-10 h-4" />
                    <Skeleton className="w-60 h-4" />
                  </div>
                ))
              ) : workspaces.length === 0 ? (
                <div className="text-sm text-content-tertiary text-center py-10">No workspaces found.</div>
              ) : (
                workspaces.map((ws) => (
                  <div key={ws.id} className="flex items-center justify-between gap-3 border border-border-subtle rounded-xl p-3">
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-content-primary truncate">{ws.name || 'Workspace'}</div>
                      <div className="text-xs text-content-tertiary truncate">Owner: {ws.owner_email || ws.owner_id.slice(0, 8)}</div>
                    </div>
                    <div className="text-[11px] px-2 py-1 rounded-full border border-border-default bg-surface-tertiary text-content-tertiary shrink-0">
                      {ws.id.slice(0, 8)}
                    </div>
                  </div>
                ))
              )}
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}

