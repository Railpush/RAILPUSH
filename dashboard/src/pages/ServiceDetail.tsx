import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { ExternalLink, RotateCw, GitBranch, ChevronDown, ShieldCheck } from 'lucide-react';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Dropdown } from '../components/ui/Dropdown';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { Skeleton } from '../components/ui/Skeleton';
import { serviceTypeLabel, timeAgo, formatDuration, truncate } from '../lib/utils';
import { buildDefaultServiceUrl } from '../lib/serviceUrl';
import { services as servicesApi, deploys as deploysApi } from '../lib/api';
import type { Service, Deploy } from '../types';

export function ServiceDetail() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const navigate = useNavigate();
  const [service, setService] = useState<Service | null>(null);
  const [deployList, setDeployList] = useState<Deploy[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!serviceId) return;
    Promise.all([
      servicesApi.get(serviceId).catch(() => null),
      deploysApi.list(serviceId).catch(() => []),
    ]).then(([s, d]) => {
      setService(s);
      setDeployList(d);
      setLoading(false);
    });
  }, [serviceId]);

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="w-48 h-8" />
        <Skeleton className="w-96 h-4" />
        <Skeleton className="w-full h-40" />
      </div>
    );
  }

  if (!service) {
    return <div className="text-content-secondary">Service not found</div>;
  }

  const latestDeploy = deployList[0];
  const serviceUrl = buildDefaultServiceUrl(service);
  const hasTLS = serviceUrl.startsWith('https://');

  const deployActions = [
    { label: 'Deploy latest commit', onClick: () => deploysApi.trigger(service.id) },
    { label: 'Clear build cache & deploy', onClick: () => deploysApi.trigger(service.id, { clearCache: true }) },
    { divider: true, label: '', onClick: () => {} },
    { label: 'Restart service', icon: <RotateCw className="w-4 h-4" />, onClick: () => servicesApi.restart(service.id) },
  ];

  return (
    <div>
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <ServiceIcon type={service.type} size="lg" />
            <div>
              <h1 className="text-2xl font-semibold text-content-primary">{service.name}</h1>
              <div className="flex items-center gap-2 mt-1 text-sm text-content-secondary">
                <span>{serviceTypeLabel(service.type)}</span>
                <span>&middot;</span>
                <div className="flex items-center gap-1">
                  <GitBranch className="w-3.5 h-3.5" />
                  {service.branch}
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Dropdown
              trigger={
                <Button variant="primary">
                  Manual Deploy
                  <ChevronDown className="w-4 h-4" />
                </Button>
              }
              items={deployActions}
              align="right"
            />
          </div>
        </div>

        {/* Status & URL */}
        <Card padding="md" className="mt-4">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div>
              <div className="text-[11px] uppercase tracking-wider text-content-tertiary mb-1">Status</div>
              <StatusBadge status={service.status} />
            </div>
            <div className="md:col-span-2">
              <div className="text-[11px] uppercase tracking-wider text-content-tertiary mb-1">Live URL</div>
              {serviceUrl ? (
                <a
                  href={serviceUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-sm text-brand hover:text-brand-hover transition-colors break-all"
                >
                  {serviceUrl}
                  <ExternalLink className="w-3.5 h-3.5 shrink-0" />
                </a>
              ) : (
                <span className="text-sm text-content-secondary">Available after first successful deploy.</span>
              )}
              {serviceUrl && (
                <div className="mt-1 text-xs text-content-tertiary inline-flex items-center gap-1.5">
                  {hasTLS ? <ShieldCheck className="w-3.5 h-3.5 text-status-success" /> : null}
                  {hasTLS ? 'TLS certificate active or provisioning automatically.' : 'Using non-TLS local URL.'}
                </div>
              )}
            </div>
          </div>
        </Card>
      </div>

      {/* Latest Deploy */}
      {latestDeploy && (
        <div className="mb-6">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
            Latest Deploy
          </h2>
          <Card hover onClick={() => navigate(`/services/${service.id}/events`)}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <StatusBadge status={latestDeploy.status} size="sm" />
                <span className="text-sm font-medium text-content-primary">
                  Deploy #{deployList.indexOf(latestDeploy) + 1}
                </span>
              </div>
              <span className="text-xs text-content-secondary">{timeAgo(latestDeploy.started_at || latestDeploy.finished_at)}</span>
            </div>
            {latestDeploy.commit_sha && (
              <div className="mt-2 flex items-center gap-2 text-xs text-content-secondary">
                <code className="px-1.5 py-0.5 bg-surface-tertiary rounded font-mono text-content-primary">
                  {latestDeploy.commit_sha.slice(0, 7)}
                </code>
                {latestDeploy.commit_message && (
                  <span>"{truncate(latestDeploy.commit_message, 60)}"</span>
                )}
              </div>
            )}
            {latestDeploy.started_at && latestDeploy.finished_at && (
              <div className="mt-2 text-xs text-content-tertiary">
                Duration: {formatDuration(new Date(latestDeploy.finished_at).getTime() - new Date(latestDeploy.started_at).getTime())}
              </div>
            )}
          </Card>
        </div>
      )}

      {/* Recent Events */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary">
            Recent Events
          </h2>
          <button
            onClick={() => navigate(`/services/${service.id}/events`)}
            className="text-xs text-brand hover:text-brand-hover transition-colors"
          >
            View all
          </button>
        </div>
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          {deployList.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-content-secondary">No deploys yet</div>
          ) : (
            deployList.slice(0, 5).map((deploy, i) => (
              <div
                key={deploy.id}
                className="flex items-center justify-between px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary cursor-pointer transition-colors"
                onClick={() => navigate(`/services/${service.id}/events`)}
              >
                <div className="flex items-center gap-3">
                  <StatusBadge status={deploy.status} size="sm" />
                  <span className="text-sm text-content-primary">Deploy #{deployList.length - i}</span>
                </div>
                <div className="flex items-center gap-3">
                  {deploy.commit_sha && (
                    <code className="text-xs font-mono text-content-secondary">
                      {deploy.commit_sha.slice(0, 7)}
                    </code>
                  )}
                  <span className="text-xs text-content-tertiary">
                    {timeAgo(deploy.started_at || deploy.finished_at)}
                  </span>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
