import { useEffect, useMemo, useState } from 'react';
import { Search, Users } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn } from '../lib/utils';
import { toast } from 'sonner';
import type { OpsUserItem, OpsWorkspaceItem } from '../types';

export function OpsCustomersPage() {
  const [query, setQuery] = useState('');
  const [users, setUsers] = useState<OpsUserItem[]>([]);
  const [workspaces, setWorkspaces] = useState<OpsWorkspaceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [roleModal, setRoleModal] = useState<{ open: boolean; userId: string; email: string; role: string }>({ open: false, userId: '', email: '', role: 'member' });

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
                      <div className="text-xs text-content-tertiary truncate">
                        {u.email || '(no email)'} · {u.role}
                        {u.is_suspended ? <span className="ml-2 text-status-warning">suspended</span> : null}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={busyKey === `role:${u.id}`}
                        loading={busyKey === `role:${u.id}`}
                        onClick={() => setRoleModal({ open: true, userId: u.id, email: u.email || '', role: (u.role || 'member').toLowerCase() })}
                      >
                        Role
                      </Button>
                      <Button
                        size="sm"
                        variant={u.is_suspended ? 'secondary' : 'danger'}
                        className={cn(u.is_suspended ? '' : 'bg-status-error hover:bg-red-600 text-white border-0')}
                        disabled={busyKey === `suspend-user:${u.id}`}
                        loading={busyKey === `suspend-user:${u.id}`}
                        onClick={async () => {
                          setBusyKey(`suspend-user:${u.id}`);
                          try {
                            if (u.is_suspended) {
                              await ops.resumeUser(u.id);
                              setUsers((prev) => prev.map((x) => (x.id === u.id ? { ...x, is_suspended: false } : x)));
                              toast.success('User resumed');
                            } else {
                              await ops.suspendUser(u.id);
                              setUsers((prev) => prev.map((x) => (x.id === u.id ? { ...x, is_suspended: true } : x)));
                              toast.success('User suspended');
                            }
                          } catch (e: unknown) {
                            toast.error(e instanceof Error ? e.message : 'Failed');
                          } finally {
                            setBusyKey(null);
                          }
                        }}
                      >
                        {u.is_suspended ? 'Resume' : 'Suspend'}
                      </Button>
                      <div className="text-[11px] px-2 py-1 rounded-full border border-border-default bg-surface-tertiary text-content-tertiary">
                        {u.id.slice(0, 8)}
                      </div>
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
                      <div className="text-xs text-content-tertiary truncate">
                        Owner: {ws.owner_email || ws.owner_id.slice(0, 8)}
                        {ws.is_suspended ? <span className="ml-2 text-status-warning">suspended</span> : null}
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <Button
                        size="sm"
                        variant={ws.is_suspended ? 'secondary' : 'danger'}
                        className={cn(ws.is_suspended ? '' : 'bg-status-error hover:bg-red-600 text-white border-0')}
                        disabled={busyKey === `suspend-ws:${ws.id}`}
                        loading={busyKey === `suspend-ws:${ws.id}`}
                        onClick={async () => {
                          setBusyKey(`suspend-ws:${ws.id}`);
                          try {
                            if (ws.is_suspended) {
                              await ops.resumeWorkspace(ws.id);
                              setWorkspaces((prev) => prev.map((x) => (x.id === ws.id ? { ...x, is_suspended: false } : x)));
                              toast.success('Workspace resumed');
                            } else {
                              await ops.suspendWorkspace(ws.id);
                              setWorkspaces((prev) => prev.map((x) => (x.id === ws.id ? { ...x, is_suspended: true } : x)));
                              toast.success('Workspace suspended');
                            }
                          } catch (e: unknown) {
                            toast.error(e instanceof Error ? e.message : 'Failed');
                          } finally {
                            setBusyKey(null);
                          }
                        }}
                      >
                        {ws.is_suspended ? 'Resume' : 'Suspend'}
                      </Button>
                      <div className="text-[11px] px-2 py-1 rounded-full border border-border-default bg-surface-tertiary text-content-tertiary shrink-0">
                        {ws.id.slice(0, 8)}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </Card>
        </div>
      )}

      <Modal
        open={roleModal.open}
        onClose={() => setRoleModal({ open: false, userId: '', email: '', role: 'member' })}
        title="Change User Role"
        footer={
          <>
            <Button variant="secondary" onClick={() => setRoleModal({ open: false, userId: '', email: '', role: 'member' })}>
              Cancel
            </Button>
            <Button
              disabled={!roleModal.userId || busyKey === `role:${roleModal.userId}`}
              loading={busyKey === `role:${roleModal.userId}`}
              onClick={async () => {
                if (!roleModal.userId) return;
                setBusyKey(`role:${roleModal.userId}`);
                try {
                  await ops.setUserRole(roleModal.userId, roleModal.role);
                  setUsers((prev) => prev.map((x) => (x.id === roleModal.userId ? { ...x, role: roleModal.role } : x)));
                  toast.success('Role updated');
                  setRoleModal({ open: false, userId: '', email: '', role: 'member' });
                } catch (e: unknown) {
                  toast.error(e instanceof Error ? e.message : 'Failed to update role');
                } finally {
                  setBusyKey(null);
                }
              }}
            >
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="text-sm text-content-secondary">
            User: <span className="font-mono text-xs">{roleModal.email || roleModal.userId}</span>
          </div>
          <label className="block">
            <div className="text-xs text-content-tertiary mb-1.5">Role</div>
            <select
              value={roleModal.role}
              onChange={(e) => setRoleModal((p) => ({ ...p, role: e.target.value }))}
              className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm capitalize"
            >
              <option value="member">member</option>
              <option value="ops">ops</option>
              <option value="admin">admin</option>
            </select>
          </label>
          <div className="text-xs text-content-tertiary">
            Ops/Admin can access the ops dashboard. Use with care.
          </div>
        </div>
      </Modal>
    </div>
  );
}
