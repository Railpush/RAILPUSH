import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Search } from 'lucide-react';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { StatusBadge } from '../components/ui/StatusBadge';
import { EmptyState } from '../components/ui/EmptyState';
import { ServiceSkeleton } from '../components/ui/Skeleton';
import { serviceTypeLabel, timeAgo } from '../lib/utils';
import type { Service, ManagedDatabase, ManagedKeyValue } from '../types';
import { services as servicesApi, databases as dbApi, keyvalue as kvApi } from '../lib/api';

export type DashboardScope =
  | 'all'
  | 'web-services'
  | 'static-sites'
  | 'private-services'
  | 'workers'
  | 'cron-jobs'
  | 'postgres'
  | 'keyvalue';

type ServiceScope = Exclude<DashboardScope, 'all' | 'postgres' | 'keyvalue'>;

interface DashboardProps {
  scope?: DashboardScope;
}

const SERVICE_SCOPE_MAP: Record<ServiceScope, Service['type']> = {
  'web-services': 'web',
  'static-sites': 'static',
  'private-services': 'pserv',
  workers: 'worker',
  'cron-jobs': 'cron',
};

const TITLE_BY_SCOPE: Record<DashboardScope, string> = {
  all: 'Dashboard',
  'web-services': 'Web Services',
  'static-sites': 'Static Sites',
  'private-services': 'Private Services',
  workers: 'Background Workers',
  'cron-jobs': 'Cron Jobs',
  postgres: 'PostgreSQL',
  keyvalue: 'Key Value',
};

const CREATE_ROUTE_BY_SCOPE: Record<DashboardScope, string> = {
  all: '/new/web',
  'web-services': '/new/web',
  'static-sites': '/new/static',
  'private-services': '/new/pserv',
  workers: '/new/worker',
  'cron-jobs': '/new/cron',
  postgres: '/new/postgres',
  keyvalue: '/new/keyvalue',
};

const CREATE_LABEL_BY_SCOPE: Record<DashboardScope, string> = {
  all: 'New',
  'web-services': 'New Web Service',
  'static-sites': 'New Static Site',
  'private-services': 'New Private Service',
  workers: 'New Worker',
  'cron-jobs': 'New Cron Job',
  postgres: 'New PostgreSQL',
  keyvalue: 'New Key Value',
};

export function Dashboard({ scope = 'all' }: DashboardProps) {
  const navigate = useNavigate();
  const [serviceList, setServiceList] = useState<Service[]>([]);
  const [dbList, setDbList] = useState<ManagedDatabase[]>([]);
  const [kvList, setKvList] = useState<ManagedKeyValue[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    Promise.all([
      servicesApi.list().catch(() => []),
      dbApi.list().catch(() => []),
      kvApi.list().catch(() => []),
    ]).then(([s, d, k]) => {
      setServiceList(s);
      setDbList(d);
      setKvList(k);
      setLoading(false);
    });
  }, []);

  const serviceSearch = search.toLowerCase();
  const filteredServices = useMemo(
    () => serviceList.filter((s) => s.name.toLowerCase().includes(serviceSearch)),
    [serviceList, serviceSearch]
  );
  const filteredDatabases = useMemo(
    () => dbList.filter((d) => d.name.toLowerCase().includes(serviceSearch)),
    [dbList, serviceSearch]
  );
  const filteredKeyValue = useMemo(
    () => kvList.filter((k) => k.name.toLowerCase().includes(serviceSearch)),
    [kvList, serviceSearch]
  );

  const serviceType = scope in SERVICE_SCOPE_MAP ? SERVICE_SCOPE_MAP[scope as ServiceScope] : null;

  const webServices = filteredServices.filter((s) => s.type === 'web');
  const staticSites = filteredServices.filter((s) => s.type === 'static');
  const workers = filteredServices.filter((s) => s.type === 'worker');
  const cronJobs = filteredServices.filter((s) => s.type === 'cron');
  const privateServices = filteredServices.filter((s) => s.type === 'pserv');

  const scopedServices = serviceType ? filteredServices.filter((s) => s.type === serviceType) : [];

  const pageTitle = TITLE_BY_SCOPE[scope];
  const createPath = CREATE_ROUTE_BY_SCOPE[scope];
  const createLabel = CREATE_LABEL_BY_SCOPE[scope];

  const allEmpty = serviceList.length === 0 && dbList.length === 0 && kvList.length === 0;
  const scopedEmpty =
    scope === 'postgres'
      ? dbList.length === 0
      : scope === 'keyvalue'
      ? kvList.length === 0
      : serviceType
      ? serviceList.filter((s) => s.type === serviceType).length === 0
      : allEmpty;

  const shouldShowSearch =
    scope === 'all'
      ? serviceList.length > 3 || dbList.length > 3 || kvList.length > 3
      : scope === 'postgres'
      ? dbList.length > 3
      : scope === 'keyvalue'
      ? kvList.length > 3
      : scopedServices.length > 3;

  if (loading) {
    return (
      <div>
        <div className="flex items-center justify-between mb-6">
          <h1 className="text-2xl font-semibold text-content-primary">{pageTitle}</h1>
        </div>
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          {Array.from({ length: 5 }).map((_, i) => <ServiceSkeleton key={i} />)}
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">{pageTitle}</h1>
        <button
          onClick={() => navigate(createPath)}
          className="inline-flex items-center gap-1.5 px-4 py-2 bg-brand text-white rounded-md text-sm font-medium hover:bg-brand-hover transition-colors cursor-pointer"
        >
          <Plus className="w-4 h-4" />
          {createLabel}
        </button>
      </div>

      {scopedEmpty ? (
        <EmptyState
          icon={<Plus className="w-6 h-6" />}
          title={`No ${pageTitle.toLowerCase()} yet`}
          description="Create your first resource to get started."
          action={{ label: createLabel, onClick: () => navigate(createPath) }}
        />
      ) : (
        <>
          {shouldShowSearch && (
            <div className="mb-4">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
                <input
                  type="text"
                  placeholder={`Search ${pageTitle.toLowerCase()}...`}
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="w-full bg-surface-tertiary border border-border-default rounded-md pl-9 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all"
                />
              </div>
            </div>
          )}

          {scope === 'all' && (
            <>
              <ServiceSection title="Web Services" items={webServices} navigate={navigate} />
              <ServiceSection title="Static Sites" items={staticSites} navigate={navigate} />
              <ServiceSection title="Private Services" items={privateServices} navigate={navigate} />
              <ServiceSection title="Background Workers" items={workers} navigate={navigate} />
              <ServiceSection title="Cron Jobs" items={cronJobs} navigate={navigate} />
              <DatabaseSection items={filteredDatabases} navigate={navigate} />
              <KeyValueSection items={filteredKeyValue} navigate={navigate} />
            </>
          )}

          {serviceType && <ServiceSection title={pageTitle} items={scopedServices} navigate={navigate} />}
          {scope === 'postgres' && <DatabaseSection items={filteredDatabases} navigate={navigate} force />}
          {scope === 'keyvalue' && <KeyValueSection items={filteredKeyValue} navigate={navigate} force />}
        </>
      )}
    </div>
  );
}

