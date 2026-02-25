import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Download, LifeBuoy, RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import { toast } from 'sonner';
import type { OpsTicketFacets, OpsTicketItem, SupportTicketComponent, TicketCategory } from '../types';

type StatusFilter = 'all' | 'open' | 'pending' | 'solved' | 'closed';
type CategoryFilter = 'all' | 'support' | 'feature_request' | 'bug' | 'security' | 'billing' | 'how_to' | 'incident' | 'feedback';
type PriorityFilter = 'all' | 'low' | 'normal' | 'high' | 'urgent';
type ComponentFilter = 'all' | Exclude<SupportTicketComponent, ''>;
type SortBy = 'updated_at' | 'created_at' | 'priority';
type SortOrder = 'asc' | 'desc';

const categoryLabels: Record<TicketCategory, string> = {
  support: 'Support',
  feature_request: 'Feature Request',
  bug: 'Bug',
  security: 'Security',
  billing: 'Billing',
  how_to: 'How-to',
  incident: 'Incident',
  feedback: 'Feedback',
  bug_report: 'Bug',
};

const componentLabels: Record<Exclude<SupportTicketComponent, ''>, string> = {
  services: 'Services',
  databases: 'Databases',
  'key-value': 'Key-Value',
  deployments: 'Deployments',
  'env-vars': 'Env Vars',
  domains: 'Domains',
  'mcp-api': 'MCP / API',
  billing: 'Billing',
  auth: 'Auth',
  builds: 'Builds',
  dashboard: 'Dashboard',
};

const emptyFacets: OpsTicketFacets = {
  by_status: {},
  by_priority: {},
  by_category: {},
  by_component: {},
  by_tag: {},
};

const statusLabels: Record<Exclude<StatusFilter, 'all'>, string> = {
  open: 'Open',
  pending: 'Pending',
  solved: 'Solved',
  closed: 'Closed',
};

function csvEscape(value: string) {
  if (value.includes(',') || value.includes('"') || value.includes('\n')) {
    return `"${value.replace(/"/g, '""')}"`;
  }
  return value;
}

