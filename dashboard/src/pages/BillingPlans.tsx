import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ArrowLeft, CreditCard } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Modal } from '../components/ui/Modal';
import { ApiError, billing, blueprints, databases, keyvalue, services as servicesApi } from '../lib/api';
import { PLAN_SPECS, type PlanID } from '../lib/plans';
import { cn } from '../lib/utils';
import { toast } from 'sonner';
import type { BillingOverview, Blueprint, BlueprintResource, ManagedDatabase, ManagedKeyValue, Service } from '../types';

function normalizePlan(plan: string): PlanID {
  const p = (plan || '').trim().toLowerCase();
  if (p === 'free' || p === 'starter' || p === 'standard' || p === 'pro') return p;
  return 'free';
}

type ResourceKind = 'service' | 'database' | 'keyvalue';
type ResourceRow = {
  kind: ResourceKind;
  id: string;
  name: string;
  plan: PlanID;
  status?: string;
  href: string;
};

function rowKey(row: Pick<ResourceRow, 'kind' | 'id'>) {
  return `${row.kind}:${row.id}`;
}

function safeReturnTo(value: string): string | null {
  const v = (value || '').trim();
  if (!v) return null;
  if (!v.startsWith('/')) return null;
  if (v.startsWith('//')) return null;
  if (v.toLowerCase().startsWith('/\\')) return null;
  return v;
}

