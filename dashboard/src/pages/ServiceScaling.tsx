import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { services as servicesApi, ApiError } from '../lib/api';
import { PLAN_SPECS } from '../lib/plans';
import { toast } from 'sonner';
import type { PlanID } from '../lib/plans';
import type { Service } from '../types';
import { UpgradePromptModal } from '../components/billing/UpgradePromptModal';

export function ServiceScaling() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [upgradePrompt, setUpgradePrompt] = useState<{ open: boolean; message: string }>({ open: false, message: '' });
  const [selectedPlan, setSelectedPlan] = useState<PlanID>('starter');
  const [instances, setInstances] = useState(1);

  useEffect(() => {
    if (!serviceId) return;
    setLoading(true);
    servicesApi.get(serviceId)
      .then((s) => {
        setService(s);
        const plan = (s.plan as PlanID) || 'starter';
        setSelectedPlan(plan);
        setInstances(s.instances > 0 ? s.instances : 1);
      })
      .catch(() => {
        setService(null);
      })
      .finally(() => setLoading(false));
  }, [serviceId]);

  const dirty = useMemo(() => {
    if (!service) return false;
    return service.plan !== selectedPlan || service.instances !== instances;
  }, [service, selectedPlan, instances]);

  const handleSave = async () => {
    if (!serviceId) return;
    setSaving(true);
    try {
      const updated = await servicesApi.update(serviceId, { plan: selectedPlan, instances });
      setService(updated);
      toast.success('Scaling updated');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to update scaling';
      if (err instanceof ApiError) {
        if (err.status === 402) {
          setUpgradePrompt({ open: true, message: msg || 'Payment method required.' });
          return;
        }
        if (msg.toLowerCase().includes('billing error') || msg.toLowerCase().includes('stripe price')) {
          setUpgradePrompt({ open: true, message: msg });
          return;
        }
      }
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Scaling</h1>
        <div className="text-sm text-content-secondary">Loading...</div>
      </div>
    );
  }
  if (!service) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Scaling</h1>
        <div className="text-sm text-content-secondary">Service not found.</div>
      </div>
    );
  }

  return (
    <div>
      <h1 className="text-2xl font-semibold text-content-primary mb-6">Scaling</h1>

      {/* Instance Type */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Instance Type
        </h2>
        <div className="grid grid-cols-2 gap-3">
          {PLAN_SPECS.map((plan) => (
            <Card
              key={plan.id}
              hover
              onClick={() => setSelectedPlan(plan.id)}
              className={selectedPlan === plan.id ? 'border-brand ring-2 ring-brand/15' : ''}
            >
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-semibold text-content-primary">{plan.name}</span>
                {plan.id === service.plan && selectedPlan === plan.id && (
                  <span className="text-[11px] px-2 py-0.5 rounded-full bg-brand/10 text-brand font-medium">Current</span>
                )}
                {plan.id !== service.plan && selectedPlan === plan.id && (
                  <span className="text-[11px] px-2 py-0.5 rounded-full bg-surface-tertiary text-content-secondary font-medium border border-border-default">Selected</span>
                )}
              </div>
              <div className="text-xs text-content-secondary space-y-0.5">
                <div>{plan.cpu} &middot; {plan.mem}</div>
                <div className="text-content-primary font-medium">{plan.priceLabel}</div>
              </div>
            </Card>
          ))}
        </div>
      </div>

      {/* Manual Scaling */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Manual Scaling
        </h2>
        <Card padding="lg">
          <div className="flex items-center gap-4 mb-4">
            <label className="text-sm text-content-primary font-medium">Instances:</label>
            <div className="flex-1 flex items-center gap-4">
              <input
                type="range"
                min={1}
                max={10}
                value={instances}
                onChange={(e) => setInstances(Number(e.target.value))}
                className="flex-1 accent-brand"
              />
              <span className="text-sm font-mono text-content-primary w-8 text-center">{instances}</span>
            </div>
          </div>
          <div className="flex items-center justify-between text-xs text-content-tertiary mb-4">
            <span>Min: 1</span>
            <span>Max: 10</span>
          </div>
          <Button onClick={handleSave} disabled={!dirty || saving}>
            {saving ? 'Saving...' : 'Save Changes'}
          </Button>
        </Card>
      </div>

      {/* Autoscaling info */}
      <div>
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Autoscaling
        </h2>
        <Card>
          <div className="text-center py-4">
            <p className="text-sm text-content-secondary mb-1">Autoscaling coming soon</p>
            <p className="text-xs text-content-tertiary">
              Automatically scale based on CPU and memory usage targets.
            </p>
          </div>
        </Card>
      </div>

      <UpgradePromptModal
        open={upgradePrompt.open}
        message={upgradePrompt.message}
        onClose={() => setUpgradePrompt({ open: false, message: '' })}
      />
    </div>
  );
}
