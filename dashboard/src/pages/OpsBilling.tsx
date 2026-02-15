import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { CreditCard, RefreshCcw, Search } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { cn, truncate } from '../lib/utils';
import type { OpsBillingCustomerItem } from '../types';

type StatusFilter = 'all' | 'active' | 'trialing' | 'past_due' | 'incomplete' | 'canceled';

export function OpsBillingPage() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<StatusFilter>('all');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<OpsBillingCustomerItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const q = useMemo(() => query.trim(), [query]);

  const load = () => {
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .listBillingCustomers({ status: status === 'all' ? '' : status, query: q, limit: 200 })
      .then(setRows)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load billing customers');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, q]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
          <h1 className="text-2xl font-semibold text-content-primary mt-1 flex items-center gap-2">
            <CreditCard className="w-5 h-5 text-content-tertiary" />
            Billing
          </h1>
          <p className="text-sm text-content-secondary mt-1">Stripe customers and subscription inventory (database-backed).</p>
        </div>
        <Button variant="secondary" onClick={load} loading={loading}>
          <RefreshCcw className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      <Card className="p-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-border-default/60">
          <div className="flex items-center gap-2 text-sm">
            {([
              { key: 'all', label: 'All' },
              { key: 'active', label: 'Active' },
              { key: 'trialing', label: 'Trialing' },
              { key: 'past_due', label: 'Past Due' },
              { key: 'incomplete', label: 'Incomplete' },
              { key: 'canceled', label: 'Canceled' },
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
          </div>

          <div className="relative w-full md:w-[360px]">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
            <input
              type="text"
              placeholder="Search email, username, Stripe ID..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="app-input w-full bg-surface-secondary border border-border-default rounded-md pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/12 transition-all"
            />
          </div>
        </div>

        {forbidden ? (
          <div className="p-8 text-center text-sm text-content-secondary">Forbidden.</div>
        ) : loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 border border-border-subtle rounded-md p-3">
                <Skeleton className="w-52 h-4" />
                <Skeleton className="w-24 h-4" />
                <div className="flex-1" />
                <Skeleton className="w-20 h-4" />
              </div>
            ))}
          </div>
        ) : error ? (
          <div className="p-8 text-center text-sm text-status-error">{error}</div>
        ) : rows.length === 0 ? (
          <div className="p-10 text-center text-sm text-content-tertiary">No billing customers.</div>
        ) : (
          <div className="overflow-hidden">
            <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
              <div className="col-span-4">Customer</div>
              <div className="col-span-2">Status</div>
              <div className="col-span-2">Payment</div>
              <div className="col-span-2">Items</div>
              <div className="col-span-2 text-right">Stripe</div>
            </div>

            {rows.map((c) => (
              <button
                key={c.id}
                onClick={() => navigate(`/ops/billing/${encodeURIComponent(c.id)}`)}
                className="w-full text-left grid grid-cols-12 px-4 py-3 border-b border-border-subtle hover:bg-surface-tertiary/40 transition-colors"
              >
                <div className="col-span-4 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{c.email || c.username || 'Customer'}</div>
                  <div className="text-xs text-content-tertiary truncate">{truncate(c.user_id, 10)} · {truncate(c.id, 10)}</div>
                </div>
                <div className="col-span-2 text-sm text-content-secondary">
                  {c.subscription_status || 'unknown'}
                </div>
                <div className="col-span-2 text-sm text-content-secondary">
                  {c.payment_method_brand && c.payment_method_last4 ? `${c.payment_method_brand} •••• ${c.payment_method_last4}` : '—'}
                </div>
                <div className="col-span-2 text-sm text-content-secondary">
                  {c.items_count}
                </div>
                <div className="col-span-2 text-right text-xs text-content-tertiary font-mono truncate">
                  {truncate(c.stripe_customer_id || '', 18) || '—'}
                </div>
              </button>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

