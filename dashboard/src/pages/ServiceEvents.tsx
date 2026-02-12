import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { GitCommit, Rocket, RotateCw } from 'lucide-react';
import { StatusBadge } from '../components/ui/StatusBadge';
import { Skeleton } from '../components/ui/Skeleton';
import { Card } from '../components/ui/Card';
import { formatDate, formatTime, formatDuration, truncate } from '../lib/utils';
import { deploys as deploysApi } from '../lib/api';
import type { Deploy } from '../types';

export function ServiceEvents() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [deployList, setDeployList] = useState<Deploy[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!serviceId) return;
    deploysApi.list(serviceId).then(setDeployList).catch(() => []).finally(() => setLoading(false));
  }, [serviceId]);

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="w-32 h-7" />
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="w-full h-20" />
        ))}
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
      case 'git_push': return <GitCommit className="w-4 h-4" />;
      case 'manual': return <Rocket className="w-4 h-4" />;
      case 'rollback': return <RotateCw className="w-4 h-4" />;
      default: return <Rocket className="w-4 h-4" />;
    }
  };

  const triggerLabel = (trigger: string) => {
    switch (trigger) {
      case 'git_push': return 'Git push';
      case 'manual': return 'Manual deploy';
      case 'rollback': return 'Rollback';
      case 'blueprint': return 'Blueprint sync';
      default: return trigger;
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <p className="text-[11px] uppercase tracking-[0.22em] text-content-tertiary font-semibold">Service timeline</p>
          <h1 className="text-2xl font-semibold text-content-primary">Events</h1>
        </div>
      </div>

      {deployList.length === 0 ? (
        <Card className="p-8 text-center text-sm text-content-secondary">
          No deploy events yet. Trigger a deploy to see events here.
        </Card>
      ) : (
        <div className="space-y-6">
          {Object.entries(grouped).map(([date, deploys]) => (
            <div key={date}>
              <h3 className="text-xs font-semibold uppercase tracking-[0.2em] text-content-tertiary mb-3">
                {date}
              </h3>
              <div className="grid gap-3 md:grid-cols-2">
                {deploys.map((deploy) => (
                  <Card key={deploy.id} hover className="space-y-2">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2.5">
                        <StatusBadge status={deploy.status} size="sm" />
                        <span className="text-sm font-medium text-content-primary">
                          Deploy #{deployList.length - deployList.indexOf(deploy)}
                        </span>
                      </div>
                      <span className="text-xs text-content-tertiary">
                        {deploy.started_at ? formatTime(deploy.started_at) : ''}
                      </span>
                    </div>

                    {deploy.commit_sha && (
                      <div className="flex items-center gap-2">
                        <code className="text-xs font-mono px-1.5 py-0.5 bg-surface-tertiary rounded text-content-primary">
                          {deploy.commit_sha.slice(0, 7)}
                        </code>
                        {deploy.commit_message && (
                          <span className="text-xs text-content-secondary">
                            "{truncate(deploy.commit_message, 80)}"
                          </span>
                        )}
                      </div>
                    )}

                    <div className="flex items-center gap-3 text-xs text-content-tertiary flex-wrap">
                      <div className="flex items-center gap-1">
                        {triggerIcon(deploy.trigger)}
                        <span>{triggerLabel(deploy.trigger)}</span>
                      </div>
                      {deploy.started_at && deploy.finished_at && (
                        <span>
                          Duration: {formatDuration(
                            new Date(deploy.finished_at).getTime() - new Date(deploy.started_at).getTime()
                          )}
                        </span>
                      )}
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
