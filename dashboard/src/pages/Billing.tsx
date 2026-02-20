import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ChevronDown,
  ChevronRight,
  CircleDollarSign,
  CreditCard,
  Database,
  ExternalLink,
  AlertTriangle,
  FileDown,
  Globe,
  Key,
  PencilLine,
  ReceiptText,
  Wallet,
} from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/StatusBadge';
import { auth, billing } from '../lib/api';
import { PLAN_BY_ID, type PlanID } from '../lib/plans';
import { cn } from '../lib/utils';
import { toast } from 'sonner';
import type { BillingLineItem, BillingOverview } from '../types';

type UsageEnvelope = {
  instanceHours: number;
  pipelineMinutes: number;
  bandwidthGb: number;
};

const planRank: Record<PlanID, number> = {
  free: 0,
  starter: 1,
  standard: 2,
  pro: 3,
};

const includedByPlan: Record<PlanID, UsageEnvelope> = {
  free: { instanceHours: 750, pipelineMinutes: 500, bandwidthGb: 100 },
  starter: { instanceHours: 1500, pipelineMinutes: 2000, bandwidthGb: 1024 },
  standard: { instanceHours: 3000, pipelineMinutes: 5000, bandwidthGb: 2048 },
  pro: { instanceHours: 6000, pipelineMinutes: 12000, bandwidthGb: 4096 },
};

function normalizePlan(plan: string): PlanID {
  const normalized = plan.trim().toLowerCase();
  if (normalized === 'starter' || normalized === 'standard' || normalized === 'pro' || normalized === 'free') {
    return normalized;
  }
  return 'free';
}

function brandDisplay(brand: string) {
  const brands: Record<string, string> = {
    visa: 'Visa',
    mastercard: 'Mastercard',
    amex: 'Amex',
    discover: 'Discover',
  };
  if (!brand) return 'Card';
  return brands[brand.toLowerCase()] || brand;
}

function VisaLogo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 780 500" className={className} fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="780" height="500" rx="40" fill="#1A1F71" />
      <path d="M293.2 348.7l33.4-195.8h53.4l-33.4 195.8h-53.4zm221.5-191c-10.6-4-27.2-8.3-47.9-8.3-52.8 0-90 26.6-90.2 64.6-.3 28.1 26.5 43.8 46.7 53.2 20.8 9.6 27.8 15.7 27.7 24.3-.1 13.1-16.6 19.1-32 19.1-21.4 0-32.7-3-50.3-10.2l-6.9-3.1-7.5 43.8c12.5 5.5 35.6 10.2 59.6 10.5 56.2 0 92.6-26.3 93-66.8.2-22.3-14-39.2-44.8-53.2-18.6-9.1-30.1-15.1-30-24.3 0-8.1 9.7-16.8 30.6-16.8 17.5-.3 30.1 3.5 40 7.5l4.8 2.3 7.2-42.7zm138.3-4.8h-41.3c-12.8 0-22.4 3.5-28 16.3l-79.4 179.5h56.2s9.2-24.1 11.2-29.4h68.6c1.6 6.9 6.5 29.4 6.5 29.4h49.7l-43.5-195.8zm-65.8 126.4c4.4-11.3 21.4-54.8 21.4-54.8-.3.5 4.4-11.4 7.1-18.8l3.6 17s10.3 47 12.5 56.6h-44.6zM285 152.9l-52.4 133.6-5.6-27.1c-9.7-31.3-40-65.2-73.9-82.2l47.9 171.3 56.6-.1 84.2-195.5H285z" fill="white" />
      <path d="M146.9 152.9H60.1l-.7 3.8c67.1 16.2 111.5 55.3 129.9 102.3l-18.7-90.2c-3.2-12.4-12.8-15.5-23.7-15.9z" fill="#F9A51A" />
    </svg>
  );
}

function MastercardLogo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 780 500" className={className} fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="780" height="500" rx="40" fill="#252525" />
      <circle cx="312" cy="250" r="150" fill="#EB001B" />
      <circle cx="468" cy="250" r="150" fill="#F79E1B" />
      <path d="M390 130.7c-37.1 29.3-60.9 74.5-60.9 125.3s23.8 96 60.9 125.3c37.1-29.3 60.9-74.5 60.9-125.3s-23.8-96-60.9-125.3z" fill="#FF5F00" />
    </svg>
  );
}

