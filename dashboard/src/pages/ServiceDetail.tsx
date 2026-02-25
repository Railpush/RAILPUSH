import { useState, useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ExternalLink, RotateCw, GitBranch, ChevronDown, ShieldCheck, Clock, Activity, Box, Settings, Copy, Check, Loader2 } from 'lucide-react';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Dropdown } from '../components/ui/Dropdown';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { deriveDeployAutomationState, parseWorkflowNames, type DeployAutomationMode } from '../lib/deployAutomation';
import { serviceTypeLabel, timeAgo, formatDuration } from '../lib/utils';
import { buildDefaultServiceUrl } from '../lib/serviceUrl';
import { services as servicesApi, deploys as deploysApi, envVars as envVarsApi, connectBuildStream } from '../lib/api';
import type { Service, Deploy } from '../types';
import { toast } from 'sonner';

interface QuickMetrics { cpu_percent: string; memory_used: string; memory_percent: string }

function CopyUrlButton({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => { navigator.clipboard.writeText(url); setCopied(true); setTimeout(() => setCopied(false), 1500); }}
      className="p-1 rounded hover:bg-surface-tertiary/50 text-content-tertiary hover:text-content-secondary transition-colors"
      title="Copy URL"
    >
      {copied ? <Check className="w-3.5 h-3.5 text-green-400" /> : <Copy className="w-3.5 h-3.5" />}
    </button>
  );
}

