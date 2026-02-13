import { useEffect, useMemo, useState } from 'react';
import {
  ChevronDown,
  ChevronRight,
  CircleDollarSign,
  CreditCard,
  Database,
  ExternalLink,
  FileDown,
  Globe,
  Info,
  Key,
  PencilLine,
  ReceiptText,
} from 'lucide-react';
import { Button } from '../components/ui/Button';
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

type Section = {
  id: string;
  label: string;
};

const sections: Section[] = [
  { id: 'plan', label: 'Plan' },
  { id: 'payment-method', label: 'Payment Method' },
  { id: 'billing-information', label: 'Billing Information' },
  { id: 'included-usage', label: 'Included Usage' },
  { id: 'unbilled-charges', label: 'Unbilled Charges' },
  { id: 'credit-balance', label: 'Credit Balance' },
  { id: 'invoice-history', label: 'Invoice History' },
];

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
        className={cn('inline-flex items-center justify-center rounded-md overflow-hidden', className)}
        aria-label="Visa"
      >
        <VisaLogo className="h-6 w-auto" />
      </div>
    );
  }

  if (key === 'mastercard') {
    return (
      <div
        className={cn('inline-flex items-center justify-center rounded-md overflow-hidden', className)}
        aria-label="Mastercard"
      >
        <MastercardLogo className="h-6 w-auto" />
      </div>
    );
  }

  return (
    <div
      className={cn(
        'inline-flex items-center justify-center rounded-md border border-border-default bg-surface-primary text-content-primary',
        className
      )}
      aria-label="Card"
    >
      <CreditCard className="w-4 h-4" />
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

function formatMonthLabel(date: Date) {
  return date.toLocaleString('en-US', { month: 'long', year: 'numeric' });
}

function usagePercent(used: number, included: number) {
  if (included <= 0) return 0;
  return Math.min(100, Math.round((used / included) * 100));
}

type StatusTone = 'success' | 'warning' | 'danger' | 'neutral';

function StatCard({
  title,
  value,
  helper,
  icon,
  emphasis = false,
}: {
  title: string;
  value: string;
  helper?: string | React.ReactNode;
  icon?: React.ReactNode;
  emphasis?: boolean;
}) {
  return (
    <div
      className={cn(
        'border border-border-default bg-surface-secondary rounded-lg p-5 flex items-start gap-3',
        emphasis && 'bg-surface-tertiary border-border-hover'
      )}
    >
      {icon && <div className="w-10 h-10 rounded-full bg-surface-primary border border-border-default flex items-center justify-center text-content-primary">{icon}</div>}
      <div className="flex-1">
        <p className="text-xs uppercase tracking-wide text-content-tertiary">{title}</p>
        <p className="text-xl font-semibold text-content-primary mt-1">{value}</p>
        {helper && <p className="text-sm text-content-secondary mt-1 leading-relaxed">{helper}</p>}
      </div>
    </div>
  );
}

function subscriptionStatusDisplay(status?: string): { label: string; tone: StatusTone } {
  const normalized = (status || 'active').trim().toLowerCase();
  const label = normalized
    .replace(/_/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (c) => c.toUpperCase());

  switch (normalized) {
    case 'active':
    case 'trialing':
      return { label: label || 'Active', tone: 'success' };
    case 'past_due':
    case 'incomplete':
      return { label: label || 'Past due', tone: 'warning' };
    case 'unpaid':
    case 'incomplete_expired':
      return { label: label || 'Unpaid', tone: 'danger' };
    case 'canceled':
    case 'cancelled':
    case 'ended':
      return { label: label || 'Canceled', tone: 'neutral' };
    default:
      return { label: label || 'Unknown', tone: 'neutral' };
  }
}

function StatusPill({ status }: { status?: string }) {
  const { label, tone } = subscriptionStatusDisplay(status);

  const toneClass =
    tone === 'success'
      ? 'bg-status-success-bg text-status-success border border-status-success/20'
      : tone === 'warning'
        ? 'bg-status-warning-bg text-status-warning border border-status-warning/20'
        : tone === 'danger'
          ? 'bg-status-error-bg text-status-error border border-status-error/20'
          : 'bg-surface-tertiary text-content-secondary border border-border-default';

  const dotClass =
    tone === 'success'
      ? 'bg-status-success'
      : tone === 'warning'
        ? 'bg-status-warning'
        : tone === 'danger'
          ? 'bg-status-error'
          : 'bg-content-tertiary';

  return (
    <span className={cn('inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-medium', toneClass)}>
      <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
      {label}
    </span>
  );
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
    <section id={id} className="border border-border-default bg-surface-primary rounded-lg overflow-hidden">
      <div className="px-6 py-5 border-b border-border-subtle">
        <h2 className="text-xl font-semibold text-content-primary tracking-tight">{title}</h2>
      </div>
      <div className="px-6 py-6">{children}</div>
    </section>
  );
}

