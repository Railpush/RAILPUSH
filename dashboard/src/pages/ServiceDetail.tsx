import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ExternalLink, RotateCw, GitBranch, ChevronDown, ShieldCheck, Clock, Activity, Box, Settings, Copy, Check, Loader2 } from 'lucide-react';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Dropdown } from '../components/ui/Dropdown';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { serviceTypeLabel, timeAgo, formatDuration } from '../lib/utils';
import { buildDefaultServiceUrl } from '../lib/serviceUrl';
import { services as servicesApi, deploys as deploysApi } from '../lib/api';
import type { Service, Deploy } from '../types';

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

  const refresh = () => {
    if (!serviceId) return;
    Promise.all([
      servicesApi.get(serviceId).catch(() => null),
      deploysApi.list(serviceId).catch(() => []),
    ]).then(([s, d]) => {
      setService(s);
      setDeployList(d);
      setLoading(false);
    });
    // Fetch real metrics (best-effort)
    fetch(`/api/v1/services/${serviceId}/metrics`, { credentials: 'include' })
      .then(r => r.ok ? r.json() : null)
      .then(m => { if (m) setMetrics(m); })
      .catch(() => {});
  };

  useEffect(() => { refresh(); }, [serviceId]);

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

  const runAction = async (label: string, action: () => Promise<unknown>) => {
    setActionInProgress(label);
    try {
      await action();
      setTimeout(refresh, 1000);
    } catch { /* ignore */ }
    setActionInProgress(null);
  };

  const deployActions = [
    { label: 'Deploy latest commit', onClick: () => runAction('Deploying…', () => deploysApi.trigger(service.id)) },
    { label: 'Clear build cache & deploy', onClick: () => runAction('Deploying…', () => deploysApi.trigger(service.id, { clearCache: true })) },
    { divider: true, label: '', onClick: () => { } },
    { label: 'Restart service', icon: <RotateCw className="w-4 h-4" />, onClick: () => runAction('Restarting…', () => servicesApi.restart(service.id)) },
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

          {/* Build Log */}
          {latestDeploy?.build_log && (
            <div className="space-y-3">
              <div className="flex items-center justify-between px-1">
                <h2 className="text-xs font-bold uppercase tracking-wider text-content-tertiary">Console Output</h2>
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
                    build · {latestDeploy.commit_sha?.slice(0, 7) || 'latest'}
                  </div>
                </div>
                <div className="p-4 overflow-y-auto h-[320px]">
                  <pre className="text-content-secondary whitespace-pre-wrap break-all leading-relaxed">
                    {latestDeploy.build_log}
                  </pre>
                </div>
              </div>
            </div>
          )}
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
    </div>
  );
}
