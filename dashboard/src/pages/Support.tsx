import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { LifeBuoy, Plus, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, support } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import type { SupportTicket, SupportTicketComponent, TicketCategory } from '../types';

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

const supportCategories: Array<{ value: TicketCategory; label: string }> = [
  { value: 'support', label: 'Support' },
  { value: 'feature_request', label: 'Feature Request' },
  { value: 'bug', label: 'Bug' },
  { value: 'security', label: 'Security' },
  { value: 'billing', label: 'Billing' },
  { value: 'how_to', label: 'How-to' },
  { value: 'incident', label: 'Incident' },
  { value: 'feedback', label: 'Feedback' },
];

const componentOptions: Array<{ value: SupportTicketComponent; label: string }> = [
  { value: '', label: 'Unspecified' },
  { value: 'services', label: 'Services' },
  { value: 'databases', label: 'Databases' },
  { value: 'key-value', label: 'Key-Value' },
  { value: 'deployments', label: 'Deployments' },
  { value: 'env-vars', label: 'Env Vars' },
  { value: 'domains', label: 'Domains' },
  { value: 'mcp-api', label: 'MCP / API' },
  { value: 'billing', label: 'Billing' },
  { value: 'auth', label: 'Auth' },
  { value: 'builds', label: 'Builds' },
  { value: 'dashboard', label: 'Dashboard' },
];

const statusOptions = [
  { value: 'all', label: 'All' },
  { value: 'open', label: 'Open' },
  { value: 'pending', label: 'Pending' },
  { value: 'solved', label: 'Solved' },
  { value: 'closed', label: 'Closed' },
] as const;

