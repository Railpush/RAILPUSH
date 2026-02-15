import { useEffect, useMemo, useState } from 'react';
import { CheckCircle2, GitBranch, Globe, Mail, ShieldAlert, XCircle, Zap } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn } from '../lib/utils';
import { toast } from 'sonner';

type SettingsPayload = Record<string, unknown> & {
  control_plane_domain?: string;
  deploy_domain?: string;
  kube_enabled?: boolean;
  email?: {
    enabled?: boolean;
    provider?: string;
    from?: string;
    reply_to?: string;
    smtp?: { host?: string; port?: number };
    outbox?: {
      poll_interval_ms?: number;
      batch_size?: number;
      lease_seconds?: number;
      max_attempts?: number;
    };
  };
  github?: {
    webhook_secret_configured?: boolean;
    callback_url?: string;
  };
  alerts?: {
    alert_webhook_configured?: boolean;
    alertmanager_url?: string;
  };
};

function BoolPill({ ok, yes, no }: { ok: boolean; yes: string; no: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 text-[11px] px-2 py-1 rounded-full border',
        ok ? 'border-status-success/30 bg-status-success/10 text-status-success' : 'border-status-error/30 bg-status-error/10 text-status-error'
      )}
    >
      {ok ? <CheckCircle2 className="w-3.5 h-3.5" /> : <XCircle className="w-3.5 h-3.5" />}
      {ok ? yes : no}
    </span>
  );
}

