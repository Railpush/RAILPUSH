import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Layers, GitBranch } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { EmptyState } from '../components/ui/EmptyState';
import { StatusBadge } from '../components/ui/StatusBadge';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { blueprints as bpApi } from '../lib/api';
import { timeAgo } from '../lib/utils';
import type { Blueprint } from '../types';

export function Blueprints() {
  const navigate = useNavigate();
  const [bpList, setBpList] = useState<Blueprint[]>([]);
  const [, setLoading] = useState(true);

  useEffect(() => {
    bpApi.list().then(setBpList).catch(() => {}).finally(() => setLoading(false));
  }, []);

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Blueprints</h1>
        <Button onClick={() => navigate('/new/blueprint')}>
          <Plus className="w-4 h-4" />
          New Blueprint
        </Button>
      </div>

      {bpList.length === 0 ? (
        <EmptyState
          icon={<Layers className="w-6 h-6" />}
          title="No blueprints"
          description="Blueprints let you define your entire infrastructure in a railpush.yaml file. Push to deploy your full stack."
          action={{ label: 'Create Blueprint', onClick: () => navigate('/new/blueprint') }}
        />
      ) : (
        <div className="space-y-3">
          {bpList.map((bp) => (
            <Card key={bp.id} hover onClick={() => navigate(`/blueprints/${bp.id}`)}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <ServiceIcon type="blueprint" />
                  <div>
                    <div className="text-sm font-semibold text-content-primary">{bp.name}</div>
                    <div className="flex items-center gap-2 text-xs text-content-secondary mt-0.5">
                      <GitBranch className="w-3 h-3" />
                      {bp.branch}
                      <span>&middot;</span>
                      <span>{bp.file_path}</span>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  {bp.last_synced_at && (
                    <span className="text-xs text-content-tertiary">
                      Synced {timeAgo(bp.last_synced_at)}
                    </span>
                  )}
                  <StatusBadge status={bp.last_sync_status === 'synced' ? 'live' : bp.last_sync_status === 'syncing' ? 'building' : bp.last_sync_status?.startsWith('failed') ? 'failed' : 'failed'} size="sm" />
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
