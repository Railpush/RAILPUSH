import { useState } from 'react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { toast } from 'sonner';

const instanceTypes = [
  { id: 'starter', name: 'Starter', cpu: '0.5 CPU', memory: '512 MB', price: '$7/mo' },
  { id: 'standard', name: 'Standard', cpu: '1 CPU', memory: '2 GB', price: '$25/mo' },
  { id: 'pro', name: 'Pro', cpu: '2 CPU', memory: '4 GB', price: '$85/mo' },
  { id: 'pro-plus', name: 'Pro Plus', cpu: '4 CPU', memory: '8 GB', price: '$175/mo' },
];

export function ServiceScaling() {
  const [selectedPlan, setSelectedPlan] = useState('starter');
  const [instances, setInstances] = useState(1);

  return (
    <div>
      <h1 className="text-2xl font-semibold text-content-primary mb-6">Scaling</h1>

      {/* Instance Type */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Instance Type
        </h2>
        <div className="grid grid-cols-2 gap-3">
          {instanceTypes.map((type) => (
            <Card
              key={type.id}
              hover
              onClick={() => setSelectedPlan(type.id)}
              className={selectedPlan === type.id ? 'border-brand ring-2 ring-brand/15' : ''}
            >
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-semibold text-content-primary">{type.name}</span>
                {selectedPlan === type.id && (
                  <span className="text-[11px] px-2 py-0.5 rounded-full bg-brand/10 text-brand font-medium">Current</span>
                )}
              </div>
              <div className="text-xs text-content-secondary space-y-0.5">
                <div>{type.cpu} &middot; {type.memory}</div>
                <div className="text-content-primary font-medium">{type.price}</div>
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
          <Button onClick={() => toast.success(`Scaled to ${instances} instance(s)`)}>Save Changes</Button>
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
    </div>
  );
}
