import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Plus, Trash2, Eye, EyeOff, Upload, ChevronDown } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Dropdown } from '../components/ui/Dropdown';
import { envVars as envVarsApi } from '../lib/api';
import type { EnvVar } from '../types';
import { toast } from 'sonner';

export function ServiceEnvironment() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [vars, setVars] = useState<EnvVar[]>([]);
  const [, setLoading] = useState(true);
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');

  useEffect(() => {
    if (!serviceId) return;
    envVarsApi.list(serviceId)
      .then(setVars)
      .catch(() => setVars([]))
      .finally(() => setLoading(false));
  }, [serviceId]);

  const addVar = () => {
    if (!newKey.trim()) return;
    setVars([...vars, { id: crypto.randomUUID(), key: newKey, value: newValue, is_secret: false }]);
    setNewKey('');
    setNewValue('');
  };

  const removeVar = (id: string) => {
    setVars(vars.filter((v) => v.id !== id));
  };

  const saveActions = [
    { label: 'Save, rebuild, and deploy', onClick: () => handleSave('rebuild') },
    { label: 'Save and deploy', onClick: () => handleSave('deploy') },
    { label: 'Save only', onClick: () => handleSave('save') },
  ];

  const handleSave = async (action: string) => {
    if (!serviceId) return;
    try {
      await envVarsApi.update(serviceId, vars);
      toast.success(`Environment variables saved${action !== 'save' ? ` and ${action} triggered` : ''}`);
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
          <div className="grid grid-cols-[1fr_1fr_40px] gap-2 px-4 py-2 border-b border-border-default">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-content-tertiary">Key</span>
            <span className="text-[11px] font-semibold uppercase tracking-wider text-content-tertiary">Value</span>
            <span />
          </div>

          {/* Existing vars */}
          {vars.map((v) => (
            <div key={v.id} className="grid grid-cols-[1fr_1fr_40px] gap-2 px-4 py-2.5 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50 items-center group">
              <span className="font-mono text-[13px] font-medium text-content-primary">{v.key}</span>
              <div className="flex items-center gap-2">
                {v.is_secret && !showSecrets[v.id] ? (
                  <>
                    <span className="font-mono text-[13px] text-content-tertiary tracking-widest">
                      {'●'.repeat(12)}
                    </span>
                    <button
                      onClick={() => setShowSecrets({ ...showSecrets, [v.id]: true })}
                      className="text-content-tertiary hover:text-content-primary transition-colors"
                    >
                      <Eye className="w-3.5 h-3.5" />
                    </button>
                  </>
                ) : (
                  <>
                    <span className="font-mono text-[13px] text-content-secondary break-all">{v.value}</span>
                    {v.is_secret && (
                      <button
                        onClick={() => setShowSecrets({ ...showSecrets, [v.id]: false })}
                        className="text-content-tertiary hover:text-content-primary transition-colors"
                      >
                        <EyeOff className="w-3.5 h-3.5" />
                      </button>
                    )}
                  </>
                )}
              </div>
              <button
                onClick={() => removeVar(v.id)}
                className="opacity-0 group-hover:opacity-100 p-1 rounded text-content-tertiary hover:text-status-error transition-all"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          ))}

          {/* Add new */}
          <div className="grid grid-cols-[1fr_1fr_40px] gap-2 px-4 py-2.5 bg-surface-tertiary/30 items-center">
            <input
              type="text"
              placeholder="KEY"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value.toUpperCase())}
              className="bg-transparent border-none text-[13px] font-mono text-content-primary placeholder:text-content-tertiary focus:outline-none"
            />
            <input
              type="text"
              placeholder="value"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              className="bg-transparent border-none text-[13px] font-mono text-content-secondary placeholder:text-content-tertiary focus:outline-none"
            />
            <button
              onClick={addVar}
              disabled={!newKey.trim()}
              className="p-1 rounded text-content-tertiary hover:text-status-success transition-colors disabled:opacity-30"
            >
              <Plus className="w-3.5 h-3.5" />
            </button>
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