export function OpsSettingsPage() {
  const [data, setData] = useState<SettingsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [enableConfirm, setEnableConfirm] = useState('');
  const [enableBusy, setEnableBusy] = useState(false);
  const [enableUpdated, setEnableUpdated] = useState<number | null>(null);
  const [testTo, setTestTo] = useState('');
  const [testBusy, setTestBusy] = useState(false);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getSettings()
      .then((payload) => setData(payload as SettingsPayload))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load ops settings');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const emailEnabled = Boolean(data?.email?.enabled);
  const githubWebhookConfigured = Boolean(data?.github?.webhook_secret_configured);
  const alertWebhookConfigured = Boolean(data?.alerts?.alert_webhook_configured);

  const pretty = useMemo(() => JSON.stringify(data || {}, null, 2), [data]);

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
        <h1 className="text-3xl font-semibold text-content-primary mt-1">Settings</h1>
        <p className="text-sm text-content-secondary mt-1">Platform config visibility (no secrets).</p>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">Forbidden.</Card>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : loading || !data ? (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Card key={i} className="p-5">
              <Skeleton className="w-32 h-3" />
              <Skeleton className="w-48 h-4 mt-4" />
              <Skeleton className="w-40 h-4 mt-2" />
            </Card>
          ))}
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            <Card className="p-5">
              <div className="flex items-center gap-2 text-sm font-semibold text-content-primary">
                <Globe className="w-4 h-4 text-brand" />
                Domains
              </div>
              <div className="mt-3 space-y-2 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Control plane</div>
                  <div className="font-mono text-xs text-content-secondary truncate">{data.control_plane_domain || '-'}</div>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Deploy domain</div>
                  <div className="font-mono text-xs text-content-secondary truncate">{data.deploy_domain || '-'}</div>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Kubernetes</div>
                  <BoolPill ok={Boolean(data.kube_enabled)} yes="Enabled" no="Disabled" />
                </div>
              </div>
            </Card>

            <Card className="p-5">
              <div className="flex items-center gap-2 text-sm font-semibold text-content-primary">
                <Mail className="w-4 h-4 text-brand" />
                Email
              </div>
              <div className="mt-3 space-y-2 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Status</div>
                  <BoolPill ok={emailEnabled} yes="Enabled" no="Disabled" />
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Provider</div>
                  <div className="font-mono text-xs text-content-secondary">{data.email?.provider || '-'}</div>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">From</div>
                  <div className="font-mono text-xs text-content-secondary truncate">{data.email?.from || '-'}</div>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">SMTP</div>
                  <div className="font-mono text-xs text-content-secondary truncate">
                    {(data.email?.smtp?.host || '-') + ':' + String(data.email?.smtp?.port || '-')}
                  </div>
                </div>
              </div>
            </Card>

            <Card className="p-5">
              <div className="flex items-center gap-2 text-sm font-semibold text-content-primary">
                <GitBranch className="w-4 h-4 text-brand" />
                GitHub
              </div>
              <div className="mt-3 space-y-2 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Webhook secret</div>
                  <BoolPill ok={githubWebhookConfigured} yes="Configured" no="Missing" />
                </div>
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Callback URL</div>
                  <div className="font-mono text-xs text-content-secondary truncate">{data.github?.callback_url || '-'}</div>
                </div>
              </div>
            </Card>

            <Card className="p-5 lg:col-span-3">
              <div className="flex items-center gap-2 text-sm font-semibold text-content-primary">
                <ShieldAlert className="w-4 h-4 text-brand" />
                Alerts
              </div>
              <div className="mt-3 grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">RailPush receiver token</div>
                  <BoolPill ok={alertWebhookConfigured} yes="Configured" no="Missing" />
                </div>
                <div className="md:col-span-2 flex items-center justify-between gap-3">
                  <div className="text-content-tertiary">Alertmanager URL</div>
                  <div className="font-mono text-xs text-content-secondary truncate">{data.alerts?.alertmanager_url || '-'}</div>
                </div>
              </div>
            </Card>
          </div>

          <Card className="p-5">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="flex items-center gap-2 text-sm font-semibold text-content-primary">
                  <Zap className="w-4 h-4 text-brand" />
                  Ops Actions
                </div>
                <div className="text-xs text-content-tertiary mt-1">
                  One-off platform actions. Use with care.
                </div>
              </div>
              {enableUpdated !== null && (
                <div className="text-xs text-content-tertiary">
                  Updated: <span className="font-mono">{enableUpdated}</span>
                </div>
              )}
            </div>

            <div className="mt-4 grid grid-cols-1 md:grid-cols-3 gap-3 items-end">
              <label className="block md:col-span-2">
                <div className="text-xs text-content-tertiary mb-1.5">Enable auto-deploy for all services</div>
                <input
                  value={enableConfirm}
                  onChange={(e) => setEnableConfirm(e.target.value)}
                  placeholder='Type "ENABLE" to confirm'
                  className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
              <div>
                <Button
                  variant="secondary"
                  className="w-full h-10"
                  loading={enableBusy}
                  disabled={enableConfirm.trim().toUpperCase() !== 'ENABLE'}
                  onClick={async () => {
                    setEnableBusy(true);
                    try {
                      const res = await ops.enableAutoDeployAll(enableConfirm);
                      setEnableUpdated(res.updated);
                      toast.success(`Auto-deploy enabled for ${res.updated} services`);
                    } catch (e: unknown) {
                      toast.error(e instanceof Error ? e.message : 'Failed to enable auto-deploy');
                    } finally {
                      setEnableBusy(false);
                    }
                  }}
                >
                  Run
                </Button>
              </div>
            </div>

            <div className="mt-4 grid grid-cols-1 md:grid-cols-3 gap-3 items-end">
              <label className="block md:col-span-2">
                <div className="text-xs text-content-tertiary mb-1.5">Send test email (transactional outbox)</div>
                <input
                  value={testTo}
                  onChange={(e) => setTestTo(e.target.value)}
                  placeholder="you@example.com"
                  className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
              <div>
                <Button
                  variant="secondary"
                  className="w-full h-10"
                  loading={testBusy}
                  disabled={!testTo.trim()}
                  onClick={async () => {
                    setTestBusy(true);
                    try {
                      await ops.sendTestEmail(testTo.trim());
                      toast.success('Test email enqueued (check Ops -> Email outbox)');
                    } catch (e: unknown) {
                      toast.error(e instanceof Error ? e.message : 'Failed to send test email');
                    } finally {
                      setTestBusy(false);
                    }
                  }}
                >
                  Send
                </Button>
              </div>
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">
              Raw payload
            </div>
            <pre className="p-4 text-xs overflow-auto bg-surface-secondary font-mono text-content-secondary">{pretty}</pre>
          </Card>
        </>
      )}
    </div>
  );
}
