import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Database, Key, Shield, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { CopyButton } from '../components/ui/CopyButton';
import { StatusBadge } from '../components/ui/StatusBadge';
import { keyvalue as keyValueApi } from '../lib/api';
import type { ManagedKeyValue } from '../types';

type KeyValueDetailData = ManagedKeyValue & {
  password?: string;
  redis_url?: string;
};

export function KeyValueDetail() {
  const { kvId } = useParams<{ kvId: string }>();
  const navigate = useNavigate();
  const [kv, setKv] = useState<KeyValueDetailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (!kvId) return;
    keyValueApi
      .get(kvId)
      .then((data) => setKv(data as KeyValueDetailData))
      .catch(() => toast.error('Failed to load key value store'))
      .finally(() => setLoading(false));
  }, [kvId]);

  const redisURL = kv
    ? kv.redis_url ||
      (kv.password
        ? `redis://:${kv.password}@${kv.host}:${kv.port}`
        : `redis://${kv.host}:${kv.port}`)
    : '';

  const handleDelete = async () => {
    if (!kv || deleting) return;
    if (!window.confirm(`Delete key value store \"${kv.name}\"? This action cannot be undone.`)) return;

    setDeleting(true);
    try {
      await keyValueApi.delete(kv.id);
      toast.success('Key value store deleted');
      navigate('/keyvalue');
    } catch {
      toast.error('Failed to delete key value store');
    } finally {
      setDeleting(false);
    }
  };

  if (loading) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Key Value</h1>
        <Card>
          <div className="text-sm text-content-secondary">Loading...</div>
        </Card>
      </div>
    );
  }

  if (!kv) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Key Value</h1>
        <Card>
          <div className="text-sm text-content-secondary">Key value store not found.</div>
        </Card>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold text-content-primary">{kv.name}</h1>
          <div className="flex items-center gap-2 mt-2">
            <StatusBadge status={kv.status === 'available' ? 'live' : 'created'} />
            <span className="text-xs text-content-tertiary uppercase tracking-wider">{kv.plan}</span>
          </div>
        </div>
        <Button variant="danger" onClick={handleDelete} loading={deleting}>
          <Trash2 className="w-4 h-4" />
          Delete
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-2 mb-4">
        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Connection</div>
          <div className="space-y-3 text-sm">
            <div className="flex items-center gap-2 text-content-primary">
              <Database className="w-4 h-4 text-content-tertiary" />
              <span className="font-mono">{kv.host}:{kv.port}</span>
            </div>
            <div className="flex items-center gap-2 text-content-primary">
              <Key className="w-4 h-4 text-content-tertiary" />
              <span className="font-mono">{kv.maxmemory_policy}</span>
            </div>
            {kv.password && (
              <div className="flex items-center gap-2 text-content-primary">
                <Shield className="w-4 h-4 text-content-tertiary" />
                <span className="font-mono">Password available</span>
              </div>
            )}
          </div>
        </Card>

        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Redis URL</div>
          <div className="flex items-center gap-2 bg-surface-tertiary border border-border-subtle rounded-md px-3 py-2">
            <code className="text-xs text-content-primary break-all flex-1">{redisURL}</code>
            <CopyButton text={redisURL} />
          </div>
        </Card>
      </div>
    </div>
  );
}