function CardBrandMark({
  brand,
  className,
}: {
  brand?: string | null;
  className?: string;
}) {
  const key = (brand || '').trim().toLowerCase();

  if (key === 'visa') {
    return (
      <div
        className={cn('inline-flex items-center justify-center rounded-md overflow-hidden bg-white', className)}
        aria-label="Visa"
      >
        <VisaLogo className="h-6 w-auto" />
      </div>
    );
  }

  if (key === 'mastercard') {
    return (
      <div
        className={cn('inline-flex items-center justify-center rounded-md overflow-hidden bg-white', className)}
        aria-label="Mastercard"
      >
        <MastercardLogo className="h-6 w-auto" />
      </div>
    );
  }

  return (
    <div
      className={cn(
        'inline-flex items-center justify-center rounded-md border border-border-default bg-surface-secondary text-content-primary',
        className
      )}
      aria-label="Card"
    >
      <CreditCard className="w-5 h-5 opacity-70" />
    </div>
  );
}

function resourceLabel(type: string) {
  switch (type) {
    case 'service': return 'Services';
    case 'database': return 'PostgreSQL';
    case 'keyvalue': return 'Key Value';
    default: return 'Resources';
  }
}

function resourceIcon(type: string) {
  switch (type) {
    case 'service': return <Globe className="w-4 h-4" />;
    case 'database': return <Database className="w-4 h-4" />;
    case 'keyvalue': return <Key className="w-4 h-4" />;
    default: return <CircleDollarSign className="w-4 h-4" />;
  }
}

function formatCurrency(cents: number) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);
}

function formatCreditReason(reason: string | null | undefined): string {
  if (!reason) return 'Credit adjustment';
  const r = reason.trim();
  // Strip raw UUIDs and internal billing prefixes to make it user-friendly.
  // "billing: service menthor-api (uuid) plan=starter" -> "Service: menthor-api (Starter)"
  const billingMatch = r.match(/^billing:\s*(service|database|keyvalue)\s+(\S+)\s*\([^)]*\)\s*plan=(\S+)/i);
  if (billingMatch) {
    const kind = billingMatch[1].toLowerCase() === 'keyvalue' ? 'Key Value' : billingMatch[1].charAt(0).toUpperCase() + billingMatch[1].slice(1);
    const name = billingMatch[2];
    const plan = billingMatch[3].charAt(0).toUpperCase() + billingMatch[3].slice(1);
    return `${kind}: ${name} (${plan})`;
  }
  // Strip any leftover UUIDs: (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)
  const cleaned = r.replace(/\s*\([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\)/gi, '');
  return cleaned || 'Credit adjustment';
}

function usagePercent(used: number, included: number) {
  if (included <= 0) return 0;
  return Math.min(100, Math.round((used / included) * 100));
}

function StatCard({
  title,
  value,
  helper,
  icon,
}: {
  title: string;
  value: string;
  helper?: string | React.ReactNode;
  icon?: React.ReactNode;
}) {
  return (
    <div className="glass-panel p-5 rounded-xl relative overflow-hidden group hover:border-border-hover transition-colors">
      <div className="flex items-start gap-3">
        {icon && <div className="w-10 h-10 rounded-lg bg-surface-tertiary border border-border-default flex items-center justify-center text-brand/80">{icon}</div>}
        <div className="flex-1">
          <p className="text-xs uppercase tracking-wider text-content-tertiary font-semibold">{title}</p>
          <p className="text-2xl font-bold text-content-primary mt-1 tracking-tight">{value}</p>
          {helper && <div className="mt-2 text-xs leading-snug text-content-secondary">{helper}</div>}
        </div>
      </div>
    </div>
  );
}

function StatusPill({ status }: { status?: string }) {
  const normalized = (status || 'active').trim().toLowerCase();
  let displayStatus: 'live' | 'suspended' | 'created' = 'created';

  if (normalized === 'active' || normalized === 'trialing') displayStatus = 'live';
  else if (normalized === 'unpaid' || normalized === 'canceled') displayStatus = 'suspended';

  return <StatusBadge status={displayStatus} size="sm" />;
}

