import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, RefreshCw, GitBranch, FileText, Database, Globe, Key, Trash2, Sparkles } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/StatusBadge';
import { blueprints as bpApi, settings as settingsApi } from '../lib/api';
import { timeAgo } from '../lib/utils';
import { toast } from 'sonner';
import type { Blueprint, BlueprintResource } from '../types';

const resourceIcons: Record<string, typeof Globe> = {
  service: Globe,
  database: Database,
  keyvalue: Key,
};

const resourceLabels: Record<string, string> = {
  service: 'Service',
  database: 'PostgreSQL',
  keyvalue: 'Key Value',
};

function resourceLink(r: BlueprintResource): string {
  if (r.resource_type === 'service') return `/services/${r.resource_id}`;
  if (r.resource_type === 'database') return `/databases/${r.resource_id}`;
  return `/`;
}

function formatSyncError(syncError: string | null): string | null {
  if (!syncError) return null;
  const lower = syncError.toLowerCase();

  // Hide raw Stripe/internal errors from end-users. Keep it actionable.
  if (lower.includes('payment method required')) {
    return 'Payment method required. Add a payment method in Billing, then try syncing again.';
  }
  if (lower.startsWith('billing error')) {
    return 'Billing error. Open Billing > Plans and try syncing again.';
  }

  if (lower === 'yaml_missing_ai_disabled') {
    return 'No render.yaml found in the repository. Enable Blueprint AI to automatically generate one.';
  }

  // Legacy format: sometimes we stored a JSON-encoded Stripe error blob.
  if (syncError.includes('{"status"') && syncError.includes('"message"')) {
    const m = syncError.match(/"message"\s*:\s*"([^"]+)"/);
    if (m && m[1]) return m[1];
  }

  return syncError;
}

