import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Search, Server, Database, Key, Activity, ArrowUpRight, Zap, FolderKanban } from 'lucide-react';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { StatusBadge } from '../components/ui/StatusBadge';
import { EmptyState } from '../components/ui/EmptyState';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { serviceTypeLabel, timeAgo } from '../lib/utils';
import type { Service, ManagedDatabase, ManagedKeyValue, ServiceStatus, DeployStatus, Project } from '../types';
import { services as servicesApi, databases as dbApi, keyvalue as kvApi, projects as projectsApi } from '../lib/api';

function isSuspendedService(svc: Service): boolean {
  return Boolean(svc.is_suspended) || svc.status === 'suspended';
}

function effectiveServiceStatus(svc: Service): ServiceStatus {
  return isSuspendedService(svc) ? 'suspended' : svc.status;
}

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
  all: 'Mission Control',
  'web-services': 'Web Services',
  'static-sites': 'Static Sites',
  'private-services': 'Private Services',
  workers: 'Background Workers',
  'cron-jobs': 'Cron Jobs',
  postgres: 'PostgreSQL Databases',
  keyvalue: 'Key Value Stores',
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
  all: 'New Service',
  'web-services': 'New Web Service',
  'static-sites': 'New Static Site',
  'private-services': 'New Private Service',
  workers: 'New Worker',
  'cron-jobs': 'New Cron Job',
  postgres: 'New Database',
  keyvalue: 'New Key Value',
};

