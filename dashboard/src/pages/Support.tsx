import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { LifeBuoy, Plus, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, support } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import type { SupportTicket } from '../types';

export function SupportPage() {
  const navigate = useNavigate();
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [subject, setSubject] = useState('');
  const [message, setMessage] = useState('');
  const [priority, setPriority] = useState('normal');
  const [creating, setCreating] = useState(false);

  const load = () => {
    setLoading(true);
    setError(null);
    support
      .listTickets({ limit: 50 })
      .then(setTickets)
      .catch((e) => setError(e?.message || 'Failed to load tickets'))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
  }, []);

  const create = async () => {
    const s = subject.trim();
    const m = message.trim();
    if (!s || !m) return;
    setCreating(true);
    setError(null);
    try {
      const created = await support.createTicket({ subject: s, message: m, priority });
      setSubject('');
      setMessage('');
      setPriority('normal');
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
        <div className="mt-3 grid grid-cols-1 md:grid-cols-3 gap-3">
          <label className="block md:col-span-2">
            <div className="text-xs text-content-tertiary mb-1.5">Subject</div>
            <input
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
              placeholder="Deploy failed, billing question, domain issue..."
            />
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
              <div className="col-span-7">Subject</div>
              <div className="col-span-3">Status</div>
              <div className="col-span-2 text-right">Updated</div>
            </div>
            {tickets.map((t) => (
              <button
                key={t.id}
                onClick={() => navigate(`/support/${encodeURIComponent(t.id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-7 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{t.subject}</div>
                  <div className="text-xs text-content-tertiary font-mono">{truncate(t.id, 12)}</div>
                </div>
                <div className="col-span-3 text-sm">
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