export function BlueprintDetail() {
  const { blueprintId } = useParams<{ blueprintId: string }>();
  const navigate = useNavigate();
  const [bp, setBp] = useState<Blueprint | null>(null);
  const [resources, setResources] = useState<BlueprintResource[]>([]);
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [enablingAI, setEnablingAI] = useState(false);

  const load = useCallback(async () => {
    if (!blueprintId) return;
    try {
      const data = await bpApi.get(blueprintId);
      setBp(data);
      setResources(data.resources || []);
    } catch {
      toast.error('Failed to load blueprint');
    } finally {
      setLoading(false);
    }
  }, [blueprintId]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  // Poll while syncing
  useEffect(() => {
    if (bp?.last_sync_status !== 'syncing') return;
    const interval = setInterval(load, 3000);
    return () => clearInterval(interval);
  }, [bp?.last_sync_status, load]);

  const handleSync = async () => {
    if (!blueprintId) return;
    setSyncing(true);
    try {
      await bpApi.sync(blueprintId);
      toast.success('Sync started');
      setBp((prev) => prev ? { ...prev, last_sync_status: 'syncing' } : prev);
    } catch {
      toast.error('Failed to start sync');
    } finally {
      setSyncing(false);
    }
  };

  const handleDelete = async () => {
    if (!blueprintId) return;
    const svcCount = resources.filter((r) => r.resource_type === 'service').length;
    const msg = svcCount > 0
      ? `Delete this blueprint and ${svcCount} linked service${svcCount === 1 ? '' : 's'}?\n\nServices will be deleted. Databases and key-value stores will remain.`
      : 'Delete this blueprint?';
    if (!confirm(msg)) return;
    setDeleting(true);
    try {
      await bpApi.delete(blueprintId);
      toast.success('Blueprint deleted');
      navigate('/blueprints');
    } catch {
      toast.error('Failed to delete blueprint');
    } finally {
      setDeleting(false);
    }
  };

  const handleEnableAIAndSync = async () => {
    if (!blueprintId) return;
    setEnablingAI(true);
    try {
      const aiStatus = await settingsApi.getBlueprintAI();
      if (!aiStatus.available) {
        toast.error('Blueprint AI is not available — OpenRouter is not configured on this server.');
        return;
      }
      await settingsApi.updateBlueprintAI(true);
      await bpApi.sync(blueprintId);
      toast.success('Blueprint AI enabled — sync started');
      setBp((prev) => prev ? { ...prev, last_sync_status: 'syncing' } : prev);
    } catch {
      toast.error('Failed to enable Blueprint AI');
    } finally {
      setEnablingAI(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20 text-sm text-content-tertiary">
        Loading...
      </div>
    );
  }

  if (!bp) {
    return (
      <div className="text-center py-20 text-sm text-content-secondary">
        Blueprint not found.
      </div>
    );
  }

  const isSyncing = bp.last_sync_status === 'syncing';
  const isFailed = bp.last_sync_status?.startsWith('failed');
  const syncError = isFailed && bp.last_sync_status.includes(': ')
    ? bp.last_sync_status.substring(bp.last_sync_status.indexOf(': ') + 2)
    : null;

  const syncErrorDisplay = formatSyncError(syncError);

  const syncBadgeStatus = bp.last_sync_status === 'synced' ? 'live'
    : bp.last_sync_status === 'syncing' ? 'building'
    : 'failed';

  return (
    <div>
      <button
        onClick={() => navigate('/blueprints')}
        className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        Back to Blueprints
      </button>

      {/* Header */}
      <div className="flex items-start justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold text-content-primary">{bp.name}</h1>
          <div className="flex items-center gap-3 mt-1 text-sm text-content-secondary">
            <span className="flex items-center gap-1">
              <GitBranch className="w-3.5 h-3.5" />
              {bp.branch}
            </span>
            <span className="flex items-center gap-1">
              <FileText className="w-3.5 h-3.5" />
              {bp.file_path}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge status={syncBadgeStatus} size="sm" />
          <Button
            variant="secondary"
            size="sm"
            onClick={handleSync}
            loading={syncing || isSyncing}
            disabled={isSyncing}
          >
            <RefreshCw className="w-3.5 h-3.5" />
            {isSyncing ? 'Syncing...' : 'Sync'}
          </Button>
        </div>
      </div>

      {/* Error banner */}
      {syncError && (
        <Card padding="md" className="mb-4 border-status-error/30 bg-status-error/5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex items-start gap-2 min-w-0">
              <span className="text-status-error text-sm font-medium shrink-0">Sync failed:</span>
              <span className="text-sm text-content-secondary break-words">{syncErrorDisplay}</span>
            </div>
            {(syncError.toLowerCase().includes('payment method') ||
              syncError.toLowerCase().includes('billing error') ||
              syncError.toLowerCase().includes('stripe price') ||
              syncError.toLowerCase().includes('free tier limit')) && (
              <Button
                size="sm"
                onClick={() => {
                  const returnTo = `/blueprints/${encodeURIComponent(bp.id)}`;
                  const qs = new URLSearchParams({
                    source: 'blueprint',
                    blueprint_id: bp.id,
                    return_to: returnTo,
                  });
                  navigate(`/billing/plans?${qs.toString()}`);
                }}
              >
                Upgrade
              </Button>
            )}
            {syncError === 'yaml_missing_ai_disabled' && (
              <Button size="sm" onClick={handleEnableAIAndSync} loading={enablingAI}>
                <Sparkles className="w-3.5 h-3.5" />
                Enable Blueprint AI
              </Button>
            )}
          </div>
        </Card>
      )}

      {/* Info */}
      <Card padding="md" className="mb-4">
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <div className="text-content-tertiary text-xs mb-1">Repository</div>
            <div className="text-content-primary font-mono text-xs break-all">{bp.repo_url}</div>
          </div>
          <div>
            <div className="text-content-tertiary text-xs mb-1">Last Synced</div>
            <div className="text-content-primary">
              {bp.last_synced_at ? timeAgo(bp.last_synced_at) : 'Never'}
            </div>
          </div>
        </div>
      </Card>

      {/* Resources */}
      <h2 className="text-sm font-semibold text-content-primary mt-6 mb-3">
        Resources ({resources.length})
      </h2>

      {resources.length === 0 ? (
        <Card padding="md">
          <p className="text-sm text-content-tertiary text-center py-4">
            {isSyncing ? 'Resources are being provisioned...' : 'No resources created yet. Trigger a sync to provision resources.'}
          </p>
        </Card>
      ) : (
        <div className="space-y-2">
          {resources.map((r) => {
            const Icon = resourceIcons[r.resource_type] || Globe;
            return (
              <Card
                key={r.resource_id}
                hover
                onClick={() => navigate(resourceLink(r))}
                padding="sm"
              >
                <div className="flex items-center gap-3">
                  <Icon className="w-4 h-4 text-content-secondary" />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium text-content-primary">{r.resource_name}</div>
                    <div className="text-xs text-content-tertiary">{resourceLabels[r.resource_type] || r.resource_type}</div>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {/* Danger Zone */}
      <div className="mt-10 pt-6 border-t border-border-default">
        <h2 className="text-sm font-semibold text-status-error mb-3">Danger Zone</h2>
        <Card padding="md">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-medium text-content-primary">Delete Blueprint</div>
              <div className="text-xs text-content-tertiary">
                This will delete the blueprint and any linked services. Databases and key-value stores will remain.
              </div>
            </div>
            <Button variant="danger" size="sm" onClick={handleDelete} loading={deleting}>
              <Trash2 className="w-3.5 h-3.5" />
              Delete
            </Button>
          </div>
        </Card>
      </div>
    </div>
  );
}
