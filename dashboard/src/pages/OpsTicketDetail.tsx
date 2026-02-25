import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, MessageSquare, RefreshCcw, Save } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { useSession } from '../lib/session';
import { cn, truncate } from '../lib/utils';
import type { OpsTicketDetail } from '../types';

const statusOptions = [
  { value: 'open', label: 'Open' },
  { value: 'pending', label: 'Pending' },
  { value: 'solved', label: 'Solved' },
  { value: 'closed', label: 'Closed' },
];

const priorityOptions = [
  { value: 'low', label: 'Low' },
  { value: 'normal', label: 'Normal' },
  { value: 'high', label: 'High' },
  { value: 'urgent', label: 'Urgent' },
];

const categoryOptions = [
  { value: 'support', label: 'Support' },
  { value: 'feature_request', label: 'Feature Request' },
  { value: 'bug', label: 'Bug' },
  { value: 'security', label: 'Security' },
  { value: 'billing', label: 'Billing' },
  { value: 'how_to', label: 'How-to' },
  { value: 'incident', label: 'Incident' },
  { value: 'feedback', label: 'Feedback' },
];

const componentOptions = [
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

export function OpsTicketDetailPage() {
  const navigate = useNavigate();
  const { ticketId } = useParams<{ ticketId: string }>();
  const { user } = useSession();

  const [data, setData] = useState<OpsTicketDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [status, setStatus] = useState('open');
  const [priority, setPriority] = useState('normal');
  const [ticketCategory, setTicketCategory] = useState('support');
  const [assignedTo, setAssignedTo] = useState('');
  const [ticketComponent, setTicketComponent] = useState('');
  const [ticketTags, setTicketTags] = useState('');

  const [message, setMessage] = useState('');
  const [internal, setInternal] = useState(false);
  const [saving, setSaving] = useState(false);
  const [sending, setSending] = useState(false);

  const load = () => {
    if (!ticketId) return;
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getTicket(ticketId)
      .then((d) => {
        setData(d);
        setStatus(d.ticket.status || 'open');
        setPriority(d.ticket.priority || 'normal');
        setTicketCategory(d.ticket.category || 'support');
        setAssignedTo(d.ticket.assigned_to || '');
        setTicketComponent(d.ticket.component || '');
        setTicketTags((d.ticket.tags || []).join(', '));
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load ticket');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ticketId]);

  const changed = useMemo(() => {
    if (!data) return false;
    const currentTags = (data.ticket.tags || []).join(',');
    const nextTags = ticketTags.split(',').map((v) => v.trim()).filter(Boolean).join(',');
    return (data.ticket.status || '') !== status
      || (data.ticket.priority || '') !== priority
      || (data.ticket.category || 'support') !== ticketCategory
      || (data.ticket.assigned_to || '') !== assignedTo
      || (data.ticket.component || '') !== ticketComponent
      || currentTags !== nextTags;
  }, [assignedTo, data, priority, status, ticketCategory, ticketComponent, ticketTags]);

  const save = async () => {
    if (!ticketId) return;
    setSaving(true);
    try {
      await ops.updateTicket(ticketId, {
        status,
        priority,
        category: ticketCategory,
        assigned_to: assignedTo || '',
        component: ticketComponent,
        tags: ticketTags.split(',').map((v) => v.trim()).filter(Boolean),
      });
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to update ticket');
    } finally {
      setSaving(false);
    }
  };

  const send = async () => {
    if (!ticketId) return;
    const body = message.trim();
    if (!body) return;
    setSending(true);
    try {
      await ops.createTicketMessage(ticketId, { message: body, is_internal: internal });
      setMessage('');
      setInternal(false);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to send message');
    } finally {
      setSending(false);
    }
  };

  const t = data?.ticket;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <button
            onClick={() => navigate('/ops/tickets')}
            className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
            Back to Tickets
          </button>
          <h1 className="text-2xl font-semibold text-content-primary mt-2">Ticket</h1>
          {t && (
            <p className="text-sm text-content-secondary mt-1">
              {t.subject} <span className="text-content-tertiary">({truncate(t.id, 10)})</span>
            </p>
          )}
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">Forbidden.</Card>
      ) : loading ? (
        <Card className="p-6 space-y-3">
          <Skeleton className="w-64 h-6" />
          <Skeleton className="w-full h-24" />
        </Card>
      ) : error ? (
        <Card className="p-6 text-sm text-status-error">{error}</Card>
      ) : !data || !t ? (
        <Card className="p-8 text-center text-sm text-content-tertiary">Not found.</Card>
      ) : (
        <>
          <Card className="p-6">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Customer</div>
                <div className="text-sm font-semibold text-content-primary mt-2">{t.created_by_email || t.created_by_username || 'Customer'}</div>
                <div className="text-xs text-content-tertiary mt-1">
                  Workspace: {t.workspace_name || truncate(t.workspace_id, 10) || '—'}
                </div>
              </div>

              <div className="space-y-3">
                <div className="flex flex-wrap items-center gap-3">
                  <label className="text-sm text-content-secondary">
                    Status
                    <select
                      value={status}
                      onChange={(e) => setStatus(e.target.value)}
                      className="ml-2 h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                    >
                      {statusOptions.map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  </label>

                  <label className="text-sm text-content-secondary">
                    Priority
                    <select
                      value={priority}
                      onChange={(e) => setPriority(e.target.value)}
                      className="ml-2 h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                    >
                      {priorityOptions.map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  </label>

                  <label className="text-sm text-content-secondary">
                    Category
                    <select
                      value={ticketCategory}
                      onChange={(e) => setTicketCategory(e.target.value)}
                      className="ml-2 h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                    >
                      {categoryOptions.map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  </label>

                  <label className="text-sm text-content-secondary">
                    Component
                    <select
                      value={ticketComponent}
                      onChange={(e) => setTicketComponent(e.target.value)}
                      className="ml-2 h-9 px-2 rounded-md bg-surface-secondary border border-border-default text-sm"
                    >
                      {componentOptions.map((o) => (
                        <option key={o.value || 'none'} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <div className="text-sm text-content-secondary">
                    Assigned to:{' '}
                    <span className="font-mono text-xs text-content-tertiary">{assignedTo ? truncate(assignedTo, 12) : '—'}</span>
                  </div>
                  {user?.id && (
                    <button
                      onClick={() => setAssignedTo(user.id)}
                      className="text-xs text-brand hover:text-brand-hover transition-colors"
                    >
                      Assign to me
                    </button>
                  )}
                </div>

                <label className="block">
                  <div className="text-xs text-content-tertiary mb-1">Tags (comma-separated)</div>
                  <input
                    value={ticketTags}
                    onChange={(e) => setTicketTags(e.target.value)}
                    placeholder="mcp, api"
                    className="w-full h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                  />
                </label>

                <div className="flex items-center gap-2">
                  <Button variant="secondary" onClick={save} loading={saving} disabled={!changed}>
                    <Save className="w-4 h-4" />
                    Save
                  </Button>
                </div>
              </div>
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary flex items-center gap-2">
              <MessageSquare className="w-4 h-4 text-content-tertiary" />
              Messages
            </div>

            <div className="p-4 space-y-3">
              {data.messages.length === 0 ? (
                <div className="text-sm text-content-tertiary text-center py-6">No messages.</div>
              ) : (
                data.messages.map((m) => (
                  <div
                    key={m.id}
                    className={cn(
                      'border rounded-md p-3',
                      m.is_internal ? 'border-border-default bg-surface-tertiary' : 'border-border-subtle bg-surface-secondary'
                    )}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-xs text-content-tertiary font-mono truncate">
                        {m.author_id ? truncate(m.author_id, 10) : 'unknown'}{m.is_internal ? ' · internal' : ''}
                      </div>
                      <div className="text-xs text-content-tertiary">{new Date(m.created_at).toLocaleString()}</div>
                    </div>
                    <div className="text-sm text-content-secondary mt-2 whitespace-pre-wrap">{m.body}</div>
                  </div>
                ))
              )}

              <div className="pt-3 border-t border-border-subtle">
                <div className="text-sm font-semibold text-content-primary mb-2">Reply</div>
                <textarea
                  value={message}
                  onChange={(e) => setMessage(e.target.value)}
                  placeholder="Write a response..."
                  className="w-full min-h-[110px] rounded-md bg-surface-secondary border border-border-default p-3 text-sm focus:outline-none focus:ring-2 focus:ring-brand/12"
                />
                <label className="mt-2 flex items-center gap-2 text-sm text-content-secondary">
                  <input type="checkbox" checked={internal} onChange={(e) => setInternal(e.target.checked)} className="accent-brand" />
                  Internal note (not visible to customer)
                </label>
                <div className="mt-3">
                  <Button onClick={send} loading={sending} disabled={!message.trim()}>
                    Send
                  </Button>
                </div>
              </div>
            </div>
          </Card>
        </>
      )}
    </div>
  );
}