export function BillingPlans() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const [loading, setLoading] = useState(true);
  const [overview, setOverview] = useState<BillingOverview | null>(null);
  const [blueprint, setBlueprint] = useState<(Blueprint & { resources: BlueprintResource[] }) | null>(null);
  const [svcs, setSvcs] = useState<Service[]>([]);
  const [dbs, setDbs] = useState<ManagedDatabase[]>([]);
  const [kvs, setKvs] = useState<ManagedKeyValue[]>([]);

  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [selected, setSelected] = useState<Record<string, PlanID>>({});
  const [upgradeModal, setUpgradeModal] = useState<{ open: boolean; message: string }>({ open: false, message: '' });

  const focus = (searchParams.get('focus') || '').trim().toLowerCase();
  const focusID = (searchParams.get('resource_id') || '').trim();
  const source = (searchParams.get('source') || '').trim().toLowerCase();
  const blueprintId = (searchParams.get('blueprint_id') || '').trim();
  const returnTo = safeReturnTo(searchParams.get('return_to') || '');
  const backHref = returnTo || '/billing';
  const backLabel = returnTo?.startsWith('/blueprints/') ? 'Back to Blueprint' : 'Back';

  function formatBlueprintSyncError(syncError: string | null): string | null {
    if (!syncError) return null;
    const lower = syncError.toLowerCase();

    if (lower.startsWith('billing error')) {
      // Prefer showing backend-provided details if present: "billing error: <detail>"
      const idx = syncError.indexOf(':');
      if (idx !== -1 && idx < syncError.length - 1) {
        const detail = syncError.slice(idx + 1).trim();
        if (detail) return detail;
      }
      return 'Billing error. Please update your payment method (or contact support) and retry sync.';
    }
    if (lower.includes('payment method required')) {
      return 'Payment method required. Add or update a payment method, then retry sync.';
    }
    if (lower.includes('stripe') && lower.includes('price')) {
      return 'Paid plan billing is not configured on this server. Please contact support.';
    }
    return syncError;
  }

  useEffect(() => {
    let alive = true;
    setLoading(true);
    Promise.all([
      billing.getOverview(),
      servicesApi.list(),
      databases.list(),
      keyvalue.list(),
    ])
      .then(([o, s, d, k]) => {
        if (!alive) return;
        setOverview(o);
        setSvcs(Array.isArray(s) ? s : []);
        setDbs(Array.isArray(d) ? d : []);
        setKvs(Array.isArray(k) ? k : []);
      })
      .catch((e: unknown) => {
        if (!alive) return;
        toast.error(e instanceof Error ? e.message : 'Failed to load plans');
      })
      .finally(() => {
        if (!alive) return;
        setLoading(false);
      });
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    let alive = true;
    if (source !== 'blueprint' || !blueprintId) {
      setBlueprint(null);
      return () => { alive = false; };
    }
    blueprints
      .get(blueprintId)
      .then((bp) => {
        if (!alive) return;
        setBlueprint(bp);
      })
      .catch(() => {
        if (!alive) return;
        setBlueprint(null);
      });
    return () => { alive = false; };
  }, [source, blueprintId]);

  const rows = useMemo<ResourceRow[]>(() => {
    const out: ResourceRow[] = [];
    svcs.forEach((s) => {
      out.push({
        kind: 'service',
        id: s.id,
        name: s.name || 'Service',
        plan: normalizePlan(s.plan),
        status: s.status,
        href: `/services/${encodeURIComponent(s.id)}/scaling`,
      });
    });
    dbs.forEach((d) => {
      out.push({
        kind: 'database',
        id: d.id,
        name: d.name || 'Database',
        plan: normalizePlan(d.plan),
        status: d.status,
        href: `/databases/${encodeURIComponent(d.id)}/settings`,
      });
    });
    kvs.forEach((k) => {
      out.push({
        kind: 'keyvalue',
        id: k.id,
        name: k.name || 'Key Value',
        plan: normalizePlan(k.plan),
        status: k.status,
        href: `/keyvalue/${encodeURIComponent(k.id)}`,
      });
    });
    return out;
  }, [dbs, kvs, svcs]);

  useEffect(() => {
    // Initialize selected state from current plans (idempotent).
    setSelected((prev) => {
      const next = { ...prev };
      rows.forEach((r) => {
        const k = rowKey(r);
        if (!next[k]) next[k] = r.plan;
      });
      return next;
    });
  }, [rows]);

  const grouped = useMemo(() => {
    return {
      services: rows.filter((r) => r.kind === 'service'),
      databases: rows.filter((r) => r.kind === 'database'),
      keyvalue: rows.filter((r) => r.kind === 'keyvalue'),
    };
  }, [rows]);

  const handleRetryBlueprintSync = async () => {
    if (!blueprintId) return;
    setBusyKey(`retry-blueprint:${blueprintId}`);
    try {
      await blueprints.sync(blueprintId);
      toast.success('Sync started');
      if (returnTo) navigate(returnTo);
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : 'Failed to start sync');
    } finally {
      setBusyKey(null);
    }
  };

  const handleAddPaymentMethod = async () => {
    try {
      const { url } = await billing.createCheckoutSession(`${window.location.origin}/billing/plans`);
      window.location.href = url;
    } catch {
      toast.error('Failed to open checkout');
    }
  };

  const applyPlan = async (row: ResourceRow) => {
    const k = rowKey(row);
    const desired = selected[k] || row.plan;
    if (desired === row.plan) return;

    setBusyKey(k);
    try {
      if (row.kind === 'service') {
        const updated = await servicesApi.update(row.id, { plan: desired });
        setSvcs((prev) => prev.map((s) => (s.id === updated.id ? updated : s)));
      } else if (row.kind === 'database') {
        const updated = await databases.update(row.id, { plan: desired });
        setDbs((prev) => prev.map((d) => (d.id === updated.id ? updated : d)));
      } else {
        const updated = await keyvalue.update(row.id, { plan: desired });
        setKvs((prev) => prev.map((x) => (x.id === updated.id ? updated : x)));
      }
      toast.success('Plan updated');
    } catch (e: unknown) {
      if (e instanceof ApiError && e.status === 402) {
        setUpgradeModal({
          open: true,
          message: 'A payment method is required to use paid plans.',
        });
        return;
      }
      const msg = e instanceof Error ? e.message : 'Failed to update plan';
      if (msg.toLowerCase().includes('stripe price')) {
        setUpgradeModal({
          open: true,
          message: 'Upgrades are temporarily unavailable because paid plan pricing is not configured. Please contact support.',
        });
        return;
      }
      toast.error(msg);
    } finally {
      setBusyKey(null);
    }
  };

  const sectionHeader = (title: string, count: number) => (
    <div className="flex items-end justify-between">
      <div>
        <div className="text-sm font-semibold text-content-primary">{title}</div>
        <div className="text-xs text-content-tertiary">{count} resources</div>
      </div>
    </div>
  );

  const ResourceTable = ({ items }: { items: ResourceRow[] }) => {
    if (items.length === 0) {
      return (
        <Card>
          <div className="text-sm text-content-tertiary text-center py-8">No resources found.</div>
        </Card>
      );
    }
    return (
      <Card className="p-0 overflow-hidden">
        <div className="grid grid-cols-12 px-4 py-2 text-[11px] uppercase tracking-[0.12em] text-content-tertiary border-b border-border-default/60">
          <div className="col-span-5">Resource</div>
          <div className="col-span-2">Current</div>
          <div className="col-span-3">New Plan</div>
          <div className="col-span-2 text-right">Action</div>
        </div>
        {items.map((r) => {
          const k = rowKey(r);
          const desired = selected[k] || r.plan;
          const isFocused = focusID && r.id === focusID && (focus === '' || focus === r.kind);
          return (
            <div
              key={k}
              className={cn(
                'grid grid-cols-12 px-4 py-3 border-b border-border-subtle items-center gap-3',
                isFocused && 'bg-brand/5'
              )}
            >
              <div className="col-span-5 min-w-0">
                <button
                  onClick={() => navigate(r.href)}
                  className="text-left w-full text-sm font-semibold text-content-primary truncate hover:underline decoration-brand/60 underline-offset-4"
                  title="Open"
                >
                  {r.name}
                </button>
                <div className="text-xs text-content-tertiary truncate">
                  <span className="font-mono">{r.id.slice(0, 8)}</span>
                  {r.status ? ` · ${r.status}` : ''}
                </div>
              </div>
              <div className="col-span-2">
                <span className="text-xs px-2 py-1 rounded-full border border-border-default bg-surface-tertiary text-content-secondary capitalize">
                  {r.plan}
                </span>
              </div>
              <div className="col-span-3">
                <select
                  value={desired}
                  onChange={(e) => {
                    const v = normalizePlan(e.target.value);
                    setSelected((prev) => ({ ...prev, [k]: v }));
                  }}
                  className="w-full h-9 px-3 rounded-md bg-surface-secondary border border-border-default text-sm capitalize"
                >
                  {PLAN_SPECS.map((p) => (
                    <option key={p.id} value={p.id}>{p.name}</option>
                  ))}
                </select>
              </div>
              <div className="col-span-2 text-right">
                <Button
                  size="sm"
                  variant={desired === r.plan ? 'secondary' : 'primary'}
                  disabled={desired === r.plan || busyKey === k}
                  loading={busyKey === k}
                  onClick={() => applyPlan(r)}
                >
                  {desired === r.plan ? 'No change' : 'Apply'}
                </Button>
              </div>
            </div>
          );
        })}
      </Card>
    );
  };

  if (loading) {
    return (
      <div className="text-sm text-content-tertiary py-16 text-center">
        Loading...
      </div>
    );
  }

  const bpIsFailed = (blueprint?.last_sync_status || '').startsWith('failed');
  const bpSyncError = bpIsFailed && (blueprint?.last_sync_status || '').includes(': ')
    ? (blueprint?.last_sync_status || '').slice((blueprint?.last_sync_status || '').indexOf(': ') + 2)
    : null;
  const bpSyncErrorDisplay = formatBlueprintSyncError(bpSyncError);

  return (
    <div className="space-y-6 pb-10">
      <div className="flex items-start justify-between gap-4">
        <div>
          <button
            onClick={() => navigate(backHref)}
            className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors mb-3"
          >
            <ArrowLeft className="w-4 h-4" />
            {backLabel}
          </button>
          <h1 className="text-3xl font-semibold text-content-primary">Plans & Upgrades</h1>
          <p className="text-sm text-content-secondary mt-1">
            Change plans per resource. You can upgrade immediately and deploy again.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {!overview?.has_payment_method && (
            <Button onClick={handleAddPaymentMethod}>
              <CreditCard className="w-4 h-4 mr-2" />
              Add Payment Method
            </Button>
          )}
        </div>
      </div>

      {source === 'blueprint' && (
        <Card className="border border-brand/20 bg-brand/5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="text-sm font-semibold text-content-primary">Blueprint sync needs an upgrade</div>
              <div className="text-xs text-content-secondary mt-1">
                Plans apply per resource. If your blueprint failed before resources were created, add/update your payment method and then retry sync.
              </div>
              {blueprint && (
                <div className="text-xs text-content-tertiary mt-2">
                  Blueprint: <span className="font-mono">{blueprint.name}</span>
                  {blueprint.last_sync_status ? ` · Status: ${blueprint.last_sync_status}` : ''}
                </div>
              )}
              {bpSyncErrorDisplay && (
                <div className="text-xs text-status-error mt-2 break-words">
                  {bpSyncErrorDisplay}
                </div>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {blueprintId && (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={busyKey === `retry-blueprint:${blueprintId}`}
                  loading={busyKey === `retry-blueprint:${blueprintId}`}
                  onClick={handleRetryBlueprintSync}
                >
                  Retry Sync
                </Button>
              )}
              {returnTo && (
                <Button size="sm" variant="secondary" onClick={() => navigate(returnTo)}>
                  Back
                </Button>
              )}
              <Button size="sm" onClick={handleAddPaymentMethod}>
                <CreditCard className="w-4 h-4 mr-2" />
                {overview?.has_payment_method ? 'Update Card' : 'Add Card'}
              </Button>
            </div>
          </div>
        </Card>
      )}

      <Card>
        <div className="flex items-center justify-between gap-4">
          <div>
            <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Payment Method</div>
            <div className="text-sm text-content-primary mt-2">
              {overview?.has_payment_method ? `•••• ${overview.payment_method_last4}` : 'None'}
            </div>
            {!overview?.has_payment_method && (
              <div className="text-xs text-amber-500 mt-1">Paid plan upgrades require a saved payment method.</div>
            )}
          </div>
          <Button variant="secondary" onClick={handleAddPaymentMethod}>
            <CreditCard className="w-4 h-4 mr-2" />
            {overview?.has_payment_method ? 'Update Card' : 'Add Card'}
          </Button>
        </div>
      </Card>

      <div className="grid grid-cols-1 lg:grid-cols-4 gap-4">
        {PLAN_SPECS.map((p) => (
          <Card key={p.id} className={cn('border border-border-default', p.highlighted && 'border-brand/40')}>
            <div className="flex items-center justify-between">
              <div className="text-sm font-semibold text-content-primary">{p.name}</div>
              {p.badge && (
                <span className="text-[11px] px-2 py-1 rounded-full bg-brand/10 text-brand border border-brand/20">
                  {p.badge}
                </span>
              )}
            </div>
            <div className="text-xs text-content-tertiary mt-1">{p.cpu} · {p.mem}</div>
            <div className="text-sm font-semibold text-content-primary mt-3">{p.priceLabel}</div>
            <div className="text-xs text-content-secondary mt-2">{p.description}</div>
          </Card>
        ))}
      </div>

      {rows.length === 0 ? (
        <Card>
          <div className="py-10 text-center space-y-4">
            <div className="text-sm font-semibold text-content-primary">No resources found</div>
            {source === 'blueprint' ? (
              <div className="text-sm text-content-secondary max-w-[70ch] mx-auto">
                This page lists existing resources so you can change their plans. Your blueprint sync failed before resources were created, so there’s nothing to upgrade yet.
                Fix the sync error above, then click <span className="font-semibold">Retry Sync</span>.
              </div>
            ) : (
              <div className="text-sm text-content-secondary max-w-[60ch] mx-auto">
                Plans are applied per resource. Create a service, database, or key value store, then come back here to upgrade it.
              </div>
            )}
            <div className="flex flex-wrap items-center justify-center gap-2">
              {source === 'blueprint' && blueprintId && (
                <Button
                  onClick={handleRetryBlueprintSync}
                  disabled={busyKey === `retry-blueprint:${blueprintId}`}
                  loading={busyKey === `retry-blueprint:${blueprintId}`}
                >
                  Retry Sync
                </Button>
              )}
              <Button onClick={() => navigate('/new')}>Create Service</Button>
              <Button variant="secondary" onClick={() => navigate('/new/postgres')}>Create Database</Button>
              <Button variant="secondary" onClick={() => navigate('/new/keyvalue')}>Create Key Value</Button>
              <Button variant="secondary" onClick={() => navigate('/new/blueprint')}>Create Blueprint</Button>
              {returnTo && (
                <Button variant="secondary" onClick={() => navigate(returnTo)}>
                  Back
                </Button>
              )}
            </div>
          </div>
        </Card>
      ) : (
        <>
          <div className="space-y-4">
            {sectionHeader('Services', grouped.services.length)}
            <ResourceTable items={grouped.services} />
          </div>

          <div className="space-y-4">
            {sectionHeader('Databases', grouped.databases.length)}
            <ResourceTable items={grouped.databases} />
          </div>

          <div className="space-y-4">
            {sectionHeader('Key Value', grouped.keyvalue.length)}
            <ResourceTable items={grouped.keyvalue} />
          </div>
        </>
      )}

      <Modal
        open={upgradeModal.open}
        onClose={() => setUpgradeModal({ open: false, message: '' })}
        title="Upgrade Required"
        footer={
          <>
            <Button variant="secondary" onClick={() => setUpgradeModal({ open: false, message: '' })}>
              Not now
            </Button>
            <Button
              onClick={() => {
                setUpgradeModal({ open: false, message: '' });
                handleAddPaymentMethod();
              }}
            >
              Add Payment Method
            </Button>
          </>
        }
      >
        <p className="text-sm text-content-secondary">{upgradeModal.message}</p>
      </Modal>
    </div>
  );
}
