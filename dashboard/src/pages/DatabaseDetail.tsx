import { useState, useEffect } from 'react';
import { useParams, useLocation, useNavigate } from 'react-router-dom';
import {
  Database, Eye, EyeOff, Trash2, AlertTriangle, Loader2,
  BarChart3, Activity, Clock, HardDrive, Download, Plus,
  Shield, Link2, ExternalLink, RefreshCw
} from 'lucide-react';
import { Card } from '../components/ui/Card';
import { CopyButton } from '../components/ui/CopyButton';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Skeleton } from '../components/ui/Skeleton';
import { Button } from '../components/ui/Button';
import { databases as dbApi } from '../lib/api';
import type { ManagedDatabase, Backup } from '../types';

export function DatabaseDetail() {
  const { dbId } = useParams<{ dbId: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const [db, setDb] = useState<ManagedDatabase | null>(null);
  const [loading, setLoading] = useState(true);

  // Determine active tab from path
  const path = location.pathname;
  const tab = path.includes('/metrics') ? 'metrics'
    : path.includes('/access') ? 'access'
    : path.includes('/backups') ? 'backups'
    : path.includes('/apps') ? 'apps'
    : path.includes('/settings') ? 'settings'
    : 'info';

  useEffect(() => {
    if (!dbId) return;
    dbApi.get(dbId)
      .then(setDb)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [dbId]);

  if (loading) {
    return <div className="space-y-4"><Skeleton className="w-48 h-8" /><Skeleton className="w-full h-60" /></div>;
  }

  if (!db) return <div className="text-content-secondary">Database not found</div>;

  return (
    <div>
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <div className="w-10 h-10 rounded-lg bg-[#336791]/15 flex items-center justify-center">
          <Database className="w-5 h-5" style={{ color: '#336791' }} />
        </div>
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold text-content-primary">{db.name}</h1>
            <StatusBadge status={db.status === 'available' ? 'live' : 'created'} />
          </div>
          <div className="text-sm text-content-secondary">
            PostgreSQL {db.pg_version} &middot; {db.plan}
          </div>
        </div>
      </div>

      {/* Tab Content */}
      {tab === 'info' && <InfoTab db={db} />}
      {tab === 'metrics' && <MetricsTab db={db} />}
      {tab === 'access' && <AccessControlTab db={db} />}
      {tab === 'backups' && <BackupsTab db={db} />}
      {tab === 'apps' && <AppsTab db={db} />}
      {tab === 'settings' && <SettingsTab db={db} navigate={navigate} />}
    </div>
  );
}

/* ── Info Tab ──────────────────────────────────────────── */
function InfoTab({ db }: { db: ManagedDatabase }) {
  const [showPassword, setShowPassword] = useState(false);
  const externalStatus = db.external_access || (db.external_url ? 'enabled' : 'disabled');

  const connectionParams = [
    { label: 'Hostname', value: db.host },
    { label: 'Port', value: String(db.port) },
    { label: 'Database', value: db.db_name },
    { label: 'Username', value: db.username },
    { label: 'Password', value: showPassword ? (db.password || 'generated_password') : '●'.repeat(16), rawValue: db.password || 'generated_password', secret: true },
  ];

  return (
    <>
      {/* Connection Details */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Connection Details
        </h2>
        <div className="space-y-3">
          <Card>
            <div className="text-xs font-medium text-content-secondary mb-1.5">Internal Database URL</div>
            <div className="flex items-center gap-2">
              <code className="flex-1 text-xs font-mono text-content-primary bg-surface-tertiary px-3 py-2 rounded-md overflow-x-auto">
                {db.internal_url}
              </code>
              <CopyButton text={db.internal_url} />
            </div>
          </Card>
          <Card>
            <div className="text-xs font-medium text-content-secondary mb-1.5">External Database URL</div>
            <div className="flex items-center gap-2">
              {db.external_url ? (
                <>
                  <code className="flex-1 text-xs font-mono text-content-primary bg-surface-tertiary px-3 py-2 rounded-md overflow-x-auto">
                    {db.external_url}
                  </code>
                  <CopyButton text={db.external_url} />
                </>
              ) : (
                <div className="flex-1 text-xs text-content-tertiary bg-surface-tertiary px-3 py-2 rounded-md">
                  {externalStatus === 'provisioning'
                    ? 'External endpoint is being provisioned. Refresh in a few seconds.'
                    : 'External database access is disabled for this cluster.'}
                </div>
              )}
            </div>
          </Card>
          <Card>
            <div className="text-xs font-medium text-content-secondary mb-1.5">PSQL Command</div>
            <div className="flex items-center gap-2">
              <code className="flex-1 text-xs font-mono text-content-primary bg-surface-tertiary px-3 py-2 rounded-md overflow-x-auto">
                psql {db.internal_url}
              </code>
              <CopyButton text={`psql ${db.internal_url}`} />
            </div>
          </Card>
        </div>
      </div>

      {/* Individual Parameters */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Individual Parameters
        </h2>
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border-default">
                <th className="text-left text-[11px] font-semibold uppercase tracking-wider text-content-tertiary px-4 py-2">Parameter</th>
                <th className="text-left text-[11px] font-semibold uppercase tracking-wider text-content-tertiary px-4 py-2">Value</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {connectionParams.map((param) => (
                <tr key={param.label} className="border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50">
                  <td className="px-4 py-2.5 text-sm text-content-secondary">{param.label}</td>
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      <code className="text-sm font-mono text-content-primary">{param.value}</code>
                      {param.secret && (
                        <button
                          onClick={() => setShowPassword(!showPassword)}
                          className="text-content-tertiary hover:text-content-primary transition-colors"
                        >
                          {showPassword ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                        </button>
                      )}
                    </div>
                  </td>
                  <td className="px-2">
                    <CopyButton text={param.rawValue || param.value} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Instance Info */}
      <div>
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Instance</h2>
        <Card>
          <div className="grid grid-cols-3 gap-4 text-sm">
            <div>
              <div className="text-content-tertiary text-xs mb-0.5">Plan</div>
              <div className="text-content-primary font-medium capitalize">{db.plan}</div>
            </div>
            <div>
              <div className="text-content-tertiary text-xs mb-0.5">PostgreSQL Version</div>
              <div className="text-content-primary font-medium">{db.pg_version}</div>
            </div>
            <div>
              <div className="text-content-tertiary text-xs mb-0.5">Status</div>
              <div className="text-content-primary font-medium capitalize">{db.status}</div>
            </div>
          </div>
        </Card>
      </div>
    </>
  );
}

/* ── Metrics Tab ──────────────────────────────────────── */
function MetricsTab({ db }: { db: ManagedDatabase }) {
  // Mock metrics data for visualization
  const cpuData = Array.from({ length: 24 }, (_, i) => ({ hour: i, value: Math.random() * 30 + 5 }));
  const memData = Array.from({ length: 24 }, (_, i) => ({ hour: i, value: Math.random() * 60 + 20 }));
  const connData = Array.from({ length: 24 }, (_, i) => ({ hour: i, value: Math.floor(Math.random() * 15 + 1) }));
  const diskData = Array.from({ length: 24 }, (_, i) => ({ hour: i, value: Math.random() * 5 + 12 }));

  return (
    <div className="space-y-6">
      <div className="text-xs text-content-tertiary">
        Metrics for <span className="font-mono">{db.name}</span> (placeholder data).
      </div>
      <div className="grid grid-cols-2 gap-4">
        <MetricCard icon={<Activity className="w-4 h-4" />} title="CPU Usage" value="12.4%" subtitle="Average (24h)" data={cpuData} color="#3B82F6" />
        <MetricCard icon={<BarChart3 className="w-4 h-4" />} title="Memory Usage" value="48.2 MB" subtitle="of 256 MB" data={memData} color="#8B5CF6" />
        <MetricCard icon={<Link2 className="w-4 h-4" />} title="Active Connections" value="4" subtitle="of 50 max" data={connData} color="#10B981" />
        <MetricCard icon={<HardDrive className="w-4 h-4" />} title="Disk Usage" value="14.6 MB" subtitle="of 1 GB" data={diskData} color="#F59E0B" />
      </div>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-1">Query Performance</h3>
        <p className="text-xs text-content-tertiary mb-4">Top queries by average execution time (last 24 hours)</p>
        <div className="bg-surface-tertiary rounded-lg overflow-hidden">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border-default text-content-tertiary">
                <th className="text-left px-3 py-2 font-medium">Query</th>
                <th className="text-right px-3 py-2 font-medium">Avg Time</th>
                <th className="text-right px-3 py-2 font-medium">Calls</th>
              </tr>
            </thead>
            <tbody className="text-content-secondary font-mono">
              <tr className="border-b border-border-subtle">
                <td className="px-3 py-2 truncate max-w-[400px]">SELECT * FROM services WHERE workspace_id = $1</td>
                <td className="px-3 py-2 text-right">2.1ms</td>
                <td className="px-3 py-2 text-right">1,247</td>
              </tr>
              <tr className="border-b border-border-subtle">
                <td className="px-3 py-2 truncate max-w-[400px]">INSERT INTO deploys (service_id, ...) VALUES ...</td>
                <td className="px-3 py-2 text-right">4.8ms</td>
                <td className="px-3 py-2 text-right">342</td>
              </tr>
              <tr>
                <td className="px-3 py-2 truncate max-w-[400px]">UPDATE services SET status = $1 WHERE id = $2</td>
                <td className="px-3 py-2 text-right">1.6ms</td>
                <td className="px-3 py-2 text-right">518</td>
              </tr>
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

function MetricCard({ icon, title, value, subtitle, data, color }: {
  icon: React.ReactNode; title: string; value: string; subtitle: string;
  data: { hour: number; value: number }[]; color: string;
}) {
  const max = Math.max(...data.map(d => d.value));
  return (
    <Card>
      <div className="flex items-center gap-2 text-content-secondary mb-3">
        {icon}
        <span className="text-xs font-medium">{title}</span>
      </div>
      <div className="text-2xl font-bold text-content-primary">{value}</div>
      <div className="text-xs text-content-tertiary mb-3">{subtitle}</div>
      <div className="flex items-end gap-[2px] h-12">
        {data.map((d, i) => (
          <div
            key={i}
            className="flex-1 rounded-t-sm transition-all hover:opacity-80"
            style={{ height: `${(d.value / max) * 100}%`, backgroundColor: color, opacity: 0.6 + (d.value / max) * 0.4 }}
          />
        ))}
      </div>
      <div className="flex justify-between mt-1 text-[10px] text-content-tertiary">
        <span>24h ago</span>
        <span>Now</span>
      </div>
    </Card>
  );
}

/* ── Access Control Tab ───────────────────────────────── */
function AccessControlTab({ db }: { db: ManagedDatabase }) {
  const externalStatus = db.external_access || (db.external_url ? 'enabled' : 'disabled');
  const externalEnabled = externalStatus === 'enabled' && Boolean(db.external_url);
  const externalProvisioning = externalStatus === 'provisioning' && !externalEnabled;
  const encryptionStatus = db.encryption_status || (db.encryption_at_rest === true ? 'enabled' : db.encryption_at_rest === false ? 'disabled' : 'unknown');
  const encryptionEnabled = encryptionStatus === 'enabled';

  return (
    <div className="space-y-6">
      <Card>
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-4 h-4 text-brand" />
          <h3 className="text-sm font-semibold text-content-primary">Access Configuration</h3>
        </div>
        <div className="space-y-4">
          <div className="flex items-center justify-between p-3 bg-surface-tertiary rounded-lg">
            <div>
              <div className="text-sm font-medium text-content-primary">Internal Access</div>
              <div className="text-xs text-content-tertiary mt-0.5">Accessible from services in this workspace</div>
            </div>
            <StatusBadge status="live" />
          </div>
          <div className="flex items-center justify-between p-3 bg-surface-tertiary rounded-lg">
            <div>
              <div className="text-sm font-medium text-content-primary">External Access</div>
              <div className="text-xs text-content-tertiary mt-0.5">
                {externalEnabled
                  ? 'Reachable from outside the cluster with TLS required (sslmode=require)'
                  : externalProvisioning
                    ? 'External endpoint is being provisioned'
                    : 'Disabled for this cluster'}
              </div>
            </div>
            <StatusBadge status={externalEnabled ? 'live' : 'created'} pulse={externalProvisioning} />
          </div>
          <div className="flex items-center justify-between p-3 bg-surface-tertiary rounded-lg">
            <div>
              <div className="text-sm font-medium text-content-primary">Encryption at Rest</div>
              <div className="text-xs text-content-tertiary mt-0.5">
                {encryptionStatus === 'enabled'
                  ? `Enabled (${db.encryption_algorithm || 'platform default'}; ${db.key_management || 'platform-managed'})`
                  : encryptionStatus === 'disabled'
                    ? `Not configured at storage class level${db.storage_class ? ` (${db.storage_class})` : ''}`
                    : 'Encryption posture could not be determined from current storage-class metadata'}
              </div>
            </div>
            <StatusBadge status={encryptionEnabled ? 'live' : 'created'} pulse={false} />
          </div>
        </div>
      </Card>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-3">Allowed IP Addresses</h3>
        <p className="text-xs text-content-tertiary mb-4">
          {externalEnabled
            ? 'Per-database IP allowlisting is not yet available. Use network/firewall controls and rotate credentials regularly.'
            : externalProvisioning
              ? 'External endpoint provisioning is in progress. IP allowlisting controls will be added in a future release.'
              : 'External access is disabled for this cluster.'}
        </p>
        <div className="p-8 text-center border border-dashed border-border-default rounded-lg">
          <Shield className="w-8 h-8 text-content-tertiary mx-auto mb-2" />
          <div className="text-sm text-content-secondary">
            {externalEnabled ? 'IP allowlisting coming soon' : externalProvisioning ? 'External access provisioning' : 'External access disabled'}
          </div>
          <div className="text-xs text-content-tertiary mt-1">
            {externalEnabled
              ? 'For now, restrict access using host/network firewall rules.'
              : 'IP allowlisting controls will appear here when available'}
          </div>
        </div>
      </Card>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-3">Database Users</h3>
        <div className="bg-surface-tertiary rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border-default text-content-tertiary text-xs">
                <th className="text-left px-3 py-2 font-medium">Username</th>
                <th className="text-left px-3 py-2 font-medium">Role</th>
                <th className="text-left px-3 py-2 font-medium">Created</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td className="px-3 py-2.5 font-mono text-content-primary">{db.username}</td>
                <td className="px-3 py-2.5 text-content-secondary">Owner</td>
                <td className="px-3 py-2.5 text-content-tertiary">{new Date(db.created_at).toLocaleDateString()}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

/* ── Backups Tab ──────────────────────────────────────── */
function BackupsTab({ db }: { db: ManagedDatabase }) {
  const [backups, setBackups] = useState<Backup[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    dbApi.listBackups(db.id)
      .then(setBackups)
      .catch(() => { /* ignore */ })
      .finally(() => setLoading(false));
  }, [db.id]);

  const handleCreateBackup = async () => {
    setCreating(true);
    try {
      const backup = await dbApi.triggerBackup(db.id);
      setBackups(prev => [backup, ...prev]);
    } catch { /* ignore */ }
    setCreating(false);
  };

  return (
    <div className="space-y-6">
      <Card>
        <div className="flex items-center justify-between mb-4">
          <div>
            <h3 className="text-sm font-semibold text-content-primary">Backups</h3>
            <p className="text-xs text-content-tertiary mt-0.5">Create on-demand database snapshots from the dashboard</p>
          </div>
          <Button size="sm" onClick={handleCreateBackup} disabled={creating}>
            {creating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
            <span className="ml-1.5">Create Backup</span>
          </Button>
        </div>

        {loading ? (
          <div className="space-y-2">
            <Skeleton className="w-full h-10" />
            <Skeleton className="w-full h-10" />
          </div>
        ) : backups.length === 0 ? (
            <div className="p-8 text-center border border-dashed border-border-default rounded-lg">
              <Download className="w-8 h-8 text-content-tertiary mx-auto mb-2" />
              <div className="text-sm text-content-secondary">No backups yet</div>
            <div className="text-xs text-content-tertiary mt-1">Create your first backup to enable restore operations</div>
            </div>
        ) : (
          <div className="bg-surface-tertiary rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border-default text-content-tertiary text-xs">
                  <th className="text-left px-3 py-2 font-medium">Created</th>
                  <th className="text-left px-3 py-2 font-medium">Size</th>
                  <th className="text-left px-3 py-2 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {backups.map(b => (
                  <tr key={b.id} className="border-b border-border-subtle last:border-0">
                    <td className="px-3 py-2.5 text-content-primary">
                      <div className="flex items-center gap-2">
                        <Clock className="w-3.5 h-3.5 text-content-tertiary" />
                        {new Date(b.started_at).toLocaleString()}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-content-secondary font-mono">
                      {b.size_bytes > 0 ? `${(b.size_bytes / 1024).toFixed(1)} KB` : '—'}
                    </td>
                    <td className="px-3 py-2.5">
                      <StatusBadge status={b.status === 'completed' ? 'live' : 'building'} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-1">Recovery</h3>
        <p className="text-xs text-content-tertiary mb-3">Restore your database from a backup point-in-time</p>
        <div className="p-4 bg-surface-tertiary rounded-lg text-xs text-content-secondary">
          To restore from a backup, use the PSQL command with the backup file. Automated point-in-time recovery is not yet available in this UI.
        </div>
      </Card>
    </div>
  );
}

/* ── Apps Tab ─────────────────────────────────────────── */
function AppsTab({ db }: { db: ManagedDatabase }) {
  return (
    <div className="space-y-6">
      <Card>
        <div className="flex items-center gap-2 mb-4">
          <ExternalLink className="w-4 h-4 text-brand" />
          <h3 className="text-sm font-semibold text-content-primary">Connected Services</h3>
        </div>
        <p className="text-xs text-content-tertiary mb-4">
          Services that reference this database via environment variables or blueprint connections.
        </p>
        <div className="p-8 text-center border border-dashed border-border-default rounded-lg">
          <Link2 className="w-8 h-8 text-content-tertiary mx-auto mb-2" />
          <div className="text-sm text-content-secondary">No connected services</div>
          <div className="text-xs text-content-tertiary mt-1">
            Connect a service by adding the database URL as an environment variable
          </div>
        </div>
      </Card>

      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-2">How to Connect</h3>
        <div className="bg-surface-tertiary rounded-lg p-3 font-mono text-xs text-content-secondary">
          <div className="text-content-tertiary mb-1"># Add as environment variable to your service:</div>
          <div>DATABASE_URL={db.internal_url}</div>
        </div>
      </Card>
    </div>
  );
}

/* ── Settings Tab ─────────────────────────────────────── */
function SettingsTab({ db, navigate }: { db: ManagedDatabase; navigate: (path: string) => void }) {
  const [confirmName, setConfirmName] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [showDelete, setShowDelete] = useState(false);

  const handleDelete = async () => {
    if (confirmName !== db.name) return;
    setDeleting(true);
    try {
      await dbApi.delete(db.id);
      navigate('/');
    } catch {
      setDeleting(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* General Settings */}
      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-4">General</h3>
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm text-content-primary">Database Name</div>
              <div className="text-xs text-content-tertiary">{db.name}</div>
            </div>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm text-content-primary">Plan</div>
              <div className="text-xs text-content-tertiary capitalize">{db.plan}</div>
            </div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => navigate(`/billing/plans?focus=database&resource_id=${encodeURIComponent(db.id)}`)}
            >
              Change Plan
            </Button>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm text-content-primary">PostgreSQL Version</div>
              <div className="text-xs text-content-tertiary">{db.pg_version}</div>
            </div>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm text-content-primary">Created</div>
              <div className="text-xs text-content-tertiary">{new Date(db.created_at).toLocaleString()}</div>
            </div>
          </div>
        </div>
      </Card>

      {/* Maintenance */}
      <Card>
        <h3 className="text-sm font-semibold text-content-primary mb-1">Maintenance</h3>
        <p className="text-xs text-content-tertiary mb-4">Perform maintenance operations on your database</p>
        <div className="flex gap-3">
          <Button size="sm" variant="secondary">
            <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
            Restart Database
          </Button>
        </div>
      </Card>

      {/* Danger Zone */}
      <div className="border border-status-error/20 rounded-lg overflow-hidden">
        <div className="px-5 py-3 bg-status-error/5 border-b border-status-error/20">
          <h3 className="text-sm font-semibold text-status-error flex items-center gap-2">
            <AlertTriangle className="w-4 h-4" />
            Danger Zone
          </h3>
        </div>
        <div className="p-5">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-medium text-content-primary">Delete this database</div>
              <div className="text-xs text-content-tertiary mt-0.5">
                Deleting now soft-deletes this database with a 72-hour recovery window before hard delete is allowed.
              </div>
            </div>
            <Button
              size="sm"
              className="bg-status-error hover:bg-red-600 text-white border-0 shrink-0"
              onClick={() => setShowDelete(true)}
            >
              <Trash2 className="w-3.5 h-3.5 mr-1.5" />
              Delete Database
            </Button>
          </div>

          {showDelete && (
            <div className="mt-4 p-4 bg-surface-tertiary rounded-lg border border-border-default">
              <p className="text-sm text-content-secondary mb-3">
                Type <code className="px-1.5 py-0.5 bg-surface-primary rounded text-status-error font-mono text-xs">{db.name}</code> to confirm deletion:
              </p>
              <div className="flex gap-3">
                <input
                  type="text"
                  value={confirmName}
                  onChange={(e) => setConfirmName(e.target.value)}
                  placeholder={db.name}
                  className="flex-1 h-9 px-3 rounded-lg bg-surface-primary border border-border-default text-content-primary text-sm focus:outline-none focus:ring-2 focus:ring-status-error/30 focus:border-status-error placeholder:text-content-tertiary"
                />
                <Button
                  size="sm"
                  className="bg-status-error hover:bg-red-600 text-white border-0"
                  disabled={confirmName !== db.name || deleting}
                  onClick={handleDelete}
                >
                  {deleting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : 'Move to Trash'}
                </Button>
                <Button size="sm" variant="secondary" onClick={() => { setShowDelete(false); setConfirmName(''); }}>
                  Cancel
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
