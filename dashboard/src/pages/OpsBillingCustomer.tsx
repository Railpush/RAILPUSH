import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, CreditCard, ExternalLink, RefreshCcw } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import { truncate } from '../lib/utils';
import type { OpsBillingCustomerDetail } from '../types';

export function OpsBillingCustomerPage() {
  const navigate = useNavigate();
  const { customerId } = useParams<{ customerId: string }>();
  const [data, setData] = useState<OpsBillingCustomerDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    if (!customerId) return;
    setLoading(true);
    setForbidden(false);
    setError(null);
    ops
      .getBillingCustomer(customerId)
      .then(setData)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load billing customer');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [customerId]);

  const c = data?.customer;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <button
            onClick={() => navigate('/ops/billing')}
            className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
            Back to Billing
          </button>
          <h1 className="text-2xl font-semibold text-content-primary mt-2 flex items-center gap-2">
            <CreditCard className="w-5 h-5 text-content-tertiary" />
            Billing Customer
          </h1>
          <p className="text-sm text-content-secondary mt-1">Subscription + billing items.</p>
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
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : !data || !c ? (
        <Card className="p-8 text-center text-sm text-content-tertiary">Not found.</Card>
      ) : (
        <>
          <Card className="p-6">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Customer</div>
                <div className="text-sm font-semibold text-content-primary mt-2">{c.email || c.username || 'Customer'}</div>
                <div className="text-xs text-content-tertiary mt-1 font-mono">
                  user {truncate(c.user_id, 14)} · bc {truncate(c.id, 14)}
                </div>
              </div>

              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Stripe</div>
                <div className="text-sm text-content-secondary mt-2 font-mono truncate">{c.stripe_customer_id || '—'}</div>
                {c.stripe_customer_id && (
                  <a
                    href={`https://dashboard.stripe.com/customers/${c.stripe_customer_id}`}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 text-xs text-brand hover:text-brand-hover mt-1"
                  >
                    Open in Stripe <ExternalLink className="w-3.5 h-3.5" />
                  </a>
                )}
              </div>

              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Status</div>
                <div className="text-sm text-content-secondary mt-2">{c.subscription_status || 'unknown'}</div>
                <div className="text-xs text-content-tertiary mt-1 font-mono truncate">{c.stripe_subscription_id || '—'}</div>
              </div>

              <div>
                <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Payment</div>
                <div className="text-sm text-content-secondary mt-2">
                  {c.payment_method_brand && c.payment_method_last4 ? `${c.payment_method_brand} •••• ${c.payment_method_last4}` : '—'}
                </div>
                <div className="text-xs text-content-tertiary mt-1">Items: {c.items_count}</div>
              </div>
            </div>
          </Card>

          <Card className="p-0 overflow-hidden">
            <div className="px-4 py-3 border-b border-border-default/60 text-sm font-semibold text-content-primary">
              Billing Items
            </div>
            {data.items.length === 0 ? (
              <div className="p-8 text-center text-sm text-content-tertiary">No billing items.</div>
            ) : (
              <div className="overflow-hidden">
                <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
                  <div className="col-span-3">Resource</div>
                  <div className="col-span-3">Name</div>
                  <div className="col-span-2">Plan</div>
                  <div className="col-span-4">Stripe Item</div>
                </div>
                {data.items.map((it) => (
                  <div key={it.id} className="grid grid-cols-12 px-4 py-3 border-b border-border-subtle">
                    <div className="col-span-3 text-sm text-content-secondary">{it.resource_type}</div>
                    <div className="col-span-3 text-sm text-content-secondary truncate">{it.resource_name || truncate(it.resource_id, 10)}</div>
                    <div className="col-span-2 text-sm text-content-secondary">{it.plan}</div>
                    <div className="col-span-4 text-xs text-content-tertiary font-mono truncate">{it.stripe_subscription_item_id}</div>
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