export function OpsTicketsPage() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<StatusFilter>('all');
  const [categoryFilter, setCategoryFilter] = useState<CategoryFilter>('all');
  const [priorityFilter, setPriorityFilter] = useState<PriorityFilter>('all');
  const [componentFilter, setComponentFilter] = useState<ComponentFilter>('all');
  const [tagFilter, setTagFilter] = useState('');
  const [query, setQuery] = useState('');
  const [createdAfter, setCreatedAfter] = useState('');
  const [createdBefore, setCreatedBefore] = useState('');
  const [sortBy, setSortBy] = useState<SortBy>('updated_at');
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc');
  const [rows, setRows] = useState<OpsTicketItem[]>([]);
  const [total, setTotal] = useState(0);
  const [facets, setFacets] = useState<OpsTicketFacets>(emptyFacets);
  const [selected, setSelected] = useState<string[]>([]);
  const [bulkStatus, setBulkStatus] = useState('');
  const [bulkPriority, setBulkPriority] = useState('');
  const [bulkCategory, setBulkCategory] = useState('');
  const [bulkComponent, setBulkComponent] = useState('');
  const [bulkTags, setBulkTags] = useState('');
  const [bulkReason, setBulkReason] = useState('');
  const [bulkBusy, setBulkBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);
  const parsedTags = useMemo(() => tagFilter.split(',').map((v) => v.trim()).filter(Boolean), [tagFilter]);
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const allVisibleSelected = rows.length > 0 && rows.every((r) => selectedSet.has(r.id));
  const openCount = facets.by_status.open || 0;
  const pendingCount = facets.by_status.pending || 0;
  const solvedCount = facets.by_status.solved || 0;
  const closedCount = facets.by_status.closed || 0;

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .searchTickets({
        status: status === 'all' ? '' : status,
        category: categoryFilter === 'all' ? '' : categoryFilter,
        priority: priorityFilter === 'all' ? '' : priorityFilter,
        component: componentFilter === 'all' ? '' : componentFilter,
        tags: parsedTags.length ? parsedTags : undefined,
        query: q,
        created_after: createdAfter || undefined,
        created_before: createdBefore || undefined,
        sort_by: sortBy,
        sort_order: sortOrder,
        limit: 200,
      })
      .then((res) => {
        setRows(res.tickets || []);
        setTotal(res.total || 0);
        setFacets(res.facets || emptyFacets);
      })
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
  }, [status, categoryFilter, priorityFilter, componentFilter, parsedTags.join(','), q, createdAfter, createdBefore, sortBy, sortOrder]);

  useEffect(() => {
    setSelected([]);
  }, [status, categoryFilter, priorityFilter, componentFilter, parsedTags.join(','), q, createdAfter, createdBefore, sortBy, sortOrder]);

  const toggleRow = (ticketID: string) => {
    setSelected((prev) => (prev.includes(ticketID) ? prev.filter((id) => id !== ticketID) : [...prev, ticketID]));
  };

  const toggleAllVisible = () => {
    if (allVisibleSelected) {
      setSelected((prev) => prev.filter((id) => !rows.some((r) => r.id === id)));
      return;
    }
    const visible = rows.map((r) => r.id);
    setSelected((prev) => Array.from(new Set([...prev, ...visible])));
  };

  const applyBulkUpdate = async () => {
    if (selected.length === 0) return;
    const patch: { status?: string; priority?: string; category?: string; component?: string; tags?: string[]; reason?: string } = {};
    if (bulkStatus) patch.status = bulkStatus;
    if (bulkPriority) patch.priority = bulkPriority;
    if (bulkCategory) patch.category = bulkCategory;
    if (bulkComponent) patch.component = bulkComponent;
    const parsedBulkTags = bulkTags.split(',').map((v) => v.trim()).filter(Boolean);
    if (parsedBulkTags.length > 0) patch.tags = parsedBulkTags;
    if (bulkReason.trim()) patch.reason = bulkReason.trim();
    if (!patch.status && !patch.priority && !patch.category && !patch.component && !patch.tags) {
      toast.error('Select at least one field to update');
      return;
    }

    setBulkBusy(true);
    try {
      const res = await ops.bulkUpdateTickets({
        ticket_ids: selected,
        status: patch.status,
        priority: patch.priority,
        category: patch.category,
        component: patch.component,
        tags: patch.tags,
        reason: patch.reason,
      });
      toast.success(`Updated ${res.updated} tickets`);
      setBulkReason('');
      setBulkTags('');
      setBulkComponent('');
      setSelected([]);
      load();
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : 'Bulk update failed');
    } finally {
      setBulkBusy(false);
    }
  };

  const exportCsv = () => {
    if (rows.length === 0) {
      toast.info('No tickets to export');
      return;
    }
    const headers = ['id', 'subject', 'status', 'priority', 'category', 'component', 'tags', 'workspace_name', 'created_by_email', 'updated_at'];
    const lines = [headers.join(',')];
    for (const row of rows) {
      lines.push([
        row.id,
        row.subject || '',
        row.status || '',
        row.priority || '',
        row.category || '',
        row.component || '',
        (row.tags || []).join('|'),
        row.workspace_name || '',
        row.created_by_email || row.created_by_username || '',
        row.updated_at || '',
      ].map((value) => csvEscape(String(value))).join(','));
    }
    const blob = new Blob([`${lines.join('\n')}\n`], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `ops-tickets-${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <LifeBuoy className="w-5 h-5 text-content-tertiary" />
            Tickets
          </h1>
          <p className="text-sm text-content-secondary mt-1">Customer support queue. Showing {rows.length} of {total} tickets.</p>
          <p className="text-xs text-content-tertiary mt-1">
            Open {openCount} · Pending {pendingCount} · Solved {solvedCount} · Closed {closedCount}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={exportCsv}>
            <Download className="w-4 h-4" />
            Export CSV
          </Button>
          <Button variant="secondary" onClick={load} loading={loading}>
            <RefreshCcw className="w-4 h-4" />
            Refresh
          </Button>
        </div>
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
                { key: 'bug', label: 'Bug' },
                { key: 'security', label: 'Security' },
                { key: 'billing', label: 'Billing' },
                { key: 'how_to', label: 'How-to' },
                { key: 'incident', label: 'Incident' },
                { key: 'feedback', label: 'Feedback' },
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
              <div className="w-px h-5 bg-border-default mx-1" />
              {([
                { key: 'all', label: 'All Priority' },
                { key: 'urgent', label: 'Urgent' },
                { key: 'high', label: 'High' },
                { key: 'normal', label: 'Normal' },
                { key: 'low', label: 'Low' },
              ] as const).map((t) => (
                <button
                  key={t.key}
                  onClick={() => setPriorityFilter(t.key)}
                  className={cn(
                    'px-3 py-1.5 rounded-full border transition-colors',
                    priorityFilter === t.key
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

          <div className="px-4 py-3 border-b border-border-default/60 bg-surface-tertiary/20 grid grid-cols-1 lg:grid-cols-8 gap-3">
            <label className="text-xs text-content-tertiary">
              Created after
              <input
                type="date"
                value={createdAfter}
                onChange={(e) => setCreatedAfter(e.target.value)}
                className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
              />
            </label>
            <label className="text-xs text-content-tertiary">
              Created before
              <input
                type="date"
                value={createdBefore}
                onChange={(e) => setCreatedBefore(e.target.value)}
                className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
              />
            </label>
            <label className="text-xs text-content-tertiary">
              Sort by
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as SortBy)}
                className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
              >
                <option value="updated_at">Updated At</option>
                <option value="created_at">Created At</option>
                <option value="priority">Priority</option>
              </select>
            </label>
            <label className="text-xs text-content-tertiary">
              Sort order
              <select
                value={sortOrder}
                onChange={(e) => setSortOrder(e.target.value as SortOrder)}
                className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
              >
                <option value="desc">Desc</option>
                <option value="asc">Asc</option>
              </select>
            </label>
            <label className="text-xs text-content-tertiary">
              Component
              <select
                value={componentFilter}
                onChange={(e) => setComponentFilter(e.target.value as ComponentFilter)}
                className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
              >
                <option value="all">All Components</option>
                {Object.entries(componentLabels).map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </label>
            <label className="text-xs text-content-tertiary lg:col-span-2">
              Tag filter (comma-separated)
              <input
                value={tagFilter}
                onChange={(e) => setTagFilter(e.target.value)}
                className="mt-1 w-full h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                placeholder="mcp, agent-dx"
              />
            </label>
            <div className="lg:col-span-8 flex items-end justify-end">
              <Button
                variant="secondary"
                className="h-9"
                onClick={() => {
                  setCreatedAfter('');
                  setCreatedBefore('');
                  setSortBy('updated_at');
                  setSortOrder('desc');
                  setPriorityFilter('all');
                  setCategoryFilter('all');
                  setComponentFilter('all');
                  setTagFilter('');
                  setStatus('all');
                  setQuery('');
                }}
              >
                Reset Filters
              </Button>
            </div>
          </div>

          {selected.length > 0 && (
            <div className="px-4 py-3 border-b border-border-default/60 bg-brand/5 grid grid-cols-1 lg:grid-cols-8 gap-3 items-end">
              <div className="text-xs text-content-secondary lg:col-span-2">
                Selected <span className="font-semibold text-content-primary">{selected.length}</span> tickets
              </div>
              <label className="text-xs text-content-tertiary">
                Bulk status
                <select
                  value={bulkStatus}
                  onChange={(e) => setBulkStatus(e.target.value)}
                  className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                >
                  <option value="">No change</option>
                  {Object.entries(statusLabels).map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </select>
              </label>
              <label className="text-xs text-content-tertiary">
                Bulk priority
                <select
                  value={bulkPriority}
                  onChange={(e) => setBulkPriority(e.target.value)}
                  className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                >
                  <option value="">No change</option>
                  <option value="urgent">Urgent</option>
                  <option value="high">High</option>
                  <option value="normal">Normal</option>
                  <option value="low">Low</option>
                </select>
              </label>
              <label className="text-xs text-content-tertiary">
                Bulk category
                <select
                  value={bulkCategory}
                  onChange={(e) => setBulkCategory(e.target.value)}
                  className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                >
                  <option value="">No change</option>
                  <option value="support">Support</option>
                  <option value="feature_request">Feature Request</option>
                  <option value="bug">Bug</option>
                  <option value="security">Security</option>
                  <option value="billing">Billing</option>
                  <option value="how_to">How-to</option>
                  <option value="incident">Incident</option>
                  <option value="feedback">Feedback</option>
                </select>
              </label>
              <label className="text-xs text-content-tertiary">
                Bulk component
                <select
                  value={bulkComponent}
                  onChange={(e) => setBulkComponent(e.target.value)}
                  className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                >
                  <option value="">No change</option>
                  {Object.entries(componentLabels).map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </select>
              </label>
              <label className="text-xs text-content-tertiary">
                Bulk tags (replace)
                <input
                  value={bulkTags}
                  onChange={(e) => setBulkTags(e.target.value)}
                  placeholder="mcp,api"
                  className="mt-1 w-full h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
              <label className="text-xs text-content-tertiary lg:col-span-2">
                Optional reason (posted to each selected ticket)
                <input
                  value={bulkReason}
                  onChange={(e) => setBulkReason(e.target.value)}
                  placeholder="Resolved in ticket tooling update"
                  className="mt-1 w-full h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
              <div className="lg:col-span-8 flex justify-end">
                <Button onClick={applyBulkUpdate} loading={bulkBusy}>Apply Bulk Update</Button>
              </div>
            </div>
          )}

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
              <div className="col-span-1">
                <input
                  type="checkbox"
                  checked={allVisibleSelected}
                  onChange={toggleAllVisible}
                  className="accent-brand"
                />
              </div>
              <div className="col-span-3">Ticket</div>
              <div className="col-span-2">Customer</div>
              <div className="col-span-2">Category</div>
              <div className="col-span-2">Status</div>
              <div className="col-span-2 text-right">Updated</div>
            </div>
            {rows.map((t) => (
              <div
                key={t.id}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-1 flex items-center">
                  <input
                    type="checkbox"
                    checked={selectedSet.has(t.id)}
                    onChange={() => toggleRow(t.id)}
                    className="accent-brand"
                  />
                </div>
                <button
                  onClick={() => navigate(`/ops/tickets/${encodeURIComponent(t.id)}`)}
                  className="col-span-3 min-w-0 text-left"
                >
                  <div className="text-sm font-semibold text-content-primary truncate">{t.subject}</div>
                  <div className="text-xs text-content-tertiary truncate">{t.workspace_name || truncate(t.workspace_id, 10) || '—'} · {truncate(t.id, 10)}</div>
                  {(t.tags || []).length > 0 && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {(t.tags || []).slice(0, 3).map((tag) => (
                        <span key={tag} className="text-[10px] px-1.5 py-0.5 rounded border border-border-default bg-surface-secondary text-content-tertiary">#{tag}</span>
                      ))}
                    </div>
                  )}
                </button>
                <div className="col-span-2 min-w-0">
                  <div className="text-sm text-content-secondary truncate">{t.created_by_email || t.created_by_username || truncate(t.created_by, 10)}</div>
                  <div className="text-xs text-content-tertiary truncate">priority: {t.priority || 'normal'}</div>
                </div>
                <div className="col-span-2 text-sm">
                  <span className={cn('text-xs px-2 py-0.5 rounded-full border inline-flex',
                    t.category === 'feature_request' ? 'border-purple-400/30 bg-purple-500/10 text-purple-300' :
                    t.category === 'bug' || t.category === 'bug_report' ? 'border-red-400/30 bg-red-500/10 text-red-300' :
                    t.category === 'security' ? 'border-orange-400/30 bg-orange-500/10 text-orange-300' :
                    'border-border-default bg-surface-secondary text-content-secondary'
                  )}>
                    {categoryLabels[t.category as TicketCategory] || t.category || 'Support'}
                  </span>
                  <div className="text-xs text-content-tertiary mt-1">{componentLabels[t.component as keyof typeof componentLabels] || 'Unspecified'}</div>
                </div>
                <div className="col-span-2 text-sm text-content-secondary">{t.status}</div>
                <div className="col-span-2 text-right text-xs text-content-tertiary">
                  {new Date(t.updated_at).toLocaleString()}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