export function ServiceDetail() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const navigate = useNavigate();
  const [service, setService] = useState<Service | null>(null);
  const [deployList, setDeployList] = useState<Deploy[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionInProgress, setActionInProgress] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<QuickMetrics | null>(null);
  const [buildLines, setBuildLines] = useState<string[]>([]);
  const [deployAutomationMode, setDeployAutomationMode] = useState<DeployAutomationMode>('push');
  const [deployAutomationWorkflows, setDeployAutomationWorkflows] = useState<string[]>([]);
  const [workflowModalOpen, setWorkflowModalOpen] = useState(false);
  const [workflowDraft, setWorkflowDraft] = useState('');
  const [workflowSaving, setWorkflowSaving] = useState(false);
  const [knownGitHubWorkflows, setKnownGitHubWorkflows] = useState<string[]>([]);
  const [loadingGitHubWorkflows, setLoadingGitHubWorkflows] = useState(false);
  const [githubWorkflowLoadError, setGitHubWorkflowLoadError] = useState<string | null>(null);
  const buildWsRef = useRef<WebSocket | null>(null);
  const buildLogEndRef = useRef<HTMLDivElement>(null);

  const loadGitHubWorkflows = async (targetServiceId?: string) => {
    const id = (targetServiceId || '').trim();
    if (!id) {
      setKnownGitHubWorkflows([]);
      setGitHubWorkflowLoadError(null);
      setLoadingGitHubWorkflows(false);
      return;
    }

    setLoadingGitHubWorkflows(true);
    setGitHubWorkflowLoadError(null);
    try {
      const workflows = await servicesApi.listGitHubWorkflows(id);
      const seen = new Set<string>();
      const names = workflows
        .map((w) => (w.name || '').trim())
        .filter(Boolean)
        .filter((name) => {
          const key = name.toLowerCase();
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        })
        .sort((a, b) => a.localeCompare(b));
      setKnownGitHubWorkflows(names);
      setGitHubWorkflowLoadError(null);
    } catch {
      setKnownGitHubWorkflows([]);
      setGitHubWorkflowLoadError('Unable to load workflow names from GitHub');
    }
    setLoadingGitHubWorkflows(false);
  };

  const refresh = () => {
    if (!serviceId) return;
    Promise.all([
      servicesApi.get(serviceId).catch(() => null),
      deploysApi.list(serviceId).catch(() => []),
      envVarsApi.list(serviceId).catch(() => null),
    ]).then(([s, d, env]) => {
      setService(s);
      setDeployList(d);
      if (s) {
        const state = deriveDeployAutomationState(Boolean(s.auto_deploy), env || []);
        setDeployAutomationMode(state.mode);
        setDeployAutomationWorkflows(state.workflows);
        void loadGitHubWorkflows(s.id);
      } else {
        setDeployAutomationMode('push');
        setDeployAutomationWorkflows([]);
        setKnownGitHubWorkflows([]);
        setGitHubWorkflowLoadError(null);
        setLoadingGitHubWorkflows(false);
      }
      setLoading(false);
    });
    // Fetch real metrics (best-effort)
    fetch(`/api/v1/services/${serviceId}/metrics`, { credentials: 'include' })
      .then(r => r.ok ? r.json() : null)
      .then(m => { if (m) setMetrics(m); })
      .catch(() => {});
  };

  useEffect(() => { refresh(); }, [serviceId]);

  // Real-time build log streaming via WebSocket
  const latestDeployForWs = deployList[0];
  const isBuildInProgress = latestDeployForWs && ['pending', 'building', 'deploying'].includes(latestDeployForWs.status);
  useEffect(() => {
    if (!isBuildInProgress || !latestDeployForWs) return;
    setBuildLines([]);
    const ws = connectBuildStream(latestDeployForWs.id, (line: string) => {
      setBuildLines(prev => [...prev, line]);
      buildLogEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    });
    buildWsRef.current = ws;
    ws.onclose = () => {
      // Build finished — refresh to get final state
      setTimeout(refresh, 1500);
    };
    return () => { ws.close(); buildWsRef.current = null; };
  }, [latestDeployForWs?.id, isBuildInProgress]);

  if (loading) {
    return (
      <div className="space-y-6 animate-pulse">
        <div className="h-20 bg-surface-tertiary rounded-xl" />
        <div className="grid grid-cols-3 gap-6">
          <div className="h-40 bg-surface-tertiary rounded-xl col-span-2" />
          <div className="h-40 bg-surface-tertiary rounded-xl" />
        </div>
      </div>
    );
  }

  if (!service) {
    return <div className="text-content-secondary flex items-center justify-center h-64">Service not found</div>;
  }

  const latestDeploy = deployList[0];
  const serviceUrl = buildDefaultServiceUrl(service);
  const deployAutomationLabel = deployAutomationMode === 'off'
    ? 'Manual deploys'
    : (deployAutomationMode === 'workflow_success' ? 'GitHub Actions gate' : 'Auto deploy on commit');
  const deployAutomationTone = deployAutomationMode === 'off'
    ? 'bg-surface-tertiary/20 border-border-default/50 text-content-tertiary'
    : (deployAutomationMode === 'workflow_success'
      ? 'bg-status-info/10 border-status-info/30 text-status-info'
      : 'bg-status-success/10 border-status-success/30 text-status-success');
  const deployAutomationDotTone = deployAutomationMode === 'off'
    ? 'bg-content-tertiary'
    : (deployAutomationMode === 'workflow_success' ? 'bg-status-info' : 'bg-status-success');
  const deployAutomationTitle = deployAutomationMode === 'workflow_success' && deployAutomationWorkflows.length > 0
    ? `Allowed workflows: ${deployAutomationWorkflows.join(', ')}`
    : undefined;
  const draftWorkflowNames = parseWorkflowNames(workflowDraft);
  const knownWorkflowSet = new Set(knownGitHubWorkflows.map((w) => w.toLowerCase()));
  const unknownDraftWorkflows = knownGitHubWorkflows.length > 0
    ? draftWorkflowNames.filter((name) => !knownWorkflowSet.has(name.toLowerCase()))
    : [];

  const runAction = async (label: string, action: () => Promise<unknown>) => {
    setActionInProgress(label);
    try {
      await action();
      setTimeout(refresh, 1000);
    } catch { /* ignore */ }
    setActionInProgress(null);
  };

  const setDeployAutomationModeQuick = async (nextMode: DeployAutomationMode) => {
    if (!serviceId || nextMode === deployAutomationMode) return;

    const workflowNames = nextMode === 'workflow_success' ? deployAutomationWorkflows : [];
    const autoDeploy = nextMode !== 'off';

    setActionInProgress('Updating automation…');
    try {
      await servicesApi.update(serviceId, { auto_deploy: autoDeploy });

      const envVars = nextMode === 'workflow_success'
        ? [
            { key: 'RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY', value: 'true', is_secret: false },
            ...(workflowNames.length > 0
              ? [{ key: 'RAILPUSH_GITHUB_ACTIONS_WORKFLOWS', value: workflowNames.join(', '), is_secret: false }]
              : []),
          ]
        : [];

      const deleteKeys = [
        'RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY',
        'RAILPUSH_GITHUB_ACTIONS_ENABLED',
        'RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS',
        'RAILPUSH_GITHUB_ACTIONS_WORKFLOW',
      ];
      if (!(nextMode === 'workflow_success' && workflowNames.length > 0)) {
        deleteKeys.push('RAILPUSH_GITHUB_ACTIONS_WORKFLOWS');
      }

      await envVarsApi.merge(serviceId, { env_vars: envVars, delete: deleteKeys });

      setDeployAutomationMode(nextMode);
      if (nextMode !== 'workflow_success') {
        setDeployAutomationWorkflows([]);
      }

      toast.success(
        nextMode === 'off'
          ? 'Deploy automation set to manual only'
          : (nextMode === 'push' ? 'Deploy automation set to on commit' : 'Deploy automation set to GitHub Actions gate')
      );
      setTimeout(refresh, 600);
    } catch {
      toast.error('Failed to update deploy automation mode');
    }

    setActionInProgress(null);
  };

  const openWorkflowAllowlistModal = () => {
    if (deployAutomationMode !== 'workflow_success') {
      toast.info('Switch to "After GitHub Actions Success" to configure workflow allowlist');
      return;
    }
    setWorkflowDraft(deployAutomationWorkflows.join(', '));
    setWorkflowModalOpen(true);
    if (knownGitHubWorkflows.length === 0 && !loadingGitHubWorkflows && !githubWorkflowLoadError) {
      void loadGitHubWorkflows(service?.id || serviceId);
    }
  };

  const saveWorkflowAllowlist = async () => {
    if (!serviceId) return;
    const workflowNames = parseWorkflowNames(workflowDraft);
    setWorkflowSaving(true);
    try {
      const envVars = workflowNames.length > 0
        ? [{ key: 'RAILPUSH_GITHUB_ACTIONS_WORKFLOWS', value: workflowNames.join(', '), is_secret: false }]
        : [];
      const deleteKeys = ['RAILPUSH_GITHUB_ACTIONS_WORKFLOW'];
      if (workflowNames.length === 0) {
        deleteKeys.push('RAILPUSH_GITHUB_ACTIONS_WORKFLOWS');
      }

      await envVarsApi.merge(serviceId, { env_vars: envVars, delete: deleteKeys });
      setDeployAutomationWorkflows(workflowNames);
      setWorkflowDraft(workflowNames.join(', '));
      setWorkflowModalOpen(false);
      toast.success(workflowNames.length > 0 ? 'Workflow allowlist updated' : 'Workflow allowlist cleared');
      setTimeout(refresh, 500);
    } catch {
      toast.error('Failed to update workflow allowlist');
    }
    setWorkflowSaving(false);
  };

  const addWorkflowSuggestion = (name: string) => {
    const merged = parseWorkflowNames([workflowDraft, name].filter(Boolean).join(', '));
    setWorkflowDraft(merged.join(', '));
  };

  const deployActions = [
    { label: 'Deploy latest commit', onClick: () => runAction('Deploying…', () => deploysApi.trigger(service.id)) },
    { label: 'Clear build cache & deploy', onClick: () => runAction('Deploying…', () => deploysApi.trigger(service.id, { clearCache: true })) },
    { divider: true, label: '', onClick: () => { } },
    { label: 'Restart service', icon: <RotateCw className="w-4 h-4" />, onClick: () => runAction('Restarting…', () => servicesApi.restart(service.id)) },
  ];

  const deployAutomationActions = [
    {
      label: 'On Commit',
      icon: deployAutomationMode === 'push' ? <Check className="w-4 h-4" /> : undefined,
      onClick: () => { setDeployAutomationModeQuick('push'); },
    },
    {
      label: 'After GitHub Actions Success',
      icon: deployAutomationMode === 'workflow_success' ? <Check className="w-4 h-4" /> : undefined,
      onClick: () => { setDeployAutomationModeQuick('workflow_success'); },
    },
    {
      label: 'Off (Manual)',
      icon: deployAutomationMode === 'off' ? <Check className="w-4 h-4" /> : undefined,
      onClick: () => { setDeployAutomationModeQuick('off'); },
    },
    { divider: true, label: '', onClick: () => { } },
    {
      label: 'Edit Workflow Allowlist',
      onClick: () => { openWorkflowAllowlistModal(); },
    },
  ];

  return (
    <div className="space-y-8 animate-enter pb-10">
      {/* Header */}
      <div className="glass-panel p-6 rounded-xl relative overflow-hidden">
        <div className="flex flex-wrap items-start justify-between gap-4 relative z-10">
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-surface-tertiary/50 ring-1 ring-border-default shadow-lg backdrop-blur-sm">
              <ServiceIcon type={service.type} size="lg" />
            </div>
            <div>
              <div className="flex items-center gap-2 mb-1">
                <h1 className="text-2xl font-bold text-white tracking-tight">{service.name}</h1>
                <StatusBadge status={service.status} />
              </div>

                <div className="flex items-center gap-4 mt-2 text-sm text-content-secondary flex-wrap">
                  <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-surface-tertiary/30 border border-border-default/50">
                    <Box className="w-3.5 h-3.5 text-content-tertiary" />
                    <span>{serviceTypeLabel(service.type)}</span>
                  </div>
                  <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-surface-tertiary/30 border border-border-default/50 font-mono text-xs">
                    <GitBranch className="w-3.5 h-3.5 text-brand" />
                    {service.branch}
                  </div>
                  <div
                    className={`flex items-center gap-1.5 px-2 py-0.5 rounded border text-xs ${deployAutomationTone}`}
                    title={deployAutomationTitle}
                  >
                    <span className={`w-1.5 h-1.5 rounded-full ${deployAutomationDotTone}`} />
                    {deployAutomationLabel}
                  </div>
                  {serviceUrl && (
                    <div className="flex items-center gap-1 ml-2">
                      <a
                      href={serviceUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-1.5 text-brand hover:text-brand-hover transition-colors"
                    >
                      <ExternalLink className="w-3.5 h-3.5" />
                      <span className="truncate max-w-[200px]">{serviceUrl.replace(/^https?:\/\//, '')}</span>
                    </a>
                    <CopyUrlButton url={serviceUrl} />
                  </div>
                )}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {actionInProgress ? (
              <Button variant="primary" className="shadow-lg shadow-brand/20 pointer-events-none opacity-80">
                <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                {actionInProgress}
              </Button>
            ) : (
              <>
                <Dropdown
                  trigger={
                    <Button variant="secondary" className="shadow-lg">
                      Automation
                      <ChevronDown className="w-4 h-4 ml-1" />
                    </Button>
                  }
                  items={deployAutomationActions}
                  align="right"
                />
                <Dropdown
                  trigger={
                    <Button variant="primary" className="shadow-lg shadow-brand/20">
                      Deploy
                      <ChevronDown className="w-4 h-4 ml-1" />
                    </Button>
                  }
                  items={deployActions}
                  align="right"
                />
              </>
            )}
          </div>
        </div>

        {/* Decorative background glow */}
        <div className="absolute top-0 right-0 w-64 h-64 bg-brand/5 rounded-full blur-3xl -translate-y-1/2 translate-x-1/2 pointer-events-none" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Column: Stats & Deploys */}
        <div className="lg:col-span-2 space-y-6">

          {/* Quick Metrics */}
          <div className="grid grid-cols-3 gap-4">
            <Card className="glass-panel p-4 flex flex-col items-center justify-center text-center space-y-1 hover:border-border-hover transition-colors">
              <span className="text-xs uppercase tracking-wider text-content-tertiary">CPU Usage</span>
              <span className="text-xl font-bold text-content-primary">{metrics?.cpu_percent ?? '—'}</span>
            </Card>
            <Card className="glass-panel p-4 flex flex-col items-center justify-center text-center space-y-1 hover:border-border-hover transition-colors">
              <span className="text-xs uppercase tracking-wider text-content-tertiary">Memory</span>
              <span className="text-xl font-bold text-content-primary">{metrics?.memory_used ?? '—'}</span>
            </Card>
            <Card className="glass-panel p-4 flex flex-col items-center justify-center text-center space-y-1 hover:border-border-hover transition-colors">
              <span className="text-xs uppercase tracking-wider text-content-tertiary">Memory %</span>
              <span className="text-xl font-bold text-content-primary">{metrics?.memory_percent ?? '—'}</span>
            </Card>
          </div>

          {/* Latest Deploy */}
          {latestDeploy && (
            <div className="space-y-3">
              <div className="flex items-center justify-between px-1">
                <h2 className="text-xs font-bold uppercase tracking-wider text-content-tertiary">Latest Deploy</h2>
                <span className="text-xs text-content-tertiary">{timeAgo(latestDeploy.started_at || latestDeploy.finished_at)}</span>
              </div>
              <Card hover onClick={() => navigate(`/services/${service.id}/events`)} className="glass-panel p-5 group">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-start gap-3">
                    <StatusBadge status={latestDeploy.status} size="sm" />
                    <div>
                      <div className="font-semibold text-content-primary group-hover:text-brand transition-colors">
                        Deploy #{deployList.indexOf(latestDeploy) + 1}
                      </div>
                      <div className="flex items-center gap-2 mt-1 text-sm text-content-secondary">
                        {latestDeploy.commit_sha && (
                          <code className="px-1.5 py-0.5 bg-surface-tertiary rounded font-mono text-xs text-content-primary border border-border-default/50">
                            {latestDeploy.commit_sha.slice(0, 7)}
                          </code>
                        )}
                        {latestDeploy.commit_message && (
                          <span className="line-clamp-1 opacity-80">
                            {latestDeploy.commit_message}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>

                  {latestDeploy.started_at && latestDeploy.finished_at && (
                    <div className="text-xs text-content-tertiary flex items-center gap-1.5 bg-surface-tertiary/20 px-2 py-1 rounded">
                      <Clock className="w-3 h-3" />
                      {formatDuration(new Date(latestDeploy.finished_at).getTime() - new Date(latestDeploy.started_at).getTime())}
                    </div>
                  )}
                </div>
              </Card>
            </div>
          )}

          {/* Build Log — streaming or completed */}
          {(isBuildInProgress && buildLines.length > 0) || latestDeploy?.build_log ? (
            <div className="space-y-3">
              <div className="flex items-center justify-between px-1">
                <h2 className="text-xs font-bold uppercase tracking-wider text-content-tertiary flex items-center gap-2">
                  Console Output
                  {isBuildInProgress && <Loader2 className="w-3 h-3 animate-spin text-brand" />}
                </h2>
                <Button variant="ghost" size="sm" className="h-6 text-xs" onClick={() => navigate(`/services/${service.id}/logs`)}>View Logs</Button>
              </div>
              <div className="font-mono text-xs bg-[#0d1117] rounded-lg border border-border-default shadow-2xl overflow-hidden flex flex-col">
                <div className="flex items-center gap-2 px-4 py-2 bg-surface-tertiary/20 border-b border-border-default/50">
                  <div className="flex gap-1.5">
                    <div className="w-2.5 h-2.5 rounded-full bg-[#FF5F56]" />
                    <div className="w-2.5 h-2.5 rounded-full bg-[#FFBD2E]" />
                    <div className="w-2.5 h-2.5 rounded-full bg-[#27C93F]" />
                  </div>
                  <div className="flex-1 text-center text-[10px] text-content-tertiary font-medium opacity-60">
                    {isBuildInProgress ? 'building' : 'build'} · {latestDeploy?.commit_sha?.slice(0, 7) || 'latest'}
                  </div>
                </div>
                <div className="p-4 overflow-y-auto h-[320px]">
                  <pre className="text-content-secondary whitespace-pre-wrap break-all leading-relaxed">
                    {isBuildInProgress && buildLines.length > 0
                      ? buildLines.join('\n')
                      : latestDeploy?.build_log}
                  </pre>
                  <div ref={buildLogEndRef} />
                </div>
              </div>
            </div>
          ) : null}
        </div>

        {/* Right Column: Sidebar Actions / Info */}
        <div className="space-y-6">
          <Card className="glass-panel p-5 space-y-4">
            <h3 className="text-sm font-semibold text-white mb-2">Service Details</h3>

            <div className="space-y-3">
              <div className="flex justify-between items-center text-sm">
                <span className="text-content-secondary">Region</span>
                <span className="text-content-primary">Oregon, USA</span>
              </div>
              <div className="flex justify-between items-center text-sm">
                <span className="text-content-secondary">Runtime</span>
                <div className="flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-emerald-500" />
                  <span className="text-content-primary capitalize">{service.runtime || 'Node'}</span>
                </div>
              </div>
              <div className="flex justify-between items-center text-sm">
                <span className="text-content-secondary">Plan</span>
                <span className="text-content-primary capitalize">{service.plan || 'Free'}</span>
              </div>
              <div className="flex justify-between items-center text-sm">
                <span className="text-content-secondary">Deploy Automation</span>
                <span className="text-content-primary text-xs">{deployAutomationLabel}</span>
              </div>
              {deployAutomationMode === 'workflow_success' && (
                <div className="rounded-md border border-border-default/60 bg-surface-tertiary/20 px-3 py-2">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-[11px] text-content-tertiary leading-relaxed">
                      Workflows: {deployAutomationWorkflows.length > 0 ? deployAutomationWorkflows.join(', ') : 'Any successful workflow'}
                    </div>
                    <button
                      onClick={openWorkflowAllowlistModal}
                      className="text-[11px] font-medium text-brand hover:text-brand-hover transition-colors whitespace-nowrap"
                    >
                      Edit
                    </button>
                  </div>
                </div>
              )}
            </div>

            <div className="pt-4 border-t border-border-default space-y-2">
              <Button variant="secondary" className="w-full justify-start" onClick={() => navigate(`/services/${service.id}/settings`)}>
                <Settings className="w-4 h-4 mr-2" /> Settings
              </Button>
              <Button variant="secondary" className="w-full justify-start" onClick={() => navigate(`/services/${service.id}/metrics`)}>
                <Activity className="w-4 h-4 mr-2" /> Metrics
              </Button>
            </div>
          </Card>

          {serviceUrl && (
            <Card className="glass-panel p-4 bg-status-success/5 border-status-success/20">
              <div className="flex items-start gap-3">
                <ShieldCheck className="w-5 h-5 text-status-success shrink-0" />
                <div>
                  <h4 className="text-sm font-medium text-content-primary">TLS Secured</h4>
                  <p className="text-xs text-content-secondary mt-1">Your service is served over HTTPS with an automatic certificate.</p>
                </div>
              </div>
            </Card>
          )}
        </div>
      </div>

      {workflowModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" onClick={() => { if (!workflowSaving) setWorkflowModalOpen(false); }}>
          <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
          <div className="relative w-full max-w-xl" onClick={(e) => e.stopPropagation()}>
            <Card className="glass-panel p-5 border-border-hover">
              <h3 className="text-base font-semibold text-content-primary">Edit Workflow Allowlist</h3>
              <p className="text-sm text-content-secondary mt-1 mb-4">
                Set comma-separated GitHub workflow names. Leave blank to allow any successful workflow run.
              </p>
              <Input
                label="Workflow Names"
                value={workflowDraft}
                onChange={(e) => setWorkflowDraft(e.target.value)}
                placeholder="CI, Release Build"
                hint="These names must match the GitHub Actions workflow names exactly."
                autoFocus
              />
              {loadingGitHubWorkflows && (
                <p className="text-xs text-content-tertiary mt-3">Loading workflow names from GitHub…</p>
              )}
              {!loadingGitHubWorkflows && githubWorkflowLoadError && (
                <p className="text-xs text-amber-400 mt-3">{githubWorkflowLoadError}</p>
              )}
              {!loadingGitHubWorkflows && knownGitHubWorkflows.length > 0 && (
                <div className="mt-3 space-y-2">
                  <p className="text-xs text-content-tertiary">Known workflows (click to add):</p>
                  <div className="flex flex-wrap gap-1.5">
                    {knownGitHubWorkflows.map((name) => (
                      <button
                        key={name}
                        onClick={() => addWorkflowSuggestion(name)}
                        className="px-2 py-1 rounded-md text-[11px] border border-border-default bg-surface-tertiary/30 text-content-secondary hover:text-content-primary hover:border-border-hover transition-colors"
                      >
                        {name}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {!loadingGitHubWorkflows && unknownDraftWorkflows.length > 0 && (
                <p className="text-xs text-amber-400 mt-3">
                  Warning: Unknown workflow names: {unknownDraftWorkflows.join(', ')}
                </p>
              )}
              <div className="mt-5 flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setWorkflowModalOpen(false)} disabled={workflowSaving}>
                  Cancel
                </Button>
                <Button onClick={saveWorkflowAllowlist} loading={workflowSaving}>
                  Save Allowlist
                </Button>
              </div>
            </Card>
          </div>
        </div>
      )}
    </div>
  );
}
