import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Plus, Trash2, Eye, EyeOff, Upload, ChevronDown, Lock, Unlock } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Dropdown } from '../components/ui/Dropdown';
import { deploys, envVars as envVarsApi } from '../lib/api';
import type { EnvVar } from '../types';
import { toast } from 'sonner';

const SECRET_MASK = '********';
const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function ServiceEnvironment() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [vars, setVars] = useState<EnvVar[]>([]);
  const [, setLoading] = useState(true);
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [newIsSecret, setNewIsSecret] = useState(false);

  useEffect(() => {
    if (!serviceId) return;
    envVarsApi.list(serviceId)
      .then(setVars)
      .catch(() => setVars([]))
      .finally(() => setLoading(false));
  }, [serviceId]);

  const addVar = () => {
    if (!newKey.trim()) return;
    setVars([...vars, { id: crypto.randomUUID(), key: newKey.trim(), value: newValue, is_secret: newIsSecret }]);
    setNewKey('');
    setNewValue('');
    setNewIsSecret(false);
  };

  const removeVar = (id: string) => {
    setVars(vars.filter((v) => v.id !== id));
  };

  const updateVar = (id: string, patch: Partial<EnvVar>) => {
    setVars((prev) => prev.map((v) => (v.id === id ? { ...v, ...patch } : v)));
  };

  const toggleSecret = (v: EnvVar) => {
    if (v.is_secret) {
      if (v.value === SECRET_MASK) {
        toast.error('To unhide a secret variable, re-enter its value first.');
        return;
      }
      updateVar(v.id, { is_secret: false });
      setShowSecrets((s) => ({ ...s, [v.id]: false }));
      return;
    }
    updateVar(v.id, { is_secret: true });
  };

  const saveActions = [
    { label: 'Save, rebuild, and deploy', onClick: () => handleSave('rebuild') },
    { label: 'Save and deploy', onClick: () => handleSave('deploy') },
    { label: 'Save only', onClick: () => handleSave('save') },
  ];

  const handleSave = async (action: string) => {
    if (!serviceId) return;
    const cleaned = vars.map((v) => ({
      key: (v.key || '').trim(),
      value: v.value ?? '',
      is_secret: Boolean(v.is_secret),
    }));

    for (const v of cleaned) {
      if (!v.key) {
        toast.error('Env var key cannot be empty.');
        return;
      }
      if (!ENV_KEY_RE.test(v.key)) {
        toast.error(`Invalid env var key: ${v.key}`);
        return;
      }
    }
    const seen = new Set<string>();
    for (const v of cleaned) {
      const k = v.key.toUpperCase();
      if (seen.has(k)) {
        toast.error(`Duplicate env var key: ${v.key}`);
        return;
      }
      seen.add(k);
    }

    try {
      await envVarsApi.update(serviceId, cleaned);
      if (action !== 'save') {
        await deploys.trigger(serviceId, {});
      }
      toast.success(action === 'save' ? 'Environment variables saved' : 'Environment variables saved and deploy triggered');
    } catch {
      toast.error('Failed to save environment variables');
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Environment</h1>
      </div>

      {/* Environment Variables */}
      <div className="mb-8">
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary">
            Environment Variables
          </h2>
          <div className="flex items-center gap-2">
            <button className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-content-secondary hover:text-content-primary bg-surface-tertiary rounded-md transition-colors">
              <Upload className="w-3.5 h-3.5" />
              Add from .env
            </button>
          </div>
        </div>

        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          {/* Header */}
          <div className="grid grid-cols-[1fr_1fr_110px] gap-2 px-4 py-2 border-b border-border-default">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-content-tertiary">Key</span>
            <span className="text-[11px] font-semibold uppercase tracking-wider text-content-tertiary">Value</span>
            <span />
          </div>

          {/* Existing vars */}
          {vars.map((v) => (
            <div key={v.id} className="grid grid-cols-[1fr_1fr_110px] gap-2 px-4 py-2.5 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50 items-center group">
              <input
                type="text"
                value={v.key}
                onChange={(e) => updateVar(v.id, { key: e.target.value.toUpperCase() })}
                className="bg-transparent border-none text-[13px] font-mono font-medium text-content-primary placeholder:text-content-tertiary focus:outline-none"
              />
              <div className="flex items-center gap-2">
                <input
                  type={v.is_secret && !showSecrets[v.id] ? 'password' : 'text'}
                  value={v.is_secret && v.value === SECRET_MASK ? '' : (v.value || '')}
                  onChange={(e) => updateVar(v.id, { value: e.target.value })}
                  placeholder={v.is_secret ? (v.value === SECRET_MASK ? 'Set secret (leave blank to keep current)' : 'secret') : 'value'}
                  className="flex-1 bg-transparent border-none text-[13px] font-mono text-content-secondary placeholder:text-content-tertiary focus:outline-none"
                />
                {v.is_secret && (
                  <button
                    onClick={() => setShowSecrets((s) => ({ ...s, [v.id]: !s[v.id] }))}
                    className="text-content-tertiary hover:text-content-primary transition-colors"
                    title={showSecrets[v.id] ? 'Hide' : 'Show'}
                  >
                    {showSecrets[v.id] ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                  </button>
                )}
              </div>
              <div className="flex items-center justify-end gap-1">
                <button
                  onClick={() => toggleSecret(v)}
                  className="opacity-0 group-hover:opacity-100 p-1 rounded text-content-tertiary hover:text-content-primary transition-all"
                  title={v.is_secret ? 'Secret' : 'Not secret'}
                >
                  {v.is_secret ? <Lock className="w-3.5 h-3.5" /> : <Unlock className="w-3.5 h-3.5" />}
                </button>
                <button
                  onClick={() => removeVar(v.id)}
                  className="opacity-0 group-hover:opacity-100 p-1 rounded text-content-tertiary hover:text-status-error transition-all"
                  title="Remove"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          ))}

          {/* Add new */}
          <div className="grid grid-cols-[1fr_1fr_110px] gap-2 px-4 py-2.5 bg-surface-tertiary/30 items-center">
            <input
              type="text"
              placeholder="KEY"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value.toUpperCase())}
              className="bg-transparent border-none text-[13px] font-mono text-content-primary placeholder:text-content-tertiary focus:outline-none"
            />
            <input
              type={newIsSecret ? 'password' : 'text'}
              placeholder={newIsSecret ? 'secret value' : 'value'}
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              className="bg-transparent border-none text-[13px] font-mono text-content-secondary placeholder:text-content-tertiary focus:outline-none"
            />
            <div className="flex items-center justify-end gap-1">
              <button
                onClick={() => setNewIsSecret((v) => !v)}
                className="p-1 rounded text-content-tertiary hover:text-content-primary transition-colors"
                title={newIsSecret ? 'Secret' : 'Not secret'}
              >
                {newIsSecret ? <Lock className="w-3.5 h-3.5" /> : <Unlock className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={addVar}
                disabled={!newKey.trim()}
                className="p-1 rounded text-content-tertiary hover:text-status-success transition-colors disabled:opacity-30"
                title="Add"
              >
                <Plus className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Secret Files */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Secret Files
        </h2>
        <Card>
          <div className="text-center py-4">
            <p className="text-sm text-content-secondary mb-3">No secret files configured</p>
            <Button variant="secondary" size="sm">
              <Plus className="w-3.5 h-3.5" />
              Add Secret File
            </Button>
          </div>
        </Card>
      </div>

      {/* Linked Environment Groups */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Linked Environment Groups
        </h2>
        <Card>
          <div className="flex items-center gap-2">
            <select className="flex-1 bg-surface-tertiary border border-border-default rounded-md px-3 py-2 text-sm text-content-primary focus:outline-none focus:border-brand">
              <option>Select env group...</option>
            </select>
            <Button variant="secondary" size="md">Link</Button>
          </div>
        </Card>
      </div>

      {/* Save button with dropdown */}
      <div className="flex justify-end">
        <Dropdown
          trigger={
            <Button variant="primary">
              Save, rebuild, and deploy
              <ChevronDown className="w-4 h-4" />
            </Button>
          }
          items={saveActions}
          align="right"
        />
      </div>
    </div>
  );
}