export function SupportPage() {
  const navigate = useNavigate();
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [subject, setSubject] = useState('');
  const [message, setMessage] = useState('');
  const [priority, setPriority] = useState('normal');
  const [category, setCategory] = useState<TicketCategory>('support');
  const [component, setComponent] = useState<SupportTicketComponent>('');
  const [tags, setTags] = useState('');

  const [statusFilter, setStatusFilter] = useState<(typeof statusOptions)[number]['value']>('all');
  const [categoryFilter, setCategoryFilter] = useState<'all' | TicketCategory>('all');
  const [componentFilter, setComponentFilter] = useState<'all' | SupportTicketComponent>('all');
  const [queryFilter, setQueryFilter] = useState('');
  const [tagFilter, setTagFilter] = useState('');
  const [creating, setCreating] = useState(false);

  const parsedFilterTags = useMemo(() => tagFilter.split(',').map((v) => v.trim()).filter(Boolean), [tagFilter]);
  const parsedCreateTags = useMemo(() => tags.split(',').map((v) => v.trim()).filter(Boolean), [tags]);

  const load = () => {
    setLoading(true);
    setError(null);
    support
      .listTickets({
        limit: 50,
        status: statusFilter === 'all' ? undefined : statusFilter,
        category: categoryFilter === 'all' ? undefined : categoryFilter,
        component: componentFilter === 'all' ? undefined : componentFilter,
        query: queryFilter.trim() || undefined,
        tags: parsedFilterTags.length > 0 ? parsedFilterTags : undefined,
      })
      .then(setTickets)
      .catch((e) => setError(e?.message || 'Failed to load tickets'))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statusFilter, categoryFilter, componentFilter, queryFilter, parsedFilterTags.join(',')]);

  const create = async () => {
    const s = subject.trim();
    const m = message.trim();
    if (!s || !m) return;
    setCreating(true);
    setError(null);
    try {
      const created = await support.createTicket({
        subject: s,
        message: m,
        priority,
        category,
        component: component || undefined,
        tags: parsedCreateTags,
      });
      setSubject('');
      setMessage('');
      setPriority('normal');
      setCategory('support');
      setComponent('');
      setTags('');
      load();
      navigate(`/support/${encodeURIComponent(created.ticket.id)}`);
    } catch (e: unknown) {
      if (e instanceof ApiError) {
        setError(e.message);
      } else {
        setError(e instanceof Error ? e.message : 'Failed to create ticket');
      }
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Support</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <LifeBuoy className="w-5 h-5 text-content-tertiary" />
            Tickets
          </h1>
          <p className="text-sm text-content-secondary mt-1">Get help from the RailPush team.</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      {error && <Card className="p-4 text-sm text-status-error">{error}</Card>}

      <Card className="p-6">
        <div className="text-sm font-semibold text-content-primary">Create a ticket</div>
        <div className="mt-3 grid grid-cols-1 md:grid-cols-6 gap-3">
          <label className="block md:col-span-3">
            <div className="text-xs text-content-tertiary mb-1.5">Subject</div>
            <input
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
              placeholder="Deploy failed, billing question, domain issue..."
            />
          </label>
          <label className="block">
            <div className="text-xs text-content-tertiary mb-1.5">Category</div>
            <select
              value={category}
              onChange={(e) => setCategory(e.target.value as TicketCategory)}
              className="w-full h-10 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              {supportCategories.map((item) => (
                <option key={item.value} value={item.value}>{item.label}</option>
              ))}
            </select>
          </label>
          <label className="block">
            <div className="text-xs text-content-tertiary mb-1.5">Priority</div>
            <select
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="w-full h-10 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="urgent">Urgent</option>
            </select>
          </label>
          <label className="block md:col-span-2">
            <div className="text-xs text-content-tertiary mb-1.5">Component</div>
            <select
              value={component}
              onChange={(e) => setComponent(e.target.value as SupportTicketComponent)}
              className="w-full h-10 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              {componentOptions.map((item) => (
                <option key={item.value || 'none'} value={item.value}>{item.label}</option>
              ))}
            </select>
          </label>
          <label className="block md:col-span-4">
            <div className="text-xs text-content-tertiary mb-1.5">Tags (comma-separated)</div>
            <input
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
              placeholder="mcp, api, agent-dx"
            />
          </label>
        </div>
        <div className="mt-3">
          <div className="text-xs text-content-tertiary mb-1.5">Message</div>
          <textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            className="w-full min-h-[120px] rounded-md bg-surface-secondary border border-border-default p-3 text-sm"
            placeholder="Describe what happened and include any relevant service name / time / logs."
          />
        </div>
        <div className="mt-3">
          <Button onClick={create} loading={creating} disabled={!subject.trim() || !message.trim()}>
            <Plus className="w-4 h-4" />
            Create
          </Button>
        </div>
      </Card>

      <Card className="p-0 overflow-hidden">
        <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">Your tickets</div>
        <div className="px-4 py-3 border-b border-border-default/60 bg-surface-tertiary/20 grid grid-cols-1 md:grid-cols-5 gap-3">
          <label className="text-xs text-content-tertiary">
            Status
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as (typeof statusOptions)[number]['value'])}
              className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              {statusOptions.map((item) => (
                <option key={item.value} value={item.value}>{item.label}</option>
              ))}
            </select>
          </label>
          <label className="text-xs text-content-tertiary">
            Category
            <select
              value={categoryFilter}
              onChange={(e) => setCategoryFilter(e.target.value as 'all' | TicketCategory)}
              className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              <option value="all">All</option>
              {supportCategories.map((item) => (
                <option key={item.value} value={item.value}>{item.label}</option>
              ))}
            </select>
          </label>
          <label className="text-xs text-content-tertiary">
            Component
            <select
              value={componentFilter}
              onChange={(e) => setComponentFilter(e.target.value as 'all' | SupportTicketComponent)}
              className="mt-1 w-full h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
            >
              <option value="all">All</option>
              {componentOptions.filter((item) => item.value !== '').map((item) => (
                <option key={item.value} value={item.value}>{item.label}</option>
              ))}
            </select>
          </label>
          <label className="text-xs text-content-tertiary md:col-span-2">
            Search / Tags
            <div className="mt-1 grid grid-cols-1 md:grid-cols-2 gap-2">
              <input
                value={queryFilter}
                onChange={(e) => setQueryFilter(e.target.value)}
                placeholder="Search subject"
                className="h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
              />
              <input
                value={tagFilter}
                onChange={(e) => setTagFilter(e.target.value)}
                placeholder="tags: mcp,api"
                className="h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
              />
            </div>
          </label>
        </div>
        {loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-md p-3">
                <Skeleton className="w-64 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-20 h-4" />
              </div>
            ))}
          </div>
        ) : tickets.length === 0 ? (
          <div className="p-8 text-center text-sm text-content-tertiary">No tickets yet.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-4">Subject</div>
              <div className="col-span-2">Category</div>
              <div className="col-span-2">Component</div>
              <div className="col-span-2">Status</div>
              <div className="col-span-2 text-right">Updated</div>
            </div>
            {tickets.map((t) => (
              <button
                key={t.id}
                onClick={() => navigate(`/support/${encodeURIComponent(t.id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-4 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{t.subject}</div>
                  <div className="text-xs text-content-tertiary font-mono">{truncate(t.id, 12)}</div>
                  {(t.tags || []).length > 0 && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {(t.tags || []).slice(0, 4).map((tag) => (
                        <span key={tag} className="text-[10px] px-1.5 py-0.5 rounded border border-border-default bg-surface-secondary text-content-tertiary">#{tag}</span>
                      ))}
                    </div>
                  )}
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
                </div>
                <div className="col-span-2 text-sm text-content-secondary">
                  {componentOptions.find((item) => item.value === (t.component || ''))?.label || 'Unspecified'}
                </div>
                <div className="col-span-2 text-sm">
                  <span className={cn('text-xs px-2 py-0.5 rounded-full border inline-flex', 'border-border-default bg-surface-secondary text-content-secondary')}>
                    {t.status}
                  </span>
                  <div className="text-xs text-content-tertiary mt-1">priority: {t.priority}</div>
                </div>
                <div className="col-span-2 text-right text-xs text-content-tertiary">{new Date(t.updated_at).toLocaleString()}</div>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
