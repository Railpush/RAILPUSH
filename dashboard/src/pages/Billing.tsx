import { useState, useEffect } from 'react';
import { CreditCard, ExternalLink, Database, Key, Globe } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { billing } from '../lib/api';
import { PLAN_SPECS } from '../lib/plans';
import { toast } from 'sonner';
import type { BillingOverview } from '../types';

function resourceIcon(type: string) {
  switch (type) {
    case 'service': return <Globe className="w-4 h-4" />;
    case 'database': return <Database className="w-4 h-4" />;
    case 'keyvalue': return <Key className="w-4 h-4" />;
    default: return <Globe className="w-4 h-4" />;
  }
}

function resourceLabel(type: string) {
  switch (type) {
    case 'service': return 'Service';
    case 'database': return 'PostgreSQL';
    case 'keyvalue': return 'Key Value';
    default: return type;
  }
}

function brandDisplay(brand: string) {
  const brands: Record<string, string> = {
    visa: 'Visa',
    mastercard: 'Mastercard',
    amex: 'Amex',
    discover: 'Discover',
  };
  return brands[brand.toLowerCase()] || brand;
}

export function Billing() {
  const [overview, setOverview] = useState<BillingOverview | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    billing.getOverview()
      .then(setOverview)
      .catch(() => toast.error('Failed to load billing info'))
      .finally(() => setLoading(false));
  }, []);

  const handleAddPayment = async () => {
    try {
      const { url } = await billing.createCheckoutSession(window.location.origin + '/billing');
      window.location.href = url;
    } catch {
      toast.error('Failed to start checkout');
    }
  };

  const handleManageBilling = async () => {
    try {
      const { url } = await billing.createPortalSession(window.location.origin + '/billing');
      window.location.href = url;
    } catch {
      toast.error('Failed to open billing portal');
    }
  };

  if (loading) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Billing</h1>
        <div className="space-y-4">
          {[1, 2].map((i) => (
            <div key={i} className="h-32 rounded-lg bg-surface-secondary animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  const monthlyTotal = overview?.monthly_total ? overview.monthly_total / 100 : 0;
  const paidItems = overview?.items?.filter(i => i.monthly_cost > 0) || [];

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Billing</h1>
        {overview?.has_payment_method && (
          <Button variant="secondary" onClick={handleManageBilling}>
            <ExternalLink className="w-4 h-4 mr-1.5" />
            Manage Billing
          </Button>
        )}
      </div>

      {/* Payment Method Card */}
      <Card padding="lg" className="mb-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-brand/10 flex items-center justify-center">
              <CreditCard className="w-5 h-5 text-brand" />
            </div>
            <div>
              <div className="text-sm font-medium text-content-primary">Payment Method</div>
              {overview?.has_payment_method ? (
                <div className="text-sm text-content-secondary">
                  {brandDisplay(overview.payment_method_brand)} ending in {overview.payment_method_last4}
                </div>
              ) : (
                <div className="text-sm text-content-tertiary">No payment method on file</div>
              )}
            </div>
          </div>
          <Button
            variant={overview?.has_payment_method ? 'secondary' : 'primary'}
            size="sm"
            onClick={handleAddPayment}
          >
            {overview?.has_payment_method ? 'Update' : 'Add Payment Method'}
          </Button>
        </div>
      </Card>

      {/* Monthly Summary */}
      <Card padding="lg" className="mb-6">
        <div className="flex items-center justify-between mb-4">
          <div className="text-sm font-medium text-content-primary">Monthly Summary</div>
          <div className="text-lg font-semibold text-content-primary">${monthlyTotal.toFixed(2)}/mo</div>
        </div>

        {paidItems.length > 0 ? (
          <div className="border-t border-border-default pt-3">
            <table className="w-full">
              <thead>
                <tr className="text-xs text-content-tertiary uppercase tracking-wider">
                  <th className="text-left pb-2 font-medium">Resource</th>
                  <th className="text-left pb-2 font-medium">Type</th>
                  <th className="text-left pb-2 font-medium">Plan</th>
                  <th className="text-right pb-2 font-medium">Cost</th>
                </tr>
              </thead>
              <tbody>
                {paidItems.map((item) => (
                  <tr key={`${item.resource_type}-${item.resource_id}`} className="border-t border-border-subtle">
                    <td className="py-2.5 text-sm text-content-primary">
                      <div className="flex items-center gap-2">
                        {resourceIcon(item.resource_type)}
                        {item.resource_name}
                      </div>
                    </td>
                    <td className="py-2.5 text-sm text-content-secondary">{resourceLabel(item.resource_type)}</td>
                    <td className="py-2.5">
                      <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-surface-tertiary text-content-secondary capitalize">
                        {item.plan}
                      </span>
                    </td>
                    <td className="py-2.5 text-sm text-content-primary text-right">${(item.monthly_cost / 100).toFixed(2)}/mo</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-sm text-content-tertiary text-center py-6">
            No paid resources. Create a resource with a paid plan to start billing.
          </div>
        )}
      </Card>

      {/* Plan Tiers Reference */}
      <div className="mb-6">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Available Plans</h2>
        <div className="grid grid-cols-4 gap-3">
          {PLAN_SPECS.map((plan) => (
            <Card key={plan.name} className="text-center py-4">
              <div className="text-sm font-semibold text-content-primary">{plan.name}</div>
              <div className="text-xs text-content-secondary mt-1">{plan.cpu}, {plan.mem}</div>
              <div className="text-sm font-medium text-brand mt-2">{plan.priceLabel}</div>
            </Card>
          ))}
        </div>
      </div>
    </div>
  );
}
