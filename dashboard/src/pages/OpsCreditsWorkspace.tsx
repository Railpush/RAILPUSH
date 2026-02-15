import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Coins, PlusCircle, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import type { OpsWorkspaceCreditDetail } from '../types';

export function OpsCreditsWorkspacePage() {
  const navigate = useNavigate();
  const { workspaceId } = useParams<{ workspaceId: string }>();
  const [data, setData] = useState<OpsWorkspaceCreditDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [amount, setAmount] = useState('25');
  const [reason, setReason] = useState('Promo credit');
  const [granting, setGranting] = useState(false);

  const load = () => {
    if (!workspaceId) return;
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getCreditsWorkspace(workspaceId)
      .then(setData)
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
  }, [workspaceId]);

  const grant = async () => {
    if (!workspaceId) return;
    const dollars = Number(amount);
    if (!Number.isFinite(dollars) || dollars === 0) return;
    const cents = Math.round(dollars * 100);
    setGranting(true);
    try {
      await ops.grantCredits(workspaceId, { amount_cents: cents, reason });
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to grant credit');
    } finally {
      setGranting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <button
            onClick={() => navigate('/ops/credits')}
            className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
            Back to Credits
          </button>
          <h1 className="text-2xl font-semibold text-content-primary mt-2 flex items-center gap-2">
            <Coins className="w-5 h-5 text-content-tertiary" />
            Workspace Credits
          </h1>
          <p className="text-sm text-content-secondary mt-1">Ledger + balance.</p>
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
      ) : !data ? (
        <Card className="p-8 text-center text-sm text-content-tertiary">Not found.</Card>
      ) : (
        <>
          <Card className="p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Workspace</div>
                <div className="text-sm font-semibold text-content-primary mt-2">{data.workspace_name || data.workspace_id}</div>
                <div className="text-xs text-content-tertiary font-mono mt-1">{data.workspace_id}</div>
              </div>
              <div className="text-right">
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Balance</div>
                <div className="text-2xl font-semibold text-content-primary mt-2 tabular-nums">
                  ${(data.balance_cents / 100).toFixed(2)}
                </div>
              </div>
            </div>
          </Card>

          <Card className="p-6">
            <div className="text-sm font-semibold text-content-primary mb-3">Grant / Adjust</div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <label className="block">
                <div className="text-xs text-content-tertiary mb-1.5">Amount (USD)</div>
                <input
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  type="number"
                  step="0.01"
                  className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
              <label className="block md:col-span-2">
                <div className="text-xs text-content-tertiary mb-1.5">Reason</div>
                <input
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  className="w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-sm"
                />
              </label>
            </div>
            <div className="mt-3">
              <Button onClick={grant} loading={granting} disabled={!amount || Number(amount) === 0}>
                <PlusCircle className="w-4 h-4" />
                Apply
              </Button>
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">Ledger</div>
            {data.ledger.length === 0 ? (
              <div className="p-8 text-center text-sm text-content-tertiary">No credit entries.</div>
            ) : (
              <div className="overflow-hidden">
                <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                  <div className="col-span-2 text-right">Amount</div>
                  <div className="col-span-6">Reason</div>
                  <div className="col-span-4">Created</div>
                </div>
                {data.ledger.map((e) => (
                  <div key={e.id} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                    <div className="col-span-2 text-right text-sm text-content-secondary tabular-nums">
                      ${(e.amount_cents / 100).toFixed(2)}
                    </div>
                    <div className="col-span-6 text-sm text-content-secondary truncate">{e.reason || '—'}</div>
                    <div className="col-span-4 text-xs text-content-tertiary">
                      {new Date(e.created_at).toLocaleString()}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

