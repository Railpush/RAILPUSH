import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, ExternalLink, RefreshCcw, Siren, ShieldCheck, VolumeX } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { Modal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { IncidentStatusBadge, SeverityBadge } from '../components/ui/IncidentBadge';
import { ApiError, ops } from '../lib/api';
import { cn, formatDate, formatTime } from '../lib/utils';
import type { IncidentDetail } from '../types';
import { toast } from 'sonner';

export function IncidentDetailPage() {
  const { incidentId } = useParams<{ incidentId: string }>();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<IncidentDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [ackOpen, setAckOpen] = useState(false);
  const [ackNote, setAckNote] = useState('');
  const [ackLoading, setAckLoading] = useState(false);

  const [silenceOpen, setSilenceOpen] = useState(false);
  const [silenceScope, setSilenceScope] = useState<'alertname' | 'group'>('alertname');
  const [silenceMinutes, setSilenceMinutes] = useState(120);
  const [silenceComment, setSilenceComment] = useState('');
  const [silenceLoading, setSilenceLoading] = useState(false);

  const load = () => {
    if (!incidentId) return;
    setLoading(true);
    setError(null);
    setForbidden(false);
    ops
      .getIncident(incidentId, { events_limit: 80 })
      .then(setDetail)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load incident');
      })
      .finally(() => setLoading(false));
  };

  const acknowledge = async () => {
    if (!incidentId) return;
    setAckLoading(true);
    try {
      await ops.acknowledgeIncident(incidentId, { note: ackNote.trim() || undefined });
      toast.success('Incident acknowledged');
      setAckOpen(false);
      setAckNote('');
      load();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e || '');
      toast.error(msg || 'Failed to acknowledge');
    } finally {
      setAckLoading(false);
    }
  };

  const silence = async () => {
    if (!incidentId) return;
    const mins = Number(silenceMinutes);
    if (!Number.isFinite(mins) || mins <= 0) {
      toast.error('Duration must be a positive number of minutes');
      return;
    }
    setSilenceLoading(true);
    try {
      await ops.silenceIncident(incidentId, {
        scope: silenceScope,
        duration_minutes: mins,
        comment: silenceComment.trim() || undefined,
      });
      toast.success('Silence created');
      setSilenceOpen(false);
      setSilenceComment('');
      load();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e || '');
      toast.error(msg || 'Failed to create silence');
    } finally {
      setSilenceLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [incidentId]);

  const latestPayloadPretty = useMemo(() => {
    if (!detail?.latest_payload) return '';
    try {
      return JSON.stringify(detail.latest_payload, null, 2);
    } catch {
      return String(detail.latest_payload);
    }
  }, [detail?.latest_payload]);

  if (!incidentId) {
    return (
      <Card className="p-6 text-sm text-content-secondary">
        Missing incident id.
      </Card>
    );
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="w-56 h-7" />
        <Skeleton className="w-full h-24" />
        <Skeleton className="w-full h-64" />
      </div>
    );
  }

  if (forbidden) {
    return (
      <Card className="p-8 text-center text-sm text-content-secondary">
        Your account doesn’t have access to ops incidents.
        <div className="text-xs text-content-tertiary mt-2">
          Ask an admin to set your user role to <code className="font-mono">admin</code>.
        </div>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
    );
  }

  if (!detail) {
    return (
      <Card className="p-8 text-center text-sm text-content-secondary">Incident not found.</Card>
    );
  }

  const isAcknowledged = !!detail.acknowledged_at;
  const isSilenced = !!detail.silence_id && !!detail.silenced_until;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <button
            onClick={() => navigate('/ops/incidents')}
            className="mt-1 p-2 rounded-lg bg-surface-secondary border border-border-default hover:border-border-hover hover:bg-surface-tertiary/40 transition-colors"
            title="Back"
          >
            <ArrowLeft className="w-4 h-4 text-content-secondary" />
          </button>
          <div>
            <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
            <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2 flex-wrap">
              <Siren className="w-5 h-5 text-status-warning" />
              {detail.alertname || 'Incident'}
              <IncidentStatusBadge status={detail.status} />
              <SeverityBadge severity={detail.severity} />
            </h1>
            {(detail.summary || detail.description) && (
              <p className="text-sm text-content-secondary mt-2 max-w-3xl">
                {detail.summary || detail.description}
              </p>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant={isAcknowledged ? 'secondary' : 'outline'}
            onClick={() => setAckOpen(true)}
            disabled={isAcknowledged}
          >
            <ShieldCheck className="w-4 h-4" />
            {isAcknowledged ? 'Acknowledged' : 'Acknowledge'}
          </Button>
          <Button
            variant="outline"
            onClick={() => setSilenceOpen(true)}
          >
            <VolumeX className="w-4 h-4" />
            {isSilenced ? 'Silenced' : 'Silence'}
          </Button>
          <Button variant="secondary" onClick={load}>
            <RefreshCcw className="w-4 h-4" />
            Refresh
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Details</div>
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <div className="text-xs text-content-tertiary">Namespace</div>
              <div className="text-content-primary font-medium">{detail.namespace || 'default'}</div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Receiver</div>
              <div className="text-content-primary font-medium">{detail.receiver || 'default'}</div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">First Seen</div>
              <div className="text-content-primary font-medium">
                {formatDate(detail.first_seen_at)} {formatTime(detail.first_seen_at)}
              </div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Last Seen</div>
              <div className="text-content-primary font-medium">
                {formatDate(detail.last_seen_at)} {formatTime(detail.last_seen_at)}
              </div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Events</div>
              <div className="text-content-primary font-medium">{detail.event_count}</div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Alerts (latest)</div>
              <div className="text-content-primary font-medium">{detail.alerts_count}</div>
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Acknowledged</div>
              <div className="text-content-primary font-medium">
                {detail.acknowledged_at ? `${formatDate(detail.acknowledged_at)} ${formatTime(detail.acknowledged_at)}` : 'No'}
              </div>
              {detail.acknowledged_by && (
                <div className="text-xs text-content-tertiary mt-0.5">by {detail.acknowledged_by}</div>
              )}
            </div>
            <div>
              <div className="text-xs text-content-tertiary">Silenced Until</div>
              <div className="text-content-primary font-medium">
                {detail.silenced_until ? `${formatDate(detail.silenced_until)} ${formatTime(detail.silenced_until)}` : 'No'}
              </div>
              {detail.silenced_by && (
                <div className="text-xs text-content-tertiary mt-0.5">by {detail.silenced_by}</div>
              )}
            </div>
          </div>

          {detail.ack_note && (
            <div className="mt-4 pt-4 border-t border-border-subtle">
              <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-2">Acknowledgement Note</div>
              <div className="text-sm text-content-secondary whitespace-pre-wrap">{detail.ack_note}</div>
            </div>
          )}

          {detail.runbook_url && (
            <div className="mt-4 pt-4 border-t border-border-subtle">
              <a
                href={detail.runbook_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 text-sm text-brand hover:text-brand-hover"
              >
                <ExternalLink className="w-4 h-4" />
                Open runbook
              </a>
            </div>
          )}
        </Card>

        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Timeline</div>
          {detail.events.length === 0 ? (
            <div className="text-sm text-content-secondary">No events yet.</div>
          ) : (
            <div className="space-y-2 max-h-[340px] overflow-auto pr-1">
              {detail.events.map((ev) => (
                <div
                  key={ev.id}
                  className={cn(
                    'flex items-center justify-between gap-3 rounded-xl border p-3',
                    (ev.status || '').toLowerCase() === 'firing'
                      ? 'border-status-error/20 bg-status-error/5'
                      : 'border-border-subtle bg-surface-secondary'
                  )}
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <IncidentStatusBadge status={ev.status} size="sm" />
                      <span className="text-xs text-content-tertiary">
                        {formatDate(ev.received_at)} {formatTime(ev.received_at)}
                      </span>
                    </div>
                    <div className="text-xs text-content-secondary mt-1">
                      {ev.alerts_count} alert(s) · receiver: {ev.receiver || 'default'}
                    </div>
                  </div>
                  <code className="text-[11px] font-mono text-content-tertiary bg-surface-tertiary px-2 py-1 rounded border border-border-default">
                    {ev.id.slice(0, 8)}
                  </code>
                </div>
              ))}
            </div>
          )}
          <div className="mt-3 text-xs text-content-tertiary">
            <Link to="/ops/incidents" className="text-brand hover:text-brand-hover">Back to incidents</Link>
          </div>
        </Card>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="px-4 py-3 border-b border-border-default/60">
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary">Latest payload</div>
          <div className="text-xs text-content-secondary mt-1">Raw Alertmanager webhook payload stored in Postgres.</div>
        </div>
        <pre className="text-[12px] leading-[1.55] font-mono p-4 overflow-auto bg-surface-secondary">
          {latestPayloadPretty || '(empty payload)'}
        </pre>
      </Card>

      {/* Acknowledge modal */}
      <Modal
        open={ackOpen}
        onClose={() => { if (!ackLoading) setAckOpen(false); }}
        title="Acknowledge Incident"
        footer={
          <>
            <Button variant="secondary" onClick={() => setAckOpen(false)} disabled={ackLoading}>Cancel</Button>
            <Button onClick={acknowledge} loading={ackLoading}>Acknowledge</Button>
          </>
        }
      >
        <p className="text-sm text-content-secondary mb-4">
          Acknowledging is internal to RailPush. It does not stop Alertmanager from firing.
        </p>
        <Input
          label="Note (optional)"
          placeholder="What are you doing / who owns this?"
          value={ackNote}
          onChange={(e) => setAckNote(e.target.value)}
        />
      </Modal>

      {/* Silence modal */}
      <Modal
        open={silenceOpen}
        onClose={() => { if (!silenceLoading) setSilenceOpen(false); }}
        title="Silence In Alertmanager"
        footer={
          <>
            <Button variant="secondary" onClick={() => setSilenceOpen(false)} disabled={silenceLoading}>Cancel</Button>
            <Button onClick={silence} loading={silenceLoading}>Create Silence</Button>
          </>
        }
      >
        <div className="space-y-4">
          <p className="text-sm text-content-secondary">
            This creates a real Alertmanager silence. Choose a narrow scope unless you’re sure.
          </p>

          <div className="grid gap-3 md:grid-cols-2">
            <div className="space-y-1.5">
              <label className="block text-sm font-medium text-content-primary">Scope</label>
              <select
                className="app-input w-full bg-surface-secondary border border-border-default rounded-lg px-3 py-2.5 text-sm text-content-primary shadow-[0_8px_20px_rgba(15,23,42,0.05)] focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all"
                value={silenceScope}
                onChange={(e) => setSilenceScope(e.target.value as 'alertname' | 'group')}
              >
                <option value="alertname">This alertname (narrow)</option>
                <option value="group">This incident group (broader)</option>
              </select>
              <p className="text-xs text-content-tertiary">
                “Incident group” uses Alertmanager’s grouping labels and may silence more alerts.
              </p>
            </div>

            <Input
              label="Duration (minutes)"
              type="number"
              min={1}
              max={10080}
              value={silenceMinutes}
              onChange={(e) => setSilenceMinutes(Number(e.target.value))}
              hint="Default is 120 minutes. Max is 7 days."
            />
          </div>

          <div className="space-y-1.5">
            <label className="block text-sm font-medium text-content-primary">Comment (optional)</label>
            <textarea
              className={cn(
                'app-input w-full bg-surface-secondary border border-border-default rounded-lg px-3 py-2.5 text-sm text-content-primary shadow-[0_8px_20px_rgba(15,23,42,0.05)]',
                'placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all'
              )}
              rows={3}
              placeholder="Why are we silencing this?"
              value={silenceComment}
              onChange={(e) => setSilenceComment(e.target.value)}
            />
          </div>
        </div>
      </Modal>
    </div>
  );
}