export function Dashboard({ scope = 'all' }: DashboardProps) {
  const navigate = useNavigate();
  const [serviceList, setServiceList] = useState<Service[]>([]);
  const [dbList, setDbList] = useState<ManagedDatabase[]>([]);
  const [kvList, setKvList] = useState<ManagedKeyValue[]>([]);
  const [projectList, setProjectList] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | 'active' | 'suspended'>('all');

  useEffect(() => {
    setLoading(true);
    Promise.all([
      servicesApi.list().catch(() => []),
      dbApi.list().catch(() => []),
      kvApi.list().catch(() => []),
      projectsApi.list().catch(() => []),
    ]).then(([s, d, k, p]) => {
      setServiceList(s);
      setDbList(d);
      setKvList(k);
      setProjectList(p);
      setLoading(false);
    });
	  }, [scope]);

	  const query = search.trim().toLowerCase();

	  const serviceType = scope in SERVICE_SCOPE_MAP ? SERVICE_SCOPE_MAP[scope as ServiceScope] : null;
	  const servicesInScope = useMemo(() => {
	    if (scope === 'all') return serviceList;
    if (serviceType) return serviceList.filter((s) => s.type === serviceType);
    return [];
  }, [serviceList, scope, serviceType]);

	  const filteredServices = useMemo(
	    () => servicesInScope.filter((s) => s.name.toLowerCase().includes(query)),
	    [servicesInScope, query]
	  );
	  const filteredByStatus = useMemo(() => filteredServices.filter((s) => {
	    if (statusFilter === 'all') return true;
	    if (statusFilter === 'suspended') return isSuspendedService(s);
	    return !isSuspendedService(s);
	  }), [filteredServices, statusFilter]);
	  const filteredDatabases = useMemo(
	    () => dbList.filter((d) => d.name.toLowerCase().includes(query)),
	    [dbList, query]
	  );
  const filteredKeyValue = useMemo(
    () => kvList.filter((k) => k.name.toLowerCase().includes(query)),
    [kvList, query]
  );

  const scopedServices = serviceType ? servicesInScope : [];

  const pageTitle = TITLE_BY_SCOPE[scope];
  const createPath = CREATE_ROUTE_BY_SCOPE[scope];
  const createLabel = CREATE_LABEL_BY_SCOPE[scope];

	  const allEmpty = serviceList.length === 0 && dbList.length === 0 && kvList.length === 0 && projectList.length === 0;

	  const hasProjects = scope === 'all' && projectList.length > 0;
	  const projectIDs = useMemo(() => new Set(projectList.map((p) => p.id)), [projectList]);

	  const servicesByProjectID = useMemo(() => {
	    const out = new Map<string, Service[]>();
	    for (const svc of serviceList) {
	      if (!svc.project_id) continue;
	      const key = svc.project_id;
	      const list = out.get(key) || [];
	      list.push(svc);
	      out.set(key, list);
	    }
	    for (const [, list] of out) {
	      list.sort((a, b) => {
	        const ta = new Date(a.updated_at || a.created_at).getTime();
	        const tb = new Date(b.updated_at || b.created_at).getTime();
	        return tb - ta;
	      });
	    }
	    return out;
	  }, [serviceList]);

	  const projectCards = useMemo(() => {
	    if (!hasProjects) return [];
	    const q = query;
	    const out: Array<{ project: Project; services: Service[]; servicesForDisplay: Service[]; lastTouchedAt?: string }> = [];

	    for (const project of projectList) {
	      const services = servicesByProjectID.get(project.id) || [];
	      const nameMatch = q !== '' && (project.name || '').toLowerCase().includes(q);
	      const matching = q === '' ? services : services.filter((svc) => svc.name.toLowerCase().includes(q));

	      if (q !== '' && !nameMatch && matching.length === 0) {
	        continue;
	      }

	      const servicesForDisplay = nameMatch || q === '' ? services : matching;
	      const lastTouchedAt = services.reduce((best: string | undefined, svc) => {
	        const t = svc.updated_at || svc.created_at;
	        if (!t) return best;
	        if (!best) return t;
	        return new Date(t).getTime() > new Date(best).getTime() ? t : best;
	      }, undefined);

	      out.push({ project, services, servicesForDisplay, lastTouchedAt });
	    }

	    out.sort((a, b) => {
	      const ta = a.lastTouchedAt ? new Date(a.lastTouchedAt).getTime() : new Date(a.project.created_at).getTime();
	      const tb = b.lastTouchedAt ? new Date(b.lastTouchedAt).getTime() : new Date(b.project.created_at).getTime();
	      if (tb !== ta) return tb - ta;
	      return (a.project.name || '').localeCompare(b.project.name || '');
	    });

	    return out;
	  }, [hasProjects, projectList, servicesByProjectID, query]);

	  const standaloneServicesAll = useMemo(() => {
	    if (!hasProjects) return [];
	    return serviceList.filter((svc) => !svc.project_id || !projectIDs.has(svc.project_id));
	  }, [hasProjects, projectIDs, serviceList]);

	  const filteredStandaloneServices = useMemo(() => {
	    if (!hasProjects) return [];
	    return standaloneServicesAll.filter((svc) => svc.name.toLowerCase().includes(query));
	  }, [hasProjects, standaloneServicesAll, query]);

	  const filteredStandaloneByStatus = useMemo(() => {
	    if (!hasProjects) return [];
	    return filteredStandaloneServices.filter((svc) => {
	      if (statusFilter === 'all') return true;
	      if (statusFilter === 'suspended') return isSuspendedService(svc);
	      return !isSuspendedService(svc);
	    });
	  }, [filteredStandaloneServices, statusFilter]);

	  // Logic to determine if we show search bar
	  const shouldShowSearch =
	    scope === 'all'
	      ? serviceList.length > 3 || dbList.length > 3 || kvList.length > 3 || projectList.length > 3
      : scope === 'postgres'
        ? dbList.length > 3
        : scope === 'keyvalue'
          ? kvList.length > 3
          : scopedServices.length > 3;

	  if (loading) {
	    return (
	      <div className="space-y-8 animate-pulse">
	        <div className="flex items-center justify-between">
	          <div className="h-8 w-48 bg-surface-tertiary rounded-md" />
          <div className="h-10 w-32 bg-surface-tertiary rounded-md" />
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="h-32 bg-surface-tertiary rounded-xl" />
          <div className="h-32 bg-surface-tertiary rounded-xl" />
          <div className="h-32 bg-surface-tertiary rounded-xl" />
        </div>
        <div className="h-[400px] bg-surface-tertiary rounded-xl" />
      </div>
	    );
	  }

	  // Calculate stats
	  const activeServices = serviceList.filter(s => s.status !== 'suspended').length;
	  const healthyServices = serviceList.filter(s => s.status === 'live').length;

  return (
    <div className="space-y-8 pb-10">

      {/* Header Section */}
      <div className="flex flex-wrap items-center justify-between gap-4 animate-enter">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-content-primary mb-1">{pageTitle}</h1>
          <p className="text-content-secondary text-sm">Overview of your infrastructure and resources</p>
        </div>
        <div className="flex items-center gap-3">
          {shouldShowSearch && (
            <div className="relative hidden md:block group">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary group-hover:text-brand transition-colors" />
              <input
                type="text"
                placeholder="Search resources..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9 pr-4 py-2 bg-surface-tertiary/50 border border-border-default rounded-lg text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:ring-2 focus:ring-brand/20 w-64 transition-all"
              />
            </div>
          )}
          <Button onClick={() => navigate(createPath)} variant="primary">
            <Plus className="w-4 h-4" />
            {createLabel}
          </Button>
        </div>
      </div>

      {allEmpty && scope === 'all' ? (
        <div className="animate-enter animate-enter-delay-1">
          <EmptyState
            icon={<Server className="w-12 h-12 text-brand" />}
            title="Welcome to RailPush"
            description="Launch your first service to get started deploying your applications."
            action={{ label: createLabel, onClick: () => navigate(createPath) }}
          />
        </div>
      ) : (
        <>
          {/* Stats Grid - Only show on main dashboard or relevant sections */}
          {scope === 'all' && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 animate-enter animate-enter-delay-1">
              <StatsCard
                label="Total Services"
                value={serviceList.length}
                icon={<Server className="w-5 h-5 text-brand" />}
                trend={activeServices > 0 ? `${activeServices} Active` : undefined}
                trendColor="text-brand"
              />
              <StatsCard
                label="System Health"
                value={`${Math.round((healthyServices / (serviceList.length || 1)) * 100)}%`}
                icon={<Activity className="w-5 h-5 text-emerald-500" />}
                subtext="Services Operational"
              />
              <StatsCard
                label="Databases"
                value={dbList.length}
                icon={<Database className="w-5 h-5 text-blue-400" />}
                onClick={() => navigate('/new/postgres')}
                actionIcon={<Plus className="w-3.5 h-3.5" />}
              />
              <StatsCard
                label="Key Value Stores"
                value={kvList.length}
                icon={<Key className="w-5 h-5 text-amber-500" />}
                onClick={() => navigate('/new/keyvalue')}
                actionIcon={<Plus className="w-3.5 h-3.5" />}
              />
            </div>
          )}

	          {/* Main Services Table */}
	          {scope === 'all' && hasProjects && (
	            <div className="space-y-4 animate-enter animate-enter-delay-2">
	              <div className="flex items-center justify-between px-1">
	                <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary">Projects</h2>
	                <Button variant="ghost" size="sm" onClick={() => navigate('/projects')} className="text-xs">
	                  View All <ArrowUpRight className="w-3 h-3 ml-1" />
	                </Button>
	              </div>

	              {projectCards.length === 0 ? (
	                <Card className="py-10 text-center text-sm text-content-secondary">
	                  No projects match your search.
	                </Card>
	              ) : (
	                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
	                  {projectCards.map(({ project, services, servicesForDisplay, lastTouchedAt }) => (
	                    <ProjectCard
	                      key={project.id}
	                      project={project}
	                      services={services}
	                      servicesForDisplay={servicesForDisplay}
	                      lastTouchedAt={lastTouchedAt}
	                      onClick={() => navigate(`/projects/${project.id}`)}
	                    />
	                  ))}
	                </div>
	              )}
	            </div>
	          )}

	          {(scope === 'all' || serviceType) && (hasProjects ? standaloneServicesAll.length > 0 : serviceList.length > 0) && (
	            <div className="space-y-4 animate-enter animate-enter-delay-2">
	              <div className="flex items-center justify-between px-1">
	                <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary">
	                  {scope === 'all'
	                    ? (hasProjects ? 'Standalone Services' : 'Active Services')
	                    : TITLE_BY_SCOPE[scope]}
	                </h2>

	                {/* Filter Tabs */}
	                <div className="flex items-center p-1 bg-surface-tertiary/50 rounded-lg border border-border-default/50">
	                  {(['all', 'active', 'suspended'] as const).map((f) => (
                    <button
                      key={f}
                      onClick={() => setStatusFilter(f)}
                      className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${statusFilter === f
                          ? 'bg-surface-elevated text-content-primary shadow-sm ring-1 ring-border-default'
                          : 'text-content-secondary hover:text-content-primary'
                        }`}
                    >
                      {f.charAt(0).toUpperCase() + f.slice(1)}
                    </button>
                  ))}
                </div>
              </div>

	              <Card className="p-0 overflow-hidden shadow-lg shadow-black/20">
	                <div className="overflow-x-auto">
	                  <table className="w-full text-left text-sm">
	                    <thead>
	                      <tr className="border-b border-border-default bg-surface-tertiary/30 text-content-tertiary text-xs uppercase tracking-wider font-semibold">
                        <th className="px-6 py-3">Name</th>
                        <th className="px-6 py-3">Status</th>
                        <th className="px-6 py-3">Type</th>
                        <th className="px-6 py-3">Runtime</th>
                        <th className="px-6 py-3 text-right">Updated</th>
                      </tr>
                    </thead>
	                    <tbody className="divide-y divide-border-default/40">
	                      {(hasProjects ? filteredStandaloneByStatus : filteredByStatus).length === 0 ? (
	                        <tr>
	                          <td colSpan={5} className="px-6 py-8 text-center text-content-secondary">
	                            No services match your filters.
	                          </td>
	                        </tr>
		                      ) : (
		                        (hasProjects ? filteredStandaloneByStatus : filteredByStatus).map((service) => (
		                          <tr
		                            key={service.id}
	                            onClick={() => navigate(`/services/${service.id}`)}
	                            className="group hover:bg-surface-tertiary/40 transition-colors cursor-pointer"
	                          >
                            <td className="px-6 py-4">
                              <div className="flex items-center gap-3">
                                <div className="p-2 rounded-lg bg-surface-tertiary ring-1 ring-border-default group-hover:ring-brand/30 transition-all">
                                  <ServiceIcon type={service.type} size="md" />
                                </div>
                                <div>
                                  <div className="font-semibold text-content-primary group-hover:text-brand transition-colors">{service.name}</div>
                                  <div className="text-xs text-content-tertiary flex items-center gap-1.5 mt-0.5">
                                    <span className="font-mono opacity-80">main</span>
                                  </div>
                                </div>
                              </div>
                            </td>
		                            <td className="px-6 py-4">
		                              <StatusBadge status={effectiveServiceStatus(service)} />
		                            </td>
		                            <td className="px-6 py-4 text-content-secondary">
		                              {serviceTypeLabel(service.type)}
		                            </td>
                            <td className="px-6 py-4">
                              <div className="inline-flex items-center gap-1.5 px-2 py-1 rounded bg-surface-tertiary/80 border border-border-subtle text-xs font-medium text-content-secondary group-hover:border-brand/20 transition-colors">
                                <Zap className="w-3 h-3 text-amber-400" />
                                {service.runtime || 'Docker'}
                              </div>
                            </td>
                            <td className="px-6 py-4 text-right text-content-tertiary text-xs font-mono">
                              {timeAgo(service.updated_at || service.created_at)}
                            </td>
                          </tr>
	                        ))
	                      )}
	                    </tbody>
	                  </table>
	                </div>
	              </Card>
	            </div>
	          )}

          {/* Databases Section */}
          {(scope === 'all' || scope === 'postgres') && filteredDatabases.length > 0 && (
            <div className="mt-8 space-y-4 animate-enter animate-enter-delay-3">
              <div className="flex items-center justify-between px-1">
                <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary">Databases</h2>
                {scope === 'all' && (
                  <Button variant="ghost" size="sm" onClick={() => navigate('/postgres')} className="text-xs">
                    View All <ArrowUpRight className="w-3 h-3 ml-1" />
                  </Button>
                )}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {filteredDatabases.map(db => (
                  <ResourceCard
                    key={db.id}
                    title={db.name}
                    subtitle={`v${db.pg_version}`}
                    icon={<Database className="w-5 h-5 text-blue-400" />}
                    status={db.status === 'available' ? 'live' : 'created'}
                    meta={[db.plan]}
                    onClick={() => navigate(`/databases/${db.id}`)}
                  />
                ))}
              </div>
            </div>
          )}

          {/* Key Value Section */}
          {(scope === 'all' || scope === 'keyvalue') && filteredKeyValue.length > 0 && (
            <div className="mt-8 space-y-4 animate-enter animate-enter-delay-3">
              <div className="flex items-center justify-between px-1">
                <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary">Key Value Stores</h2>
                {scope === 'all' && (
                  <Button variant="ghost" size="sm" onClick={() => navigate('/keyvalue')} className="text-xs">
                    View All <ArrowUpRight className="w-3 h-3 ml-1" />
                  </Button>
                )}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {filteredKeyValue.map(kv => (
                  <ResourceCard
                    key={kv.id}
                    title={kv.name}
                    subtitle="Redis Compatible"
                    icon={<Key className="w-5 h-5 text-amber-500" />}
                    status={kv.status === 'available' ? 'live' : 'created'}
                    meta={[kv.plan]}
                    onClick={() => navigate(`/keyvalue/${kv.id}`)}
                  />
                ))}
              </div>
            </div>
          )}

        </>
      )}
    </div>
  );
}

// Subcomponents

type ProjectCardProps = {
  project: Project;
  services: Service[];
  servicesForDisplay: Service[];
  lastTouchedAt?: string;
  onClick?: () => void;
};

function ProjectCard({ project, services, servicesForDisplay, lastTouchedAt, onClick }: ProjectCardProps) {
  const total = services.length;
  const suspended = services.filter((svc) => isSuspendedService(svc)).length;
  const active = services.filter((svc) => !isSuspendedService(svc) && svc.status !== 'deactivated').length;
  const live = services.filter((svc) => !isSuspendedService(svc) && svc.status === 'live').length;

  const health = (() => {
    if (total === 0) {
      return { label: 'Empty', className: 'text-content-tertiary bg-surface-tertiary/40 border-border-default/50' };
    }
    if (active === 0) {
      return { label: 'Paused', className: 'text-status-warning bg-status-warning/10 border-status-warning/20' };
    }
    if (live === active) {
      return { label: 'Healthy', className: 'text-status-success bg-status-success/10 border-status-success/20' };
    }
    if (live === 0) {
      return { label: 'Down', className: 'text-status-error bg-status-error/10 border-status-error/20' };
    }
    return { label: `${live}/${active} live`, className: 'text-status-warning bg-status-warning/10 border-status-warning/20' };
  })();

  const visible = servicesForDisplay.slice(0, 3);
  const remaining = Math.max(0, servicesForDisplay.length - visible.length);

  return (
    <Card hover onClick={onClick} className="relative group">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="p-2.5 rounded-lg bg-surface-tertiary ring-1 ring-border-default group-hover:ring-brand/30 transition-all">
            <FolderKanban className="w-5 h-5 text-brand" />
          </div>
          <div className="min-w-0">
            <h3 className="font-semibold text-content-primary group-hover:text-brand transition-colors truncate">
              {project.name || 'Untitled Project'}
            </h3>
            <p className="text-xs text-content-tertiary mt-0.5">
              {total === 0 ? 'No services yet' : `${active} active \u00b7 ${suspended} suspended`}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <span className={`text-[11px] px-2 py-0.5 rounded-full border ${health.className}`}>
            {health.label}
          </span>
          <ArrowUpRight className="w-4 h-4 text-content-tertiary group-hover:text-content-primary transition-colors" />
        </div>
      </div>

      <div className="mt-4 space-y-2">
        {total === 0 ? (
          <div className="text-xs text-content-tertiary">
            Create a service in this project to get started.
          </div>
        ) : (
          visible.map((svc) => (
            <div key={svc.id} className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 min-w-0">
                <ServiceIcon type={svc.type} size="sm" />
                <div className="min-w-0">
                  <div className="text-[13px] font-semibold text-content-primary truncate">{svc.name}</div>
                  <div className="text-[11px] text-content-tertiary">{serviceTypeLabel(svc.type)}</div>
                </div>
              </div>
              <StatusBadge status={effectiveServiceStatus(svc)} size="sm" pulse={false} />
            </div>
          ))
        )}

        {remaining > 0 && (
          <div className="text-xs text-content-tertiary pl-1">+{remaining} more</div>
        )}
      </div>

      <div className="mt-4 pt-3 border-t border-border-default/50 flex items-center justify-between text-xs">
        <span className="text-content-tertiary">
          {lastTouchedAt ? `Updated ${timeAgo(lastTouchedAt)}` : `Created ${timeAgo(project.created_at)}`}
        </span>
        <span className="text-content-tertiary group-hover:text-content-primary transition-colors">
          Open project &rarr;
        </span>
      </div>
    </Card>
  );
}

type StatsCardProps = {
  label: string;
  value: string | number;
  icon: ReactNode;
  trend?: string;
  trendColor?: string;
  subtext?: string;
  onClick?: () => void;
  actionIcon?: ReactNode;
};

function StatsCard({ label, value, icon, trend, trendColor, subtext, onClick, actionIcon }: StatsCardProps) {
  return (
    <div
      onClick={onClick}
      className={`glass-panel p-5 rounded-xl relative overflow-hidden group transition-all duration-300 ${onClick ? 'cursor-pointer hover:bg-surface-tertiary/60' : ''}`}
    >
      <div className="flex justify-between items-start mb-4">
        <div className="p-2 rounded-lg bg-surface-tertiary/50 ring-1 ring-border-default/50 group-hover:ring-brand/20 transition-all">
          {icon}
        </div>
        {onClick && (
          <div className="p-1.5 rounded-full bg-surface-tertiary/30 text-content-tertiary group-hover:text-content-primary transition-colors">
            {actionIcon || <ArrowUpRight className="w-3.5 h-3.5" />}
          </div>
        )}
      </div>
      <div>
        <div className="text-3xl font-bold text-content-primary tracking-tight">{value}</div>
        <div className="text-sm text-content-secondary font-medium mt-1">{label}</div>
        {trend && (
          <div className={`text-xs mt-2 font-semibold ${trendColor}`}>
            {trend}
          </div>
        )}
        {subtext && (
          <div className="text-xs mt-2 text-content-tertiary">
            {subtext}
          </div>
        )}
      </div>
    </div>
  )
}

type ResourceCardProps = {
  title: string;
  subtitle?: string;
  icon: ReactNode;
  status: ServiceStatus | DeployStatus;
  meta: string[];
  onClick?: () => void;
};

function ResourceCard({ title, subtitle, icon, status, meta, onClick }: ResourceCardProps) {
  return (
    <Card hover onClick={onClick} className="relative group">
      <div className="flex items-start justify-between">
        <div className="flex gap-3">
          <div className="p-2.5 rounded-lg bg-surface-tertiary ring-1 ring-border-default group-hover:ring-brand/30 transition-all">
            {icon}
          </div>
          <div>
            <h3 className="font-semibold text-content-primary group-hover:text-brand transition-colors">{title}</h3>
            <p className="text-xs text-content-tertiary mt-0.5">{subtitle}</p>
          </div>
        </div>
        <StatusBadge status={status} size="sm" />
      </div>

      <div className="mt-4 pt-3 border-t border-border-default/50 flex items-center justify-between text-xs">
        <div className="flex gap-2 text-content-secondary">
          {meta.map((m: string, i: number) => (
            <span key={i} className="px-1.5 py-0.5 rounded bg-surface-tertiary/50 border border-border-default/50">
              {m}
            </span>
          ))}
        </div>
        <span className="text-content-tertiary group-hover:text-content-primary transition-colors">
          Manage &rarr;
        </span>
      </div>
    </Card>
  )
}
