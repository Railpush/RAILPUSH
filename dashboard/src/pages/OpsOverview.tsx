import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Activity, Mail, Server, Users } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Skeleton } from '../components/ui/Skeleton';
import { ApiError, ops } from '../lib/api';
import type { OpsOverview } from '../types';

function StatCard({ title, value, icon }: { title: string; value: string | number; icon: ReactNode }) {
  return (
    <Card className="p-5">
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">{title}</div>
          <div className="text-2xl font-semibold text-content-primary mt-2">{value}</div>
        </div>
        <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center text-brand">
          {icon}
        </div>
      </div>
    </Card>
  );
}

export function OpsOverviewPage() {
  const [data, setData] = useState<OpsOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    setError(null);
    setForbidden(false);
    ops
      .overview()
      .then(setData)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(e?.message || 'Failed to load ops overview');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const t = window.setInterval(() => load(), 30000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs uppercase tracking-[0.2em] text-content-tertiary font-semibold">Ops</p>
        <h1 className="text-3xl font-semibold text-content-primary mt-1">Overview</h1>
        <p className="text-sm text-content-secondary mt-1">Platform-wide counters and queue health.</p>
      </div>

      {forbidden ? (
        <Card className="p-8 text-center text-sm text-content-secondary">
          Your account doesn’t have access to ops pages.
          <div className="text-xs text-content-tertiary mt-2">
            Ask an admin to set your user role to <code className="font-mono">admin</code> or <code className="font-mono">ops</code>.
          </div>
        </Card>
      ) : error ? (
        <Card className="p-8 text-center text-sm text-status-error">{error}</Card>
      ) : loading || !data ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Card key={i} className="p-5">
              <Skeleton className="w-32 h-3" />
              <Skeleton className="w-20 h-8 mt-3" />
            </Card>
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          <StatCard title="Users" value={data.users_total} icon={<Users className="w-5 h-5" />} />
          <StatCard title="Workspaces" value={data.workspaces_total} icon={<Server className="w-5 h-5" />} />
          <StatCard title="Services" value={data.services_total} icon={<Activity className="w-5 h-5" />} />
          <StatCard title="Deploys Pending" value={data.deploys_pending} icon={<Activity className="w-5 h-5" />} />
          <StatCard title="Deploys Failed (24h)" value={data.deploys_failed_24h} icon={<Activity className="w-5 h-5" />} />
          <StatCard title="Email Pending/Retry/Dead" value={`${data.email_pending}/${data.email_retry}/${data.email_dead}`} icon={<Mail className="w-5 h-5" />} />
        </div>
      )}
    </div>
  );
}
