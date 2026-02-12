import { useState, useEffect } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import {
  Globe2, ArrowLeft, Plus, Pencil, Trash2, Lock, RefreshCw,
  Settings, List, Info, ToggleLeft, ToggleRight, AlertTriangle
} from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Select } from '../components/ui/Select';
import { registeredDomains, dnsRecords } from '../lib/api';
import type { RegisteredDomain, DnsRecord, DnsRecordType } from '../types';
import { toast } from 'sonner';

const recordTypeColors: Record<string, string> = {
  A: '#3b82f6',
  AAAA: '#6366f1',
  CNAME: '#8b5cf6',
  MX: '#ec4899',
  TXT: '#f59e0b',
  NS: '#10b981',
  SRV: '#06b6d4',
  CAA: '#f97316',
};

const tabs = [
  { key: 'overview', label: 'Overview', icon: Info },
  { key: 'dns', label: 'DNS Records', icon: List },
  { key: 'settings', label: 'Settings', icon: Settings },
];

export function DomainDetail() {
  const { domainId } = useParams<{ domainId: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const [domain, setDomain] = useState<RegisteredDomain | null>(null);
  const [records, setRecords] = useState<DnsRecord[]>([]);
  const [loading, setLoading] = useState(true);

  // Determine active tab from path
  const pathEnd = location.pathname.split('/').pop();
  const activeTab = pathEnd === 'dns' ? 'dns' : pathEnd === 'settings' ? 'settings' : 'overview';

  useEffect(() => {
    if (!domainId) return;
    Promise.all([
      registeredDomains.get(domainId),
      dnsRecords.list(domainId),
    ])
      .then(([d, r]) => {
        setDomain(d);
        setRecords(r);
      })
      .catch(() => toast.error('Failed to load domain'))
      .finally(() => setLoading(false));
  }, [domainId]);

  if (loading) {
    return (
      <div>
        <div className="h-8 w-48 bg-surface-tertiary rounded animate-pulse mb-6" />
        <div className="h-48 bg-surface-secondary border border-border-default rounded-lg animate-pulse" />
      </div>
    );
  }

  if (!domain) {
    return <div className="text-content-secondary">Domain not found.</div>;
  }

  const basePath = `/domains/${domainId}`;

  return (
    <div>
      {/* Header */}
      <div className="flex items-center gap-3 mb-2">
        <button
          onClick={() => navigate('/domains')}
          className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <Globe2 className="w-5 h-5 text-content-tertiary" />
        <h1 className="text-2xl font-semibold text-content-primary">{domain.domain_name}</h1>
      </div>
      <p className="text-sm text-content-tertiary mb-6 ml-[52px]">
        Registered via {domain.provider} &middot; {domain.status}
      </p>

      {/* Tabs */}
      <div className="flex gap-1 mb-6 border-b border-border-subtle">
        {tabs.map((t) => {
          const Icon = t.icon;
          const isActive = activeTab === t.key;
          const path = t.key === 'overview' ? basePath : `${basePath}/${t.key}`;
          return (
            <button
              key={t.key}
              onClick={() => navigate(path)}
              className={`flex items-center gap-1.5 px-3 py-2 text-sm font-medium border-b-2 transition-colors -mb-[1px] ${
                isActive
                  ? 'border-brand text-brand'
                  : 'border-transparent text-content-tertiary hover:text-content-primary'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              {t.label}
            </button>
          );
        })}
      </div>

      {/* Tab content */}
      {activeTab === 'overview' && <OverviewTab domain={domain} records={records} />}
      {activeTab === 'dns' && <DnsTab domainId={domainId!} records={records} setRecords={setRecords} />}
      {activeTab === 'settings' && <SettingsTab domain={domain} setDomain={setDomain} />}
    </div>
  );
}

function OverviewTab({ domain, records }: { domain: RegisteredDomain; records: DnsRecord[] }) {
  const statusColor = domain.status === 'active' ? '#10b981' : domain.status === 'expired' ? '#ef4444' : '#f59e0b';

  return (
    <div className="space-y-6">
      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-4">Domain Information</h3>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <div className="text-xs text-content-tertiary mb-1">Domain</div>
            <div className="text-sm text-content-primary font-medium">{domain.domain_name}</div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Status</div>
            <span
              className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium"
              style={{ color: statusColor, backgroundColor: statusColor + '15' }}
            >
              <span className="w-1.5 h-1.5 rounded-full" style={{ backgroundColor: statusColor }} />
              {domain.status.charAt(0).toUpperCase() + domain.status.slice(1)}
            </span>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Registrar</div>
            <div className="text-sm text-content-primary">{domain.provider}</div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Expires</div>
            <div className="text-sm text-content-primary">
              {domain.expires_at ? new Date(domain.expires_at).toLocaleDateString() : 'N/A'}
            </div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Auto-Renew</div>
            <div className="text-sm text-content-primary">{domain.auto_renew ? 'Enabled' : 'Disabled'}</div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">WHOIS Privacy</div>
            <div className="text-sm text-content-primary">{domain.whois_privacy ? 'Enabled' : 'Disabled'}</div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Registered</div>
            <div className="text-sm text-content-primary">{new Date(domain.created_at).toLocaleDateString()}</div>
          </div>
          <div>
            <div className="text-xs text-content-tertiary mb-1">Cost</div>
            <div className="text-sm text-content-primary">${(domain.cost_cents / 100).toFixed(2)}/yr</div>
          </div>
        </div>
      </Card>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-3">DNS Records Summary</h3>
        <div className="text-sm text-content-secondary">
          {records.length} record{records.length !== 1 && 's'} configured
        </div>
        <div className="flex flex-wrap gap-2 mt-2">
          {Object.entries(
            records.reduce<Record<string, number>>((acc, r) => {
              acc[r.record_type] = (acc[r.record_type] || 0) + 1;
              return acc;
            }, {})
          ).map(([type, count]) => (
            <span
              key={type}
              className="text-xs font-medium px-2 py-0.5 rounded"
              style={{ color: recordTypeColors[type] || '#6b7280', backgroundColor: (recordTypeColors[type] || '#6b7280') + '15' }}
            >
              {type}: {count}
            </span>
          ))}
        </div>
      </Card>
    </div>
  );
}

function DnsTab({
  domainId,
  records,
  setRecords,
}: {
  domainId: string;
  records: DnsRecord[];
  setRecords: (r: DnsRecord[]) => void;
}) {
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [formType, setFormType] = useState<DnsRecordType>('A');
  const [formName, setFormName] = useState('');
  const [formValue, setFormValue] = useState('');
  const [formTTL, setFormTTL] = useState('3600');
  const [formPriority, setFormPriority] = useState('0');
  const [saving, setSaving] = useState(false);

  const resetForm = () => {
    setFormType('A' as DnsRecordType);
    setFormName('');
    setFormValue('');
    setFormTTL('3600');
    setFormPriority('0');
    setShowForm(false);
    setEditingId(null);
  };

  const startEdit = (rec: DnsRecord) => {
    setEditingId(rec.id);
    setFormType(rec.record_type as DnsRecordType);
    setFormName(rec.name);
    setFormValue(rec.value);
    setFormTTL(String(rec.ttl));
    setFormPriority(String(rec.priority));
    setShowForm(true);
  };

  const save = async () => {
    setSaving(true);
    try {
      const data = {
        record_type: formType,
        name: formName,
        value: formValue,
        ttl: parseInt(formTTL) || 3600,
        priority: parseInt(formPriority) || 0,
      };
      if (editingId) {
        const updated = await dnsRecords.update(domainId, editingId, data);
        setRecords(records.map((r) => (r.id === editingId ? updated : r)));
        toast.success('Record updated');
      } else {
        const created = await dnsRecords.create(domainId, data);
        setRecords([...records, created]);
        toast.success('Record created');
      }
      resetForm();
    } catch {
      toast.error('Failed to save record');
    } finally {
      setSaving(false);
    }
  };

  const deleteRecord = async (id: string) => {
    try {
      await dnsRecords.delete(domainId, id);
      setRecords(records.filter((r) => r.id !== id));
      toast.success('Record deleted');
    } catch {
      toast.error('Failed to delete record');
    }
  };

  const showPriority = formType === 'MX' || formType === 'SRV';

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-content-primary">DNS Records</h3>
        {!showForm && (
          <Button size="sm" onClick={() => { resetForm(); setShowForm(true); }}>
            <Plus className="w-3.5 h-3.5" />
            Add Record
          </Button>
        )}
      </div>

      {showForm && (
        <Card>
          <h4 className="text-sm font-medium text-content-primary mb-3">
            {editingId ? 'Edit Record' : 'New Record'}
          </h4>
          <div className="grid grid-cols-12 gap-3">
            <div className="col-span-2">
              <Select
                label="Type"
                value={formType}
                onChange={(e) => setFormType(e.target.value as DnsRecordType)}
                options={['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'].map((t) => ({
                  value: t,
                  label: t,
                }))}
              />
            </div>
            <div className={showPriority ? 'col-span-3' : 'col-span-4'}>
              <Input label="Name" placeholder="@" value={formName} onChange={(e) => setFormName(e.target.value)} />
            </div>
            <div className={showPriority ? 'col-span-3' : 'col-span-4'}>
              <Input label="Value" placeholder="192.168.1.1" value={formValue} onChange={(e) => setFormValue(e.target.value)} />
            </div>
            <div className="col-span-2">
              <Input label="TTL" type="number" value={formTTL} onChange={(e) => setFormTTL(e.target.value)} />
            </div>
            {showPriority && (
              <div className="col-span-2">
                <Input label="Priority" type="number" value={formPriority} onChange={(e) => setFormPriority(e.target.value)} />
              </div>
            )}
          </div>
          <div className="flex items-center gap-2 mt-4">
            <Button size="sm" onClick={save} loading={saving}>
              {editingId ? 'Update' : 'Create'}
            </Button>
            <Button size="sm" variant="ghost" onClick={resetForm}>
              Cancel
            </Button>
          </div>
        </Card>
      )}

      {records.length > 0 ? (
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          <div className="px-4 py-2.5 border-b border-border-subtle">
            <div className="grid grid-cols-12 text-xs font-medium text-content-tertiary uppercase tracking-wider">
              <div className="col-span-1">Type</div>
              <div className="col-span-3">Name</div>
              <div className="col-span-4">Value</div>
              <div className="col-span-1">TTL</div>
              <div className="col-span-1">Priority</div>
              <div className="col-span-2 text-right">Actions</div>
            </div>
          </div>
          {records.map((rec) => (
            <div
              key={rec.id}
              className="px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50 transition-colors"
            >
              <div className="grid grid-cols-12 items-center">
                <div className="col-span-1">
                  <span
                    className="text-[11px] font-bold px-1.5 py-0.5 rounded"
                    style={{
                      color: recordTypeColors[rec.record_type] || '#6b7280',
                      backgroundColor: (recordTypeColors[rec.record_type] || '#6b7280') + '15',
                    }}
                  >
                    {rec.record_type}
                  </span>
                </div>
                <div className="col-span-3 text-sm text-content-primary font-mono truncate">{rec.name}</div>
                <div className="col-span-4 text-sm text-content-secondary font-mono truncate">{rec.value}</div>
                <div className="col-span-1 text-sm text-content-tertiary">{rec.ttl}</div>
                <div className="col-span-1 text-sm text-content-tertiary">{rec.priority || '-'}</div>
                <div className="col-span-2 flex items-center justify-end gap-1">
                  {rec.managed ? (
                    <span className="flex items-center gap-1 text-xs text-content-tertiary">
                      <Lock className="w-3 h-3" />
                      Managed
                    </span>
                  ) : (
                    <>
                      <button
                        onClick={() => startEdit(rec)}
                        className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
                      >
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => deleteRecord(rec.id)}
                        className="p-1.5 rounded-md text-content-tertiary hover:text-status-error hover:bg-status-error-bg transition-colors"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <Card>
          <p className="text-sm text-content-tertiary text-center py-4">No DNS records configured.</p>
        </Card>
      )}
    </div>
  );
}

function SettingsTab({
  domain,
  setDomain,
}: {
  domain: RegisteredDomain;
  setDomain: (d: RegisteredDomain) => void;
}) {
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const [renewing, setRenewing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const toggle = async (field: 'auto_renew' | 'whois_privacy' | 'locked') => {
    setSaving(true);
    try {
      const updated = await registeredDomains.update(domain.id, { [field]: !domain[field] });
      setDomain(updated);
      toast.success('Setting updated');
    } catch {
      toast.error('Failed to update setting');
    } finally {
      setSaving(false);
    }
  };

  const renew = async () => {
    setRenewing(true);
    try {
      const updated = await registeredDomains.renew(domain.id);
      setDomain(updated);
      toast.success('Domain renewed successfully');
    } catch {
      toast.error('Renewal failed');
    } finally {
      setRenewing(false);
    }
  };

  const deleteDomain = async () => {
    if (!confirm('Are you sure you want to cancel this domain? This cannot be undone.')) return;
    setDeleting(true);
    try {
      await registeredDomains.delete(domain.id);
      toast.success('Domain cancelled');
      navigate('/domains');
    } catch {
      toast.error('Failed to cancel domain');
    } finally {
      setDeleting(false);
    }
  };

  const toggleItems = [
    {
      field: 'auto_renew' as const,
      label: 'Auto-Renew',
      description: 'Automatically renew this domain before it expires.',
      value: domain.auto_renew,
    },
    {
      field: 'whois_privacy' as const,
      label: 'WHOIS Privacy',
      description: 'Hide your personal information from WHOIS lookups.',
      value: domain.whois_privacy,
    },
    {
      field: 'locked' as const,
      label: 'Transfer Lock',
      description: 'Prevent unauthorized transfers of this domain.',
      value: domain.locked,
    },
  ];

  return (
    <div className="space-y-6">
      {/* Toggle settings */}
      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-4">Domain Settings</h3>
        <div className="space-y-4">
          {toggleItems.map((item) => (
            <div key={item.field} className="flex items-center justify-between">
              <div>
                <div className="text-sm font-medium text-content-primary">{item.label}</div>
                <div className="text-xs text-content-tertiary">{item.description}</div>
              </div>
              <button
                onClick={() => toggle(item.field)}
                disabled={saving}
                className="text-content-tertiary hover:text-content-primary transition-colors disabled:opacity-50"
              >
                {item.value ? (
                  <ToggleRight className="w-8 h-8 text-brand" />
                ) : (
                  <ToggleLeft className="w-8 h-8" />
                )}
              </button>
            </div>
          ))}
        </div>
      </Card>

      {/* Renew */}
      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-2">Renew Domain</h3>
        <p className="text-xs text-content-tertiary mb-3">
          Extend this domain's registration by 1 year. Cost: ${(domain.cost_cents / 100).toFixed(2)}
        </p>
        <Button size="sm" variant="secondary" onClick={renew} loading={renewing}>
          <RefreshCw className="w-3.5 h-3.5" />
          Renew Now
        </Button>
      </Card>

      {/* Danger zone */}
      <Card className="border-status-error/30">
        <div className="flex items-start gap-3">
          <AlertTriangle className="w-5 h-5 text-status-error flex-shrink-0 mt-0.5" />
          <div>
            <h3 className="text-sm font-semibold text-content-primary mb-1">Danger Zone</h3>
            <p className="text-xs text-content-tertiary mb-3">
              Cancelling this domain will stop it from renewing and release it after expiration.
            </p>
            <Button size="sm" variant="danger" onClick={deleteDomain} loading={deleting}>
              Cancel Domain
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}
