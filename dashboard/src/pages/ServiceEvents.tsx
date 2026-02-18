import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { GitCommit, Rocket, RotateCw, GitBranch, Clock, ArrowLeft, ListOrdered } from 'lucide-react';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { formatDate, formatTime, formatDuration } from '../lib/utils';
import { deploys as deploysApi, services as servicesApi } from '../lib/api';
import type { Deploy } from '../types';

export function ServiceEvents() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const navigate = useNavigate();
  const [deployList, setDeployList] = useState<Deploy[]>([]);
  const [loading, setLoading] = useState(true);
  const [serviceName, setServiceName] = useState('');
  const [queuePositions, setQueuePositions] = useState<Record<string, { position: number; total_queued: number }>>({});

  useEffect(() => {
    if (!serviceId) return;

    Promise.all([
      deploysApi.list(serviceId).catch(() => []),
      servicesApi.get(serviceId).then(s => s.name).catch(() => '')
    ]).then(([deploys, name]) => {
      setDeployList(deploys);
      setServiceName(name);
      setLoading(false);

      // Fetch queue positions for pending/building/deploying deploys
      const queued = deploys.filter(d => ['pending', 'building', 'deploying'].includes(d.status));
      queued.forEach(d => {
        deploysApi.queuePosition(serviceId, d.id).then(info => {
          setQueuePositions(prev => ({ ...prev, [d.id]: info }));
        }).catch(() => {});
      });
    });
  }, [serviceId]);

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto space-y-8 animate-pulse">
        <div className="h-8 w-48 bg-surface-tertiary rounded" />
        <div className="space-y-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="flex gap-4">
              <div className="w-12 flex flex-col items-center">
                <div className="w-3 h-3 rounded-full bg-surface-tertiary" />
                <div className="w-0.5 h-full bg-surface-tertiary/20 mt-2" />
              </div>
              <div className="flex-1 h-24 bg-surface-tertiary rounded-xl" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  // Group deploys by date
  const grouped = deployList.reduce<Record<string, Deploy[]>>((acc, d) => {
    const date = formatDate(d.started_at || d.finished_at || d.id);
    if (!acc[date]) acc[date] = [];
    acc[date].push(d);
    return acc;
  }, {});

  const triggerIcon = (trigger: string) => {
    switch (trigger) {
      case 'git_push': return <GitBranch className="w-3.5 h-3.5" />;
      case 'manual': return <Rocket className="w-3.5 h-3.5" />;
      case 'rollback': return <RotateCw className="w-3.5 h-3.5" />;
      default: return <Rocket className="w-3.5 h-3.5" />;
    }
  };

  const triggerLabel = (trigger: string) => {
    switch (trigger) {
      case 'git_push': return 'Git Push';
      case 'manual': return 'Manual Deploy';
      case 'rollback': return 'Rollback';
      case 'blueprint': return 'Blueprint Sync';
      default: return trigger;
    }
  };

  return (
    <div className="max-w-4xl mx-auto animate-enter pb-12">
      <div className="mb-8">
        <Button variant="ghost" className="mb-4 pl-0 hover:pl-2 transition-all" onClick={() => navigate(`/services/${serviceId}`)}>
          <ArrowLeft className="w-4 h-4 mr-2" /> Back to Service
        </Button>
        <div className="flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-surface-tertiary/50 border border-border-default">
            <Rocket className="w-5 h-5 text-brand" />
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-content-tertiary font-semibold">Deployment History</p>
            <h1 className="text-2xl font-bold text-content-primary">{serviceName} Events</h1>
          </div>
        </div>
      </div>

      {deployList.length === 0 ? (
        <Card className="glass-panel p-12 text-center">
          <div className="w-16 h-16 rounded-full bg-surface-tertiary/50 flex items-center justify-center mx-auto mb-4">
            <Rocket className="w-8 h-8 text-content-tertiary" />
          </div>
          <h3 className="text-lg font-medium text-content-primary">No deploys yet</h3>
          <p className="text-content-secondary mt-1 max-w-sm mx-auto">
            Trigger a deploy or push to your connected repository to see events appear here.
          </p>
        </Card>
      ) : (
        <div className="relative">
          {/* Vertical line connecting groups */}
          <div className="absolute left-[19px] top-4 bottom-0 w-px bg-border-default/30" />

          {Object.entries(grouped).map(([date, deploys]) => (
            <div key={date} className="mb-10 relative">
              <div className="flex items-center gap-4 mb-6">
                <div className="w-10 flex justify-center z-10">
                  <div className="w-2.5 h-2.5 rounded-full bg-brand ring-4 ring-background" />
                </div>
                <h3 className="text-sm font-bold uppercase tracking-wider text-content-tertiary bg-background/50 backdrop-blur px-2 rounded">
                  {date}
                </h3>
              </div>

              <div className="space-y-4 pl-14">
                {deploys.map((deploy) => (
                  <Card key={deploy.id} className="glass-panel p-5 group hover:border-brand/30 transition-colors">
                    <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                      <div className="flex items-start gap-4">
                        <div className="pt-1">
                          <StatusBadge status={deploy.status} />
                        </div>

                        <div className="space-y-1">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-content-primary">Deploy #{deployList.length - deployList.indexOf(deploy)}</span>
                            <span className="text-xs text-content-tertiary px-1.5 py-0.5 rounded border border-border-default/50 flex items-center gap-1">
                              {triggerIcon(deploy.trigger)}
                              {triggerLabel(deploy.trigger)}
                            </span>
                          </div>

                          <div className="flex items-center gap-3 text-sm text-content-secondary">
                            {deploy.commit_sha ? (
                              <>
                                <div className="flex items-center gap-1.5 font-mono text-xs bg-surface-tertiary/30 px-1.5 py-0.5 rounded text-content-primary">
                                  <GitCommit className="w-3 h-3 opacity-70" />
                                  {deploy.commit_sha.slice(0, 7)}
                                </div>
                                {deploy.commit_message && (
                                  <span className="line-clamp-1 max-w-md">"{deploy.commit_message}"</span>
                                )}
                              </>
                            ) : (
                              <span className="text-content-tertiary italic">No commit info</span>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-4 text-xs text-content-tertiary md:text-right md:flex-col md:gap-1 md:items-end min-w-[120px]">
                        <div className="flex items-center gap-1.5">
                          <Clock className="w-3.5 h-3.5" />
                          {deploy.started_at ? formatTime(deploy.started_at) : 'Pending'}
                        </div>
                        {queuePositions[deploy.id] && ['pending', 'building', 'deploying'].includes(deploy.status) && (
                          <span className="flex items-center gap-1 text-xs text-amber-400 bg-amber-400/10 px-1.5 py-0.5 rounded border border-amber-400/20">
                            <ListOrdered className="w-3 h-3" />
                            #{queuePositions[deploy.id].position} of {queuePositions[deploy.id].total_queued}
                          </span>
                        )}
                        {deploy.started_at && deploy.finished_at && (
                          <span>
                            Took {formatDuration(new Date(deploy.finished_at).getTime() - new Date(deploy.started_at).getTime())}
                          </span>
                        )}
                      </div>
                    </div>
                  </Card>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