export function Billing() {
  const [overview, setOverview] = useState<BillingOverview | null>(null);
  const [billingEmail, setBillingEmail] = useState('');
  const [loading, setLoading] = useState(true);
  const [activeSection, setActiveSection] = useState<string>(sections[0].id);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set());
  const [promoCode, setPromoCode] = useState('');

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

  useEffect(() => {
    if (loading) return;
    const observer = new IntersectionObserver(
      (entries) => {
        const firstVisible = entries
          .filter((entry) => entry.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (firstVisible?.target?.id) {
          setActiveSection(firstVisible.target.id);
        }
      },
      {
        threshold: [0.2, 0.6],
        rootMargin: '-20% 0px -65% 0px',
      }
    );

    sections.forEach((section) => {
      const el = document.getElementById(section.id);
      if (el) observer.observe(el);
    });

    return () => observer.disconnect();
  }, [loading]);

  const items = overview?.items ?? [];
  const paidItems = items.filter((item) => item.monthly_cost > 0);
  const monthToDateCents = overview?.monthly_total ?? paidItems.reduce((sum, item) => sum + item.monthly_cost, 0);

  const projectedCents = useMemo(() => {
    const now = new Date();
    const day = now.getDate();
    const daysInMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate();
    if (day <= 0 || daysInMonth <= 0) return monthToDateCents;
    return Math.round((monthToDateCents / day) * daysInMonth);
  }, [monthToDateCents]);

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
    paidItems.forEach((item) => {
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
  }, [paidItems]);

  const allGroupKeys = groupedCharges.map((group) => group.key);

  useEffect(() => {
    setCollapsedGroups((prev) => {
      if (prev.size === 0) return prev;
      const next = new Set<string>();
      prev.forEach((key) => {
        if (allGroupKeys.includes(key)) next.add(key);
      });
      return next;
    });
  }, [allGroupKeys]);

  const invoiceRows = useMemo(() => {
    if (monthToDateCents <= 0) return [];
    return [
      {
        id: 'current',
        date: formatMonthLabel(new Date()),
        status: 'Open',
        totalCents: monthToDateCents,
        appliedCreditsCents: 0,
      },
    ];
  }, [monthToDateCents]);

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
    if (paidItems.length === 0) {
      toast.info('No unbilled charges available to export');
      return;
    }

    const header = ['resource_type', 'resource_name', 'resource_id', 'plan', 'monthly_cost_usd'];
    const rows = paidItems.map((item) => [
      item.resource_type,
      item.resource_name,
      item.resource_id,
      item.plan,
      (item.monthly_cost / 100).toFixed(2),
    ]);

    const csv = [header, ...rows]
      .map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\n');

    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `railpush-unbilled-charges-${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  const handlePromoApply = () => {
    if (!promoCode.trim()) {
      toast.info('Enter a promo code first');
      return;
    }
    toast.info('Promo code redemption is coming soon');
  };

  const scrollToSection = (id: string) => {
    const section = document.getElementById(id);
    if (!section) return;
    section.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  const activeServiceCount = paidItems.filter((item) => item.resource_type === 'service').length;
  const activeDatabaseCount = paidItems.filter((item) => item.resource_type === 'database').length;
  const activeKeyValueCount = paidItems.filter((item) => item.resource_type === 'keyvalue').length;

  if (loading) {
    return (
      <div className="space-y-4">
        <div className="h-10 w-72 bg-surface-secondary border border-border-default rounded animate-pulse" />
        <div className="h-48 bg-surface-secondary border border-border-default rounded animate-pulse" />
        <div className="h-48 bg-surface-secondary border border-border-default rounded animate-pulse" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wide text-content-tertiary">Billing</p>
          <h1 className="text-2xl font-semibold tracking-tight text-content-primary">Workspace billing & invoices</h1>
          <p className="text-sm text-content-secondary">Keep plans, payment, and invoice history in sync with Stripe.</p>
        </div>
        <div className="flex items-center gap-2">
          {overview?.has_payment_method && (
            <Button variant="secondary" onClick={handleOpenBillingPortal}>
              <ExternalLink className="w-4 h-4" />
              Manage Billing
            </Button>
          )}
          <Button variant={overview?.has_payment_method ? 'secondary' : 'primary'} onClick={handleAddPaymentMethod}>
            <CreditCard className="w-4 h-4" />
            {overview?.has_payment_method ? 'Update Payment Method' : 'Add Payment Method'}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <StatCard
          title="Plan"
          value={`${currentPlan.name} • ${PLAN_BY_ID[currentPlanId].priceLabel}`}
          helper={<div className="flex items-center gap-2 text-sm"><StatusPill status={overview?.subscription_status} /><span className="text-content-secondary">Includes {PLAN_BY_ID[currentPlanId].cpu} / {PLAN_BY_ID[currentPlanId].mem}</span></div>}
          icon={<CircleDollarSign className="w-4 h-4" />}
          emphasis
        />
        <StatCard
          title="Unbilled this month"
          value={formatCurrency(monthToDateCents)}
          helper={`Projected ${formatMonthLabel(new Date())}: ${formatCurrency(projectedCents)}`}
          icon={<ReceiptText className="w-4 h-4" />}
        />
        <StatCard
          title="Payment method"
          value={
            overview?.has_payment_method
              ? `${brandDisplay(overview.payment_method_brand)} •••• ${overview.payment_method_last4}`
              : 'No card on file'
          }
          helper={overview?.has_payment_method ? 'Stripe keeps this card current.' : 'Add a card to enable paid plans.'}
          icon={
            overview?.has_payment_method ? (
              <CardBrandMark brand={overview.payment_method_brand} className="px-2.5 py-1.5 text-[10px]" />
            ) : (
              <CreditCard className="w-4 h-4" />
            )
          }
        />
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_240px] gap-6">
        <div className="space-y-6">
          <SectionFrame id="plan" title="Plan & Controls">
            <div className="grid grid-cols-1 lg:grid-cols-[2fr_1fr] gap-4">
              <div className="border border-border-default bg-surface-secondary rounded-lg p-5 space-y-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="space-y-1">
                    <p className="text-xs uppercase tracking-wider text-content-tertiary">Current Plan</p>
                    <h3 className="text-xl font-semibold text-content-primary">{currentPlan.name}</h3>
                    <p className="text-sm text-content-secondary leading-relaxed">{currentPlan.description}</p>
                  </div>
                  <StatusPill status={overview?.subscription_status} />
                </div>
                <div className="flex items-center flex-wrap gap-2 pt-1">
                  <span className="inline-flex items-center border border-border-default bg-surface-primary px-2.5 py-1 text-xs text-content-secondary">
                    {currentPlan.cpu}
                  </span>
                  <span className="inline-flex items-center border border-border-default bg-surface-primary px-2.5 py-1 text-xs text-content-secondary">
                    {currentPlan.mem}
                  </span>
                  <span className="inline-flex items-center border border-border-default bg-surface-primary px-2.5 py-1 text-xs text-content-secondary">
                    {currentPlan.priceLabel}
                  </span>
                </div>
              </div>

              <div className="border border-border-default bg-surface-secondary rounded-lg p-5 space-y-3">
                <p className="text-sm text-content-secondary">Manage your subscription and invoices in Stripe.</p>
                <div className="flex flex-wrap gap-2">
                  <Button variant="secondary" onClick={handleOpenBillingPortal}>
                    <PencilLine className="w-4 h-4" />
                    Update Plan
                  </Button>
                  <Button variant="ghost" onClick={handleOpenBillingPortal}>
                    <ExternalLink className="w-4 h-4" />
                    Open Stripe Portal
                  </Button>
                </div>
              </div>
            </div>
          </SectionFrame>

          <SectionFrame id="payment-method" title="Payment Method">
            <div className="border border-border-default bg-surface-secondary p-6">
              {overview?.has_payment_method ? (
                <div className="space-y-4">
                    <div className="flex flex-wrap items-center justify-between gap-4">
                    <div className="flex items-center gap-3">
                      <CardBrandMark brand={overview.payment_method_brand} className="w-12 h-9 rounded-lg text-[10px]" />
                      <p className="text-lg text-content-primary">
                        {brandDisplay(overview.payment_method_brand)} ending in {overview.payment_method_last4}
                      </p>
                    </div>
                    <StatusPill status={overview.subscription_status} />
                  </div>
                  <Button variant="secondary" onClick={handleOpenBillingPortal}>
                    Edit payment details
                  </Button>
                </div>
              ) : (
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <p className="text-lg text-content-primary">No payment method on file</p>
                    <p className="text-sm text-content-secondary mt-1">
                      Add a card to enable paid plans and uninterrupted service billing.
                    </p>
                  </div>
                  <Button variant="primary" onClick={handleAddPaymentMethod}>
                    Add Payment Method
                  </Button>
                </div>
              )}
            </div>
          </SectionFrame>

          <SectionFrame id="billing-information" title="Billing Information">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="border border-border-default bg-surface-secondary rounded-lg p-5 space-y-2">
                <p className="text-xs uppercase tracking-wider text-content-tertiary">Billing email</p>
                <p className="text-base text-content-primary">{billingEmail || 'Not provided'}</p>
                <p className="text-sm text-content-secondary">Used for invoices and billing notifications.</p>
                <Button variant="ghost" onClick={handleOpenBillingPortal} className="mt-2 w-fit">
                  <PencilLine className="w-4 h-4" />
                  Edit in portal
                </Button>
              </div>
              <div className="border border-border-default bg-surface-secondary rounded-lg p-5 space-y-2">
                <p className="text-xs uppercase tracking-wider text-content-tertiary">Company & Tax Details</p>
                <p className="text-base text-content-secondary">Manage company name, address, and VAT in your Stripe portal.</p>
                <Button variant="ghost" onClick={handleOpenBillingPortal} className="mt-2 w-fit">
                  <ExternalLink className="w-4 h-4" />
                  Open portal
                </Button>
              </div>
            </div>
          </SectionFrame>

          <SectionFrame id="included-usage" title="Monthly Included Usage">
            <div className="space-y-4">
              <p className="text-base text-content-secondary">
                Included quotas are based on your <span className="text-content-primary">{currentPlan.name}</span> plan.
              </p>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="border border-border-default bg-surface-secondary rounded-lg p-4 space-y-2">
                  <div className="flex items-center justify-between text-sm text-content-secondary">
                    <span>Instance Hours</span>
                    <span className="text-content-primary font-semibold">0 / {includedUsage.instanceHours}</span>
                  </div>
                  <div className="h-2 bg-surface-primary rounded-full overflow-hidden border border-border-subtle">
                    <div className="h-full bg-brand" style={{ width: `${usagePercent(0, includedUsage.instanceHours)}%` }} />
                  </div>
                </div>
                <div className="border border-border-default bg-surface-secondary rounded-lg p-4 space-y-2">
                  <div className="flex items-center justify-between text-sm text-content-secondary">
                    <span>Bandwidth (GB)</span>
                    <span className="text-content-primary font-semibold">0 / {includedUsage.bandwidthGb.toLocaleString()}</span>
                  </div>
                  <div className="h-2 bg-surface-primary rounded-full overflow-hidden border border-border-subtle">
                    <div className="h-full bg-brand-purple" style={{ width: `${usagePercent(0, includedUsage.bandwidthGb)}%` }} />
                  </div>
                  <div className="flex justify-between text-xs text-content-tertiary pt-1">
                    <span>Services ({activeServiceCount})</span>
                    <span>DB ({activeDatabaseCount})</span>
                    <span>KV ({activeKeyValueCount})</span>
                  </div>
                </div>
                <div className="border border-border-default bg-surface-secondary rounded-lg p-4 space-y-2">
                  <div className="flex items-center justify-between text-sm text-content-secondary">
                    <span>Pipeline Minutes</span>
                    <span className="text-content-primary font-semibold">0 / {includedUsage.pipelineMinutes.toLocaleString()}</span>
                  </div>
                  <div className="h-2 bg-surface-primary rounded-full overflow-hidden border border-border-subtle">
                    <div className="h-full bg-status-info" style={{ width: `${usagePercent(0, includedUsage.pipelineMinutes)}%` }} />
                  </div>
                </div>
              </div>
              <p className="text-sm text-content-tertiary">
                Usage metering is rolling out soon; until then, totals remain accurate while live usage reads as 0.
              </p>
            </div>
          </SectionFrame>

          <SectionFrame id="unbilled-charges" title="Unbilled Charges">
            <div className="space-y-4">
              <p className="text-base text-content-secondary">Amounts displayed have been accrued within the month to date.</p>

              <div className="flex flex-wrap items-center gap-2">
                <Button
                  variant="secondary"
                  onClick={() => setCollapsedGroups(new Set())}
                  disabled={groupedCharges.length === 0}
                >
                  Expand All
                </Button>
                <Button
                  variant="secondary"
                  onClick={() => setCollapsedGroups(new Set(allGroupKeys))}
                  disabled={groupedCharges.length === 0}
                >
                  Collapse All
                </Button>
              </div>

              {groupedCharges.length === 0 ? (
                <div className="border border-border-default bg-surface-secondary p-6 text-content-secondary">
                  No unbilled charges yet.
                </div>
              ) : (
                <div className="border border-border-default bg-surface-secondary divide-y divide-border-subtle">
                  {groupedCharges.map((group) => {
                    const isOpen = !collapsedGroups.has(group.key);
                    return (
                      <div key={group.key}>
                        <button
                          type="button"
                          onClick={() => {
                            setCollapsedGroups((prev) => {
                              const next = new Set(prev);
                              if (next.has(group.key)) next.delete(group.key);
                              else next.add(group.key);
                              return next;
                            });
                          }}
                          className="w-full px-5 py-4 flex items-center justify-between hover:bg-surface-primary/50 transition-colors"
                        >
                          <div className="flex items-center gap-2 text-base text-content-primary">
                            {isOpen ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                            {resourceIcon(group.key)}
                            <span>{group.label}</span>
                          </div>
                          <span className="text-xl font-semibold text-content-primary">{formatCurrency(group.total)}</span>
                        </button>

                        {isOpen && (
                          <div className="px-5 pb-4">
                            <div className="border border-border-default bg-surface-primary divide-y divide-border-subtle">
                              {group.items.map((item) => (
                                <div key={`${item.resource_type}-${item.resource_id}`} className="px-4 py-3 flex flex-wrap items-center justify-between gap-2">
                                  <div>
                                    <p className="text-base text-content-primary">{item.resource_name}</p>
                                    <p className="text-xs uppercase tracking-wider text-content-tertiary">{item.plan} plan</p>
                                  </div>
                                  <p className="text-base text-content-primary">{formatCurrency(item.monthly_cost)}</p>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}

              <div className="pt-2 flex flex-wrap items-center justify-between gap-3">
                <Button variant="secondary" onClick={handleDownloadCsv}>
                  <FileDown className="w-4 h-4" />
                  Download as CSV
                </Button>
                <div className="space-y-1 text-right">
                  <p className="text-sm text-content-secondary">
                    Total month to date <span className="text-2xl font-semibold text-content-primary ml-3">{formatCurrency(monthToDateCents)}</span>
                  </p>
                  <p className="text-sm text-content-secondary">
                    Projected total for {new Date().toLocaleString('en-US', { month: 'long' })} <span className="text-2xl font-semibold text-content-primary ml-3">{formatCurrency(projectedCents)}</span>
                  </p>
                </div>
              </div>
            </div>
          </SectionFrame>

          <SectionFrame id="credit-balance" title="Credit Balance">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-center">
              <div>
                <p className="text-base text-content-secondary mb-5">The balance will be applied to the amount due on your next invoice.</p>
                <p className="text-xs uppercase tracking-wider text-content-tertiary">Total Balance</p>
                <p className="text-4xl font-semibold text-content-primary mt-2">$0.00</p>
              </div>
              <div className="flex items-stretch">
                <input
                  value={promoCode}
                  onChange={(e) => setPromoCode(e.target.value)}
                  placeholder="Enter promo code"
                  className="flex-1 h-12 bg-surface-primary border border-border-default px-3 text-base text-content-primary outline-none focus:border-border-hover"
                />
                <button
                  type="button"
                  onClick={handlePromoApply}
                  className="h-12 px-5 border border-l-0 border-border-default bg-surface-tertiary text-content-secondary hover:text-content-primary disabled:opacity-60 transition-colors"
                  disabled={!promoCode.trim()}
                >
                  Apply
                </button>
              </div>
            </div>
          </SectionFrame>

          <SectionFrame id="invoice-history" title="Invoice History">
            <div className="space-y-4">
              <p className="text-base text-content-secondary">View or download your past invoices.</p>

              {invoiceRows.length === 0 ? (
                <div className="border border-border-default bg-surface-secondary p-6 flex flex-wrap items-center justify-between gap-3">
                  <p className="text-content-secondary">No invoices have been generated for this workspace yet.</p>
                  <Button variant="secondary" onClick={handleOpenBillingPortal}>
                    <ReceiptText className="w-4 h-4" />
                    Open Stripe Portal
                  </Button>
                </div>
              ) : (
                <div className="border border-border-default bg-surface-secondary overflow-auto">
                  <table className="w-full min-w-[720px]">
                    <thead className="text-xs uppercase tracking-wider text-content-tertiary border-b border-border-subtle">
                      <tr>
                        <th className="text-left py-3 px-4 font-medium">Date</th>
                        <th className="text-left py-3 px-4 font-medium">Status</th>
                        <th className="text-right py-3 px-4 font-medium">Total</th>
                        <th className="text-right py-3 px-4 font-medium">Applied Credits</th>
                        <th className="text-right py-3 px-4 font-medium">Billed Total</th>
                      </tr>
                    </thead>
                    <tbody>
                      {invoiceRows.map((row) => (
                        <tr key={row.id} className="border-b border-border-subtle last:border-0">
                          <td className="py-3 px-4 text-base text-content-primary">{row.date}</td>
                          <td className="py-3 px-4">
                            <span className="inline-flex items-center border border-border-default bg-surface-primary px-2 py-0.5 text-xs text-content-secondary">
                              {row.status}
                            </span>
                          </td>
                          <td className="py-3 px-4 text-right text-content-primary">{formatCurrency(row.totalCents)}</td>
                          <td className="py-3 px-4 text-right text-content-secondary">{formatCurrency(row.appliedCreditsCents)}</td>
                          <td className="py-3 px-4 text-right text-content-primary">{formatCurrency(row.totalCents - row.appliedCreditsCents)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </SectionFrame>
        </div>

        <aside className="hidden xl:block">
          <div className="sticky top-24 border-l border-border-default pl-4 space-y-2">
            {sections.map((section) => (
              <button
                key={section.id}
                type="button"
                onClick={() => scrollToSection(section.id)}
                className={cn(
                  'block w-full text-left text-base font-medium leading-6 py-1 transition-colors',
                  activeSection === section.id
                    ? 'text-content-primary'
                    : 'text-content-secondary hover:text-content-primary'
                )}
              >
                {section.label}
              </button>
            ))}
          </div>
        </aside>
      </div>

      <div className="xl:hidden overflow-x-auto pb-1">
        <div className="inline-flex items-center gap-2 border border-border-default bg-surface-secondary p-1 min-w-max">
          {sections.map((section) => (
            <button
              key={section.id}
              type="button"
              onClick={() => scrollToSection(section.id)}
              className={cn(
                'px-3 py-1.5 text-sm whitespace-nowrap transition-colors',
                activeSection === section.id
                  ? 'bg-surface-tertiary text-content-primary'
                  : 'text-content-secondary hover:text-content-primary'
              )}
            >
              {section.label}
            </button>
          ))}
        </div>
      </div>

      <div className="border border-border-default bg-surface-secondary p-4 flex flex-wrap items-start gap-2 text-sm text-content-secondary">
        <Info className="w-4 h-4 mt-0.5 flex-shrink-0" />
        <p>
          Billing data is synchronized from Stripe and your workspace resources. If totals look stale, open the Stripe portal to refresh payment activity.
        </p>
      </div>
    </div>
  );
}
