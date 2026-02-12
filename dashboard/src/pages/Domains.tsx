import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Globe2, Search } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { EmptyState } from '../components/ui/EmptyState';
import { registeredDomains } from '../lib/api';
import type { RegisteredDomain } from '../types';

const statusColors: Record<string, { color: string; bg: string }> = {
  active: { color: '#10b981', bg: '#10b98115' },
  pending: { color: '#f59e0b', bg: '#f59e0b15' },
  expired: { color: '#ef4444', bg: '#ef444415' },
  cancelled: { color: '#6b7280', bg: '#6b728015' },
  transferring: { color: '#3b82f6', bg: '#3b82f615' },
};

const tldColors: Record<string, string> = {
  com: '#3b82f6',
  net: '#8b5cf6',
  org: '#10b981',
  io: '#f59e0b',
  dev: '#06b6d4',
  app: '#ec4899',
  xyz: '#f97316',
  co: '#6366f1',
};

export function Domains() {
  const navigate = useNavigate();
  const [domains, setDomains] = useState<RegisteredDomain[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    registeredDomains.list()
      .then(setDomains)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div>
        <div className="flex items-center justify-between mb-6">
          <h1 className="text-2xl font-semibold text-content-primary">Domains</h1>
        </div>
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-16 bg-surface-secondary border border-border-default rounded-lg animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Domains</h1>
        <Button onClick={() => navigate('/domains/search')}>
          <Search className="w-4 h-4" />
          Register Domain
        </Button>
      </div>

      {domains.length === 0 ? (
        <EmptyState
          icon={<Globe2 className="w-6 h-6" />}
          title="No domains yet"
          description="Search and register your first domain to get started."
          action={{ label: 'Register Domain', onClick: () => navigate('/domains/search') }}
        />
      ) : (
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          {domains.map((d) => {
            const sc = statusColors[d.status] || statusColors.pending;
            const tc = tldColors[d.tld] || '#6b7280';
            return (
              <Card
                key={d.id}
                hover
                className="rounded-none border-0 border-b border-border-subtle last:border-0"
                onClick={() => navigate(`/domains/${d.id}`)}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <div className="w-9 h-9 rounded-lg bg-surface-tertiary flex items-center justify-center">
                      <Globe2 className="w-4.5 h-4.5 text-content-tertiary" />
                    </div>
                    <div>
                      <div className="text-sm font-medium text-content-primary">{d.domain_name}</div>
                      <div className="flex items-center gap-2 mt-0.5">
                        <span
                          className="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded"
                          style={{ color: tc, backgroundColor: tc + '15' }}
                        >
                          .{d.tld}
                        </span>
                        <span className="text-xs text-content-tertiary">
                          {d.expires_at ? `Expires ${new Date(d.expires_at).toLocaleDateString()}` : 'No expiry'}
                        </span>
                      </div>
                    </div>
                  </div>
                  <span
                    className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium"
                    style={{ color: sc.color, backgroundColor: sc.bg }}
                  >
                    <span className="w-1.5 h-1.5 rounded-full" style={{ backgroundColor: sc.color }} />
                    {d.status.charAt(0).toUpperCase() + d.status.slice(1)}
                  </span>
                </div>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}