function SectionFrame({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section id={id} className="scroll-mt-24">
      <div className="mb-4 flex items-center gap-2">
        <div className="h-4 w-1 bg-brand rounded-full" />
        <h2 className="text-lg font-semibold text-content-primary">{title}</h2>
      </div>
      <Card className="glass-panel p-6 overflow-hidden">
        {children}
      </Card>
    </section>
  );
}

export function Billing() {
  const navigate = useNavigate();
  const [overview, setOverview] = useState<BillingOverview | null>(null);
  const [billingEmail, setBillingEmail] = useState('');
  const [loading, setLoading] = useState(true);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string> | null>(null);

  useEffect(() => {
    let mounted = true;

    async function load() {
      try {
        const [billingOverview, userInfo] = await Promise.all([
          billing.getOverview(),
          auth.getUser().catch(() => null),
        ]);
        if (!mounted) return;
        setOverview(billingOverview);
        setBillingEmail(userInfo?.user?.email ?? '');
      } catch {
        if (mounted) toast.error('Failed to load billing information');
      } finally {
        if (mounted) setLoading(false);
      }
    }

    load();
    return () => { mounted = false; };
  }, []);

  const items = overview?.items ?? [];
  const paidItems = items.filter((item) => item.monthly_cost > 0);
  const stripeLinkedItems = paidItems.filter((item) => item.stripe_linked !== false);
  const unsyncedItems = paidItems.filter((item) => item.stripe_linked === false);
  const monthToDateCents = overview?.monthly_total ?? stripeLinkedItems.reduce((sum, item) => sum + item.monthly_cost, 0);
  const creditBalanceCents = Math.max(0, overview?.credit_balance_cents ?? 0);

  const nextInvoiceTotalCents = Math.max(0, overview?.next_invoice_total_cents ?? monthToDateCents);
  const nextInvoiceCreditAppliedCents = Math.max(
    0,
    overview?.next_invoice_credit_applied_cents ?? Math.min(creditBalanceCents, nextInvoiceTotalCents),
  );
  const nextInvoiceAmountDueCents = Math.max(
    0,
    overview?.next_invoice_amount_due_cents ?? (nextInvoiceTotalCents - nextInvoiceCreditAppliedCents),
  );
  const nextInvoiceCreditCarryCents = Math.max(
    0,
    overview?.next_invoice_credit_carry_cents ?? (creditBalanceCents - nextInvoiceCreditAppliedCents),
  );
  const nextChargeAt = overview?.next_charge_at ? new Date(overview.next_charge_at) : null;
  const nextChargeAtLabel = nextChargeAt
    ? nextChargeAt.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    : null;

  const currentPlanId = useMemo<PlanID>(() => {
    const fromApi = overview?.current_plan ? normalizePlan(overview.current_plan) : null;
    if (fromApi) return fromApi;

    if (paidItems.length === 0) return 'free';
    let best: PlanID = 'free';
    paidItems.forEach((item) => {
      const normalized = normalizePlan(item.plan);
      if (planRank[normalized] > planRank[best]) {
        best = normalized;
      }
    });
    return best;
  }, [overview?.current_plan, paidItems]);

  const currentPlan = PLAN_BY_ID[currentPlanId];
  const includedUsage = includedByPlan[currentPlanId];

  const groupedCharges = useMemo(() => {
    const grouped = new Map<string, { label: string; total: number; items: BillingLineItem[] }>();
    stripeLinkedItems.forEach((item) => {
      const key = item.resource_type;
      const existing = grouped.get(key);
      if (existing) {
        existing.total += item.monthly_cost;
        existing.items.push(item);
        return;
      }
      grouped.set(key, {
        label: resourceLabel(key),
        total: item.monthly_cost,
        items: [item],
      });
    });
    return Array.from(grouped.entries())
      .map(([key, value]) => ({ key, ...value }))
      .sort((a, b) => b.total - a.total);
  }, [stripeLinkedItems]);

  const allGroupKeys = useMemo(() => groupedCharges.map((group) => group.key), [groupedCharges]);
  const allGroupKeysSignature = useMemo(() => allGroupKeys.join('|'), [allGroupKeys]);
  const effectiveCollapsedGroups = useMemo(
    () => collapsedGroups ?? new Set(allGroupKeys),
    [collapsedGroups, allGroupKeysSignature],
  );

  useEffect(() => {
    // Collapse all groups by default once they load, and prune removed keys if the list changes.
    setCollapsedGroups((prev) => {
      if (prev === null) {
        if (allGroupKeys.length === 0) return prev;
        return new Set(allGroupKeys);
      }
      if (prev.size === 0) return prev;

      const valid = new Set(allGroupKeys);
      let changed = false;
      const next = new Set<string>();
      prev.forEach((key) => {
        if (valid.has(key)) next.add(key);
        else changed = true;
      });
      if (!changed) return prev;
      return next;
    });
  }, [allGroupKeysSignature]);

  const handleAddPaymentMethod = async () => {
    try {
      const { url } = await billing.createCheckoutSession(`${window.location.origin}/billing`);
      window.location.href = url;
    } catch {
      toast.error('Failed to open checkout');
    }
  };

  const handleOpenBillingPortal = async () => {
    try {
      const { url } = await billing.createPortalSession(`${window.location.origin}/billing`);
      window.location.href = url;
    } catch {
      toast.error('Failed to open billing portal');
    }
  };

  const handleDownloadCsv = () => {
    const invoices = overview?.invoices ?? [];
    if (stripeLinkedItems.length === 0 && invoices.length === 0) {
      toast.info('No billing data available to export');
      return;
    }

    const sections: string[][] = [];

    // Unbilled charges section
    if (stripeLinkedItems.length > 0) {
      sections.push(['--- Unbilled Charges ---', '', '', '', '']);
      sections.push(['resource_type', 'resource_name', 'resource_id', 'plan', 'monthly_cost_usd']);
      stripeLinkedItems.forEach((item) => {
        sections.push([
          item.resource_type,
          item.resource_name,
          item.resource_id,
          item.plan,
          (item.monthly_cost / 100).toFixed(2),
        ]);
      });
    }

    // Invoice history section
    if (invoices.length > 0) {
      if (sections.length > 0) sections.push(['', '', '', '', '']);
      sections.push(['--- Invoice History ---', '', '', '', '']);
      sections.push(['date', 'status', 'amount_due_usd', 'amount_paid_usd', 'stripe_invoice_id']);
      invoices.forEach((inv) => {
        sections.push([
          new Date(inv.created_at).toISOString().slice(0, 10),
          inv.status,
          (inv.amount_due_cents / 100).toFixed(2),
          (inv.amount_paid_cents / 100).toFixed(2),
          inv.stripe_invoice_id,
        ]);
      });
    }

    const csv = sections
      .map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\n');

    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `railpush-billing-${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  const activeServiceCount = paidItems.filter((item) => item.resource_type === 'service').length;
  const activeDatabaseCount = paidItems.filter((item) => item.resource_type === 'database').length;
  const activeKeyValueCount = paidItems.filter((item) => item.resource_type === 'keyvalue').length;

  if (loading) {
    return (
      <div className="space-y-8 animate-pulse">
        <div className="h-10 w-72 bg-surface-tertiary rounded" />
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="h-32 bg-surface-tertiary rounded-xl" />
          <div className="h-32 bg-surface-tertiary rounded-xl" />
          <div className="h-32 bg-surface-tertiary rounded-xl" />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-8 animate-enter pb-12">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wide text-brand font-semibold">Workspace</p>
          <h1 className="text-3xl font-bold tracking-tight text-white">Billing & Invoices</h1>
          <p className="text-sm text-content-secondary">Manage your subscription, payment methods, and billing history.</p>
        </div>
        <div className="flex items-center gap-2">
          {overview?.has_payment_method && (
            <Button variant="secondary" onClick={handleOpenBillingPortal}>
              <ExternalLink className="w-4 h-4 mr-2" />
              Manage Billing
            </Button>
          )}
          <Button variant={overview?.has_payment_method ? 'secondary' : 'primary'} onClick={handleAddPaymentMethod}>
            <CreditCard className="w-4 h-4 mr-2" />
            {overview?.has_payment_method ? 'Update Payment Method' : 'Add Payment Method'}
          </Button>
        </div>
      </div>

      {overview?.subscription_status === 'past_due' && (
        <div className="flex items-start gap-3 p-4 rounded-lg border border-amber-500/30 bg-amber-500/10">
          <AlertTriangle className="w-5 h-5 text-amber-400 shrink-0 mt-0.5" />
          <div>
            <p className="text-sm font-medium text-amber-200">Payment failed</p>
            <p className="text-xs text-amber-300/80 mt-0.5">
              Your last payment was unsuccessful. Please update your payment method to avoid service interruption.
            </p>
          </div>
          <Button variant="secondary" size="sm" className="ml-auto shrink-0 border-amber-500/30 text-amber-200 hover:bg-amber-500/20" onClick={handleAddPaymentMethod}>
            Update Payment
          </Button>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <StatCard
          title="Credit Balance"
          value={formatCurrency(creditBalanceCents)}
          helper={
            <div className="space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-300 text-[10px] font-medium border border-emerald-500/20">
                  Auto-applied
                </span>
                <span className="text-content-tertiary">
                  Deducted from your balance before billing
                </span>
              </div>
              {nextInvoiceCreditAppliedCents > 0 && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-content-tertiary">Applied next invoice</span>
                  <span className="font-mono text-emerald-300">-{formatCurrency(nextInvoiceCreditAppliedCents)}</span>
                </div>
              )}
            </div>
          }
          icon={<Wallet className="w-5 h-5" />}
        />
        <StatCard
          title="Unbilled Charges"
          value={formatCurrency(nextInvoiceTotalCents)}
          helper={
            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-3">
                <span className="text-content-tertiary">Credits</span>
                <span className="font-mono text-emerald-300">-{formatCurrency(nextInvoiceCreditAppliedCents)}</span>
              </div>
              <div className="h-px bg-border-default/50" />
              <div className="flex items-center justify-between gap-3">
                <span className="text-content-tertiary">Amount due</span>
                <span className="font-mono text-content-primary">{formatCurrency(nextInvoiceAmountDueCents)}</span>
              </div>
              {nextChargeAtLabel && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-content-tertiary">Next charge</span>
                  <span className="font-mono text-content-secondary">{nextChargeAtLabel}</span>
                </div>
              )}
              {nextInvoiceCreditCarryCents > 0 && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-content-tertiary">Credits next month</span>
                  <span className="font-mono text-content-secondary">{formatCurrency(nextInvoiceCreditCarryCents)}</span>
                </div>
              )}
            </div>
          }
          icon={<ReceiptText className="w-5 h-5" />}
        />
        <StatCard
          title="Current Plan"
          value={currentPlan.name}
          helper={
            <div className="flex items-center gap-2">
              <StatusPill status={overview?.subscription_status} />
              <span className="text-content-tertiary">{PLAN_BY_ID[currentPlanId].priceLabel}</span>
            </div>
          }
          icon={<CircleDollarSign className="w-5 h-5" />}
        />
        <StatCard
          title="Payment Method"
          value={
            overview?.has_payment_method
              ? `•••• ${overview.payment_method_last4}`
              : 'None'
          }
          helper={
            overview?.has_payment_method ?
              <div className="flex items-center gap-2 mt-1">
                <CardBrandMark brand={overview.payment_method_brand} className="w-8 h-5" />
                <span className="text-xs text-content-tertiary">{brandDisplay(overview.payment_method_brand)}</span>
              </div> :
              <span className="text-xs text-amber-500">Action required</span>
          }
          icon={<CreditCard className="w-5 h-5" />}
        />
      </div>

      <div className="space-y-8">
        <SectionFrame id="plan" title="Plan Overview">
            <div className="grid grid-cols-1 lg:grid-cols-[2fr_1fr] gap-6">
              <div className="space-y-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="text-xl font-bold text-white">{currentPlan.name}</h3>
                    <p className="text-sm text-content-secondary mt-1">{currentPlan.description}</p>
                  </div>
                </div>
                <div className="flex items-center gap-3 pt-2">
                  <div className="px-3 py-1.5 bg-surface-tertiary/50 border border-border-default rounded flex items-center gap-2">
                    <span className="text-xs text-content-tertiary uppercase tracking-wider">CPU</span>
                    <span className="text-sm font-mono text-content-primary">{currentPlan.cpu}</span>
                  </div>
                  <div className="px-3 py-1.5 bg-surface-tertiary/50 border border-border-default rounded flex items-center gap-2">
                    <span className="text-xs text-content-tertiary uppercase tracking-wider">RAM</span>
                    <span className="text-sm font-mono text-content-primary">{currentPlan.mem}</span>
                  </div>
                </div>
              </div>

              <div className="border-l border-border-default/50 pl-6 space-y-3 flex flex-col justify-center">
                <Button variant="secondary" onClick={() => navigate('/billing/plans')} className="w-full justify-start">
                  <PencilLine className="w-4 h-4 mr-2" />
                  Change Plan
                </Button>
                <Button variant="ghost" onClick={handleOpenBillingPortal} className="w-full justify-start">
                  <ExternalLink className="w-4 h-4 mr-2" />
                  Stripe Customer Portal
                </Button>
              </div>
            </div>
        </SectionFrame>

        <SectionFrame id="included-usage" title="Active Resources">
            <div className="space-y-6">
              <p className="text-sm text-content-secondary">
                Active billable resources in this workspace.
              </p>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <UsageBar
                  label="Services"
                  used={activeServiceCount}
                  total={Math.max(activeServiceCount, includedUsage.instanceHours / 750)}
                  color="bg-brand"
                  subtext={activeServiceCount === 1 ? '1 active service' : `${activeServiceCount} active services`}
                />
                <UsageBar
                  label="Databases"
                  used={activeDatabaseCount}
                  total={Math.max(activeDatabaseCount, 5)}
                  color="bg-purple-500"
                  subtext={activeDatabaseCount === 1 ? '1 active database' : `${activeDatabaseCount} active databases`}
                />
                <UsageBar
                  label="Key-Value Stores"
                  used={activeKeyValueCount}
                  total={Math.max(activeKeyValueCount, 5)}
                  color="bg-emerald-500"
                  subtext={activeKeyValueCount === 1 ? '1 active store' : `${activeKeyValueCount} active stores`}
                />
              </div>
            </div>
        </SectionFrame>

        <SectionFrame id="unbilled-charges" title="Unbilled Line Items">
            <div className="space-y-4">
              <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border-default/50 pb-4">
                <p className="text-sm text-content-secondary">Charges accrued this month, to be invoiced next cycle.</p>
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={() => setCollapsedGroups(new Set())} disabled={groupedCharges.length === 0}>Expand All</Button>
                  <Button variant="ghost" size="sm" onClick={() => setCollapsedGroups(new Set(allGroupKeys))} disabled={groupedCharges.length === 0}>Collapse</Button>
                </div>
              </div>

              {unsyncedItems.length > 0 && (
                <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-4">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div className="space-y-1">
                      <p className="text-sm font-semibold text-amber-200">
                        Billing update pending
                      </p>
                      <p className="text-xs text-content-secondary">
                        {unsyncedItems.length} resource{unsyncedItems.length === 1 ? '' : 's'} {unsyncedItems.length === 1 ? 'is' : 'are'} pending attachment to your billing subscription. This is handled by the platform automatically.
                      </p>
                    </div>
                  </div>

                  <details className="mt-3">
                    <summary className="text-xs text-amber-200/90 cursor-pointer select-none">
                      Show unsynced resources
                    </summary>
                    <div className="mt-2 rounded-lg border border-amber-500/15 overflow-hidden">
                      {unsyncedItems.map((item) => (
                        <div key={`${item.resource_type}-${item.resource_id}`} className="px-3 py-2 flex justify-between text-xs border-b border-amber-500/10 last:border-0 bg-surface-tertiary/10">
                          <div className="flex items-center gap-2">
                            <span className="text-content-primary">{item.resource_name}</span>
                            <span className="text-content-tertiary uppercase text-[10px]">{item.plan}</span>
                            <span className="px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-300 text-[10px] font-medium border border-amber-500/20">
                              Unsynced
                            </span>
                          </div>
                          <span className="font-mono text-content-secondary">{formatCurrency(item.monthly_cost)}</span>
                        </div>
                      ))}
                    </div>
                  </details>
                </div>
              )}

              {groupedCharges.length === 0 ? (
                <div className="py-8 text-center text-content-tertiary italic">
                  No unbilled charges for this period.
                </div>
              ) : (
                <div className="space-y-2">
                  {groupedCharges.map((group) => {
                    const isOpen = !effectiveCollapsedGroups.has(group.key);
                    return (
                      <div key={group.key} className="border border-border-default/50 rounded-lg overflow-hidden bg-surface-tertiary/10">
                        <button
                          type="button"
                          onClick={() => {
                            setCollapsedGroups((prev) => {
                              const current = prev ?? new Set(allGroupKeys);
                              const next = new Set(current);
                              if (next.has(group.key)) next.delete(group.key);
                              else next.add(group.key);
                              return next;
                            });
                          }}
                          className="w-full px-4 py-3 flex items-center justify-between hover:bg-surface-tertiary/30 transition-colors"
                        >
                          <div className="flex items-center gap-3">
                            {isOpen ? <ChevronDown className="w-4 h-4 text-content-tertiary" /> : <ChevronRight className="w-4 h-4 text-content-tertiary" />}
                            <div className="flex items-center gap-2 text-sm font-medium text-content-primary">
                              {resourceIcon(group.key)}
                              {group.label}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <span className="font-mono text-content-primary">{formatCurrency(group.total)}</span>
                          </div>
                        </button>

                        {isOpen && (
                          <div className="bg-surface-tertiary/5 border-t border-border-default/30">
                            {group.items.map((item) => (
                              <div key={`${item.resource_type}-${item.resource_id}`} className="px-4 py-2 pl-12 flex justify-between text-xs border-b border-border-default/10 last:border-0">
                                <div className="flex items-center gap-2">
                                  <span className="text-content-primary">{item.resource_name}</span>
                                  <span className="text-content-tertiary uppercase text-[10px]">{item.plan}</span>
                                </div>
                                <span className={cn('font-mono text-content-secondary')}>{formatCurrency(item.monthly_cost)}</span>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}

              {(stripeLinkedItems.length > 0 || (overview?.invoices ?? []).length > 0) && (
                <div className="flex justify-end pt-2">
                  <Button variant="ghost" size="sm" onClick={handleDownloadCsv}>
                    <FileDown className="w-4 h-4 mr-2" />
                    Export CSV
                  </Button>
                </div>
              )}
            </div>
        </SectionFrame>

        {(overview?.invoices ?? []).length > 0 && (
          <SectionFrame id="invoice-history" title="Invoice History">
            <div className="space-y-2">
              <p className="text-sm text-content-secondary mb-4">Past invoices from Stripe.</p>
              <div className="border border-border-default/50 rounded-lg overflow-hidden">
                <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60 bg-surface-tertiary/20">
                  <div className="col-span-3">Date</div>
                  <div className="col-span-2">Status</div>
                  <div className="col-span-2 text-right">Amount</div>
                  <div className="col-span-2 text-right">Paid</div>
                  <div className="col-span-3 text-right">Actions</div>
                </div>
                {(overview?.invoices ?? []).map((inv) => (
                  <div key={inv.id} className="grid grid-cols-12 px-4 py-3 border-b border-border-default/10 last:border-0 items-center">
                    <div className="col-span-3 text-sm text-content-secondary">
                      {new Date(inv.created_at).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })}
                    </div>
                    <div className="col-span-2">
                      <span className={cn(
                        'text-xs font-medium px-2 py-0.5 rounded-full',
                        inv.status === 'paid' && 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/20',
                        inv.status === 'open' && 'bg-amber-500/10 text-amber-300 border border-amber-500/20',
                        inv.status !== 'paid' && inv.status !== 'open' && 'bg-surface-tertiary text-content-tertiary border border-border-default',
                      )}>
                        {inv.status}
                      </span>
                    </div>
                    <div className="col-span-2 text-sm font-mono text-content-primary text-right">
                      {formatCurrency(inv.amount_due_cents)}
                    </div>
                    <div className="col-span-2 text-sm font-mono text-content-secondary text-right">
                      {formatCurrency(inv.amount_paid_cents)}
                    </div>
                    <div className="col-span-3 flex justify-end gap-2">
                      {inv.hosted_invoice_url && (
                        <a href={inv.hosted_invoice_url} target="_blank" rel="noreferrer" className="text-xs text-brand hover:text-brand-hover inline-flex items-center gap-1">
                          View <ExternalLink className="w-3 h-3" />
                        </a>
                      )}
                      {inv.invoice_pdf_url && (
                        <a href={inv.invoice_pdf_url} target="_blank" rel="noreferrer" className="text-xs text-content-secondary hover:text-content-primary inline-flex items-center gap-1">
                          PDF <FileDown className="w-3 h-3" />
                        </a>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </SectionFrame>
        )}

        {(overview?.credit_ledger ?? []).length > 0 && (
          <SectionFrame id="credit-history" title="Credit History">
            <div className="space-y-2">
              <div className="flex items-center justify-between mb-4">
                <p className="text-sm text-content-secondary">Transaction history for your credit balance.</p>
                <span className="text-xs text-content-tertiary">{(overview?.credit_ledger ?? []).length} transactions</span>
              </div>
              <details className="group">
                <summary className="cursor-pointer select-none text-sm text-content-secondary hover:text-content-primary transition-colors flex items-center gap-1.5">
                  <ChevronRight className="w-3.5 h-3.5 group-open:rotate-90 transition-transform" />
                  View transactions
                </summary>
                <div className="border border-border-default/50 rounded-lg overflow-hidden mt-3">
                  <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60 bg-surface-tertiary/20">
                    <div className="col-span-3">Date</div>
                    <div className="col-span-3 text-right pr-4">Amount</div>
                    <div className="col-span-6 pl-2">Reason</div>
                  </div>
                  {(overview?.credit_ledger ?? []).map((entry) => (
                    <div key={entry.id} className="grid grid-cols-12 px-4 py-2.5 border-b border-border-default/10 last:border-0 items-center">
                      <div className="col-span-3 text-xs text-content-secondary">
                        {new Date(entry.created_at).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })}
                      </div>
                      <div className={cn(
                        'col-span-3 text-xs font-mono text-right pr-4',
                        entry.amount_cents > 0 ? 'text-emerald-400' : 'text-amber-300',
                      )}>
                        {entry.amount_cents > 0 ? '+' : ''}{formatCurrency(Math.abs(entry.amount_cents))}
                      </div>
                      <div className="col-span-6 text-xs text-content-tertiary truncate pl-2">
                        {formatCreditReason(entry.reason)}
                      </div>
                    </div>
                  ))}
                </div>
              </details>
            </div>
          </SectionFrame>
        )}

        <SectionFrame id="billing-info" title="Billing Details">
            <div className="grid grid-cols-1 gap-6">
              <div className="space-y-2">
                <p className="text-xs uppercase tracking-wider text-content-tertiary">Billing Email</p>
                <div className="flex items-center justify-between p-3 rounded bg-surface-tertiary/20 border border-border-default/50">
                  <span className="text-sm font-medium">{billingEmail || 'Not configured'}</span>
                  <Button variant="ghost" size="sm" onClick={handleOpenBillingPortal}><PencilLine className="w-3 h-3" /></Button>
                </div>
              </div>
            </div>

        </SectionFrame>
      </div>
    </div>
  );
}

function UsageBar({ label, used, total, color, subtext }: { label: string, used: number, total: number, color: string, subtext?: string }) {
  return (
    <div className="p-4 rounded-lg bg-surface-tertiary/20 border border-border-default/50">
      <div className="flex justify-between text-sm mb-2">
        <span className="text-content-secondary">{label}</span>
        <span className="font-mono text-content-primary">{used} <span className="text-content-tertiary">/ {total.toLocaleString()}</span></span>
      </div>
      <div className="h-1.5 bg-surface-tertiary rounded-full overflow-hidden">
        <div className={`h-full ${color} shadow-[0_0_10px_currentColor]`} style={{ width: `${usagePercent(used, total)}%` }} />
      </div>
      {subtext && <p className="text-[10px] text-content-tertiary mt-2">{subtext}</p>}
    </div>
  )
}
