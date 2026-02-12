import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Database, KeyRound, Server } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { StatusBadge } from '../components/ui/StatusBadge';
import { blueprints as blueprintsApi, projects as projectsApi, services as servicesApi } from '../lib/api';
import { serviceTypeLabel, timeAgo } from '../lib/utils';
import { toast } from 'sonner';
import type { Blueprint, BlueprintResource, Project, Service } from '../types';

type RelatedResourceType = BlueprintResource['resource_type'];

interface RelatedResource {
  key: string;
  id: string;
  name: string;
  type: RelatedResourceType;
  sourceBlueprint: string;
}

function normalizeName(value: string): string {
  return value.toLowerCase().trim();
}

function resourcePath(type: RelatedResourceType, id: string): string {
  if (type === 'service') return `/services/${id}`;
  if (type === 'database') return `/databases/${id}`;
  return `/keyvalue/${id}`;
}

function resourceLabel(type: RelatedResourceType): string {
  if (type === 'service') return 'Service';
  if (type === 'database') return 'PostgreSQL';
  return 'Key Value';
}

function ResourceGlyph({ type }: { type: RelatedResourceType }) {
  if (type === 'service') return <Server className="w-4 h-4 text-content-secondary" />;
  if (type === 'database') return <Database className="w-4 h-4 text-content-secondary" />;
  return <KeyRound className="w-4 h-4 text-content-secondary" />;
}

export function ProjectDetail() {
  const navigate = useNavigate();
  const { projectId } = useParams<{ projectId: string }>();

  const [loading, setLoading] = useState(true);
  const [project, setProject] = useState<Project | null>(null);
  const [projectServices, setProjectServices] = useState<Service[]>([]);
  const [relatedResources, setRelatedResources] = useState<RelatedResource[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      if (!projectId) {
        setLoading(false);
        return;
      }

      setLoading(true);
      try {
        const projectData = await projectsApi.get(projectId);
        const [services, blueprints] = await Promise.all([
          servicesApi.list().catch(() => [] as Service[]),
          blueprintsApi.list().catch(() => [] as Blueprint[]),
        ]);

        const servicesInProject = services.filter((svc) => svc.project_id === projectData.id);
        const matchingBlueprints = blueprints.filter(
          (bp) => normalizeName(bp.name || '') === normalizeName(projectData.name || ''),
        );

        let resources: RelatedResource[] = [];
        if (matchingBlueprints.length > 0) {
          const details = await Promise.all(
            matchingBlueprints.map(async (bp) => blueprintsApi.get(bp.id).catch(() => null)),
          );

          const serviceIDsInProject = new Set(servicesInProject.map((svc) => svc.id));
          const byKey = new Map<string, RelatedResource>();

          for (const detail of details) {
            if (!detail) continue;
            const linked = detail.resources || [];
            for (const resource of linked) {
              if (!resource.resource_id || !resource.resource_type) continue;
              if (resource.resource_type === 'service' && serviceIDsInProject.has(resource.resource_id)) {
                continue;
              }
              const key = `${resource.resource_type}:${resource.resource_id}`;
              if (byKey.has(key)) continue;
              byKey.set(key, {
                key,
                id: resource.resource_id,
                name: resource.resource_name || resource.resource_id,
                type: resource.resource_type,
                sourceBlueprint: detail.name || 'Blueprint',
              });
            }
          }

          resources = Array.from(byKey.values());
        }

        if (cancelled) return;

        setProject(projectData);
        setProjectServices(servicesInProject);
        setRelatedResources(resources);
      } catch {
        if (!cancelled) {
          toast.error('Failed to load project');
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20 text-sm text-content-tertiary">
        Loading project...
      </div>
    );
  }

  if (!project) {
    return (
      <div className="text-center py-20 text-sm text-content-secondary">
        Project not found.
      </div>
    );
  }

  const environments = project.environments || [];

  return (
    <div>
      <button
        onClick={() => navigate('/projects')}
        className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        Back to Projects
      </button>

      <div className="flex items-start justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold text-content-primary">{project.name || 'Untitled Project'}</h1>
          <p className="text-sm text-content-secondary mt-1">
            {projectServices.length} service{projectServices.length === 1 ? '' : 's'} · {relatedResources.length} linked resource{relatedResources.length === 1 ? '' : 's'}
          </p>
        </div>
        <Button onClick={() => navigate('/new/web')}>New Service</Button>
      </div>

      <h2 className="text-sm font-semibold text-content-primary mb-2">Services ({projectServices.length})</h2>
      {projectServices.length === 0 ? (
        <Card padding="md" className="mb-6">
          <p className="text-sm text-content-secondary">No services are assigned to this project yet.</p>
        </Card>
      ) : (
        <div className="space-y-2 mb-6">
          {projectServices.map((service) => (
            <Card key={service.id} hover onClick={() => navigate(`/services/${service.id}`)} padding="sm">
              <div className="flex items-center gap-3">
                <ServiceIcon type={service.type} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{service.name}</div>
                  <div className="text-xs text-content-secondary mt-0.5">
                    {serviceTypeLabel(service.type)} · {service.branch} · {timeAgo(service.updated_at || service.created_at)}
                  </div>
                </div>
                <StatusBadge status={service.status} size="sm" />
              </div>
            </Card>
          ))}
        </div>
      )}

      <h2 className="text-sm font-semibold text-content-primary mb-2">Linked Resources ({relatedResources.length})</h2>
      {relatedResources.length === 0 ? (
        <Card padding="md" className="mb-6">
          <p className="text-sm text-content-secondary">
            No additional resources linked via project blueprints.
          </p>
        </Card>
      ) : (
        <div className="space-y-2 mb-6">
          {relatedResources.map((resource) => (
            <Card key={resource.key} hover onClick={() => navigate(resourcePath(resource.type, resource.id))} padding="sm">
              <div className="flex items-center gap-3">
                <ResourceGlyph type={resource.type} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-semibold text-content-primary truncate">{resource.name}</div>
                  <div className="text-xs text-content-secondary mt-0.5">
                    {resourceLabel(resource.type)} · from {resource.sourceBlueprint}
                  </div>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <h2 className="text-sm font-semibold text-content-primary mb-2">Environments ({environments.length})</h2>
      {environments.length === 0 ? (
        <Card padding="md">
          <p className="text-sm text-content-secondary">No environments configured for this project.</p>
        </Card>
      ) : (
        <Card padding="md">
          <div className="flex flex-wrap gap-2">
            {environments.map((env) => (
              <span
                key={env.id}
                className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-medium bg-surface-tertiary text-content-primary"
              >
                {env.name}
                {env.is_protected && (
                  <span className="text-[10px] text-content-tertiary">protected</span>
                )}
              </span>
            ))}
          </div>
        </Card>
      )}
    </div>
  );
}