function ServiceSection({ title, items, navigate }: { title: string; items: Service[]; navigate: (p: string) => void }) {
  if (items.length === 0) return null;
  return (
    <div className="mb-6">
      <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-2 px-1">
        {title}
      </h2>
      <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
        {items.map((service) => (
          <div
            key={service.id}
            onClick={() => navigate(`/services/${service.id}`)}
            className="flex items-center px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary cursor-pointer transition-colors"
          >
            <ServiceIcon type={service.type} />
            <div className="ml-3 flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-semibold text-content-primary">{service.name}</span>
                <span className="text-[11px] px-2 py-0.5 rounded bg-surface-tertiary text-content-secondary">
                  {serviceTypeLabel(service.type)}
                </span>
              </div>
              <div className="text-xs text-content-secondary mt-0.5">
                {service.branch} &middot; {timeAgo(service.updated_at || service.created_at)}
              </div>
            </div>
            <StatusBadge status={service.status} size="sm" />
          </div>
        ))}
      </div>
    </div>
  );
}

function DatabaseSection({ items, navigate, force }: { items: ManagedDatabase[]; navigate: (p: string) => void; force?: boolean }) {
  if (items.length === 0 && !force) return null;
  return (
    <div className="mb-6">
      <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-2 px-1">
        PostgreSQL
      </h2>
      <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
        {items.map((db) => (
          <div
            key={db.id}
            onClick={() => navigate(`/databases/${db.id}`)}
            className="flex items-center px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary cursor-pointer transition-colors"
          >
            <ServiceIcon type="postgres" />
            <div className="ml-3 flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-semibold text-content-primary">{db.name}</span>
                <span className="text-[11px] px-2 py-0.5 rounded bg-surface-tertiary text-content-secondary">
                  PostgreSQL
                </span>
              </div>
              <div className="text-xs text-content-secondary mt-0.5">
                PostgreSQL {db.pg_version} &middot; {db.plan}
              </div>
            </div>
            <StatusBadge status={db.status === 'available' ? 'live' : 'created'} size="sm" />
          </div>
        ))}
      </div>
    </div>
  );
}

function KeyValueSection({ items, navigate, force }: { items: ManagedKeyValue[]; navigate: (p: string) => void; force?: boolean }) {
  if (items.length === 0 && !force) return null;
  return (
    <div className="mb-6">
      <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-2 px-1">
        Key Value
      </h2>
      <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
        {items.map((kv) => (
          <div
            key={kv.id}
            onClick={() => navigate(`/keyvalue/${kv.id}`)}
            className="flex items-center px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary cursor-pointer transition-colors"
          >
            <ServiceIcon type="keyvalue" />
            <div className="ml-3 flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-semibold text-content-primary">{kv.name}</span>
                <span className="text-[11px] px-2 py-0.5 rounded bg-surface-tertiary text-content-secondary">
                  Key Value
                </span>
              </div>
              <div className="text-xs text-content-secondary mt-0.5">{kv.plan}</div>
            </div>
            <StatusBadge status={kv.status === 'available' ? 'live' : 'created'} size="sm" />
          </div>
        ))}
      </div>
    </div>
  );
}
