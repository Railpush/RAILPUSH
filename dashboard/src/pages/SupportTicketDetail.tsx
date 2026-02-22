import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, MessageSquare, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, support } from '../lib/api';
import { cn } from '../lib/utils';
import type { SupportTicket, SupportTicketMessage, TicketCategory } from '../types';

const categoryLabels: Record<TicketCategory, string> = {
  support: 'Support',
  feature_request: 'Feature Request',
  bug_report: 'Bug Report',
};

export function SupportTicketDetailPage() {
  const navigate = useNavigate();
  const { ticketId } = useParams<{ ticketId: string }>();

  const [ticket, setTicket] = useState<SupportTicket | null>(null);
  const [messages, setMessages] = useState<SupportTicketMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);

  const load = () => {
    if (!ticketId) return;
    setLoading(true);
    setError(null);
    support
      .getTicket(ticketId)
      .then((d) => {
        setTicket(d.ticket);
        setMessages(d.messages);
      })
      .catch((e) => {
        if (e instanceof ApiError) setError(e.message);
        else setError(e?.message || 'Failed to load ticket');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ticketId]);

  const send = async () => {
    if (!ticketId) return;
    const body = message.trim();
    if (!body) return;
    setSending(true);
    setError(null);
    try {
      const m = await support.createMessage(ticketId, { message: body });
      setMessages((prev) => [...prev, m]);
      setMessage('');
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to send message');
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <button
            onClick={() => navigate('/support')}
            className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
            Back to Support
          </button>
          <h1 className="text-2xl font-semibold text-content-primary mt-2">Ticket</h1>
          {ticket && (
            <div className="flex items-center gap-2 mt-1">
              <p className="text-sm text-content-secondary">{ticket.subject}</p>
              <span className={cn('text-xs px-2 py-0.5 rounded-full border inline-flex',
                ticket.category === 'feature_request' ? 'border-purple-400/30 bg-purple-500/10 text-purple-300' :
                ticket.category === 'bug_report' ? 'border-red-400/30 bg-red-500/10 text-red-300' :
                'border-border-default bg-surface-secondary text-content-secondary'
              )}>
                {categoryLabels[ticket.category as TicketCategory] || ticket.category || 'Support'}
              </span>
            </div>
          )}
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      {error && <Card className="p-4 text-sm text-status-error">{error}</Card>}

      {loading ? (
        <Card className="p-6 space-y-3">
          <Skeleton className="w-64 h-6" />
          <Skeleton className="w-full h-24" />
        </Card>
      ) : !ticket ? (
        <Card className="p-8 text-center text-sm text-content-tertiary">Not found.</Card>
      ) : (
        <Card className="p-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary flex items-center gap-2">
            <MessageSquare className="w-4 h-4 text-content-tertiary" />
            Conversation
          </div>
          <div className="p-4 space-y-3">
            {messages.length === 0 ? (
              <div className="text-sm text-content-tertiary text-center py-6">No messages yet.</div>
            ) : (
              messages.map((m) => (
                <div key={m.id} className="border border-border-subtle rounded-md p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-xs text-content-tertiary">message</div>
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
                placeholder="Add a message..."
                className="w-full min-h-[110px] rounded-md bg-surface-secondary border border-border-default p-3 text-sm"
              />
              <div className="mt-3">
                <Button onClick={send} loading={sending} disabled={!message.trim()}>
                  Send
                </Button>
              </div>
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}

