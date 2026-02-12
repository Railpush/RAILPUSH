import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ChevronDown, FolderKanban, MoreVertical, Pencil, Plus, Search, Trash2 } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Dropdown } from '../components/ui/Dropdown';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { ServiceIcon } from '../components/ui/ServiceIcon';
import { StatusBadge } from '../components/ui/StatusBadge';
import { blueprints as blueprintsApi, projectFolders as projectFoldersApi, projects as projectsApi, services as servicesApi } from '../lib/api';
import { cn, serviceTypeLabel, timeAgo } from '../lib/utils';
import { toast } from 'sonner';
import type { Blueprint, Project, ProjectFolder, Service, ServiceStatus } from '../types';

type HealthTone = 'healthy' | 'partial' | 'down' | 'empty';
type CardKind = 'project' | 'blueprint';
type ServiceTab = 'active' | 'suspended' | 'all';

interface ProjectCard {
  key: string;
  kind: CardKind;
  name: string;
  services: Service[];
  createdAt?: string;
  folderId?: string;
  projectId?: string;
  blueprintId?: string;
  editable?: boolean;
}

interface ProjectData {
  cards: ProjectCard[];
  ungroupedServices: Service[];
}

const ROOT_FOLDER_VALUE = '__no_folder__';

function summarizeHealth(services: Service[]): { label: string; tone: HealthTone } {
  const active = services.filter((svc) => !svc.is_suspended && svc.status !== 'deactivated');
  if (active.length === 0) {
    return { label: 'No active services', tone: 'empty' };
  }

  const liveCount = active.filter((svc) => svc.status === 'live').length;
  if (liveCount === active.length) {
    return { label: 'All services are up and running', tone: 'healthy' };
  }
  if (liveCount === 0) {
    return { label: 'No services are live', tone: 'down' };
  }
  return { label: `${liveCount}/${active.length} services are live`, tone: 'partial' };
}

function buildProjectData(
  projects: Project[],
  services: Service[],
  blueprints: Array<Blueprint & { resources?: Array<{ resource_id: string; resource_type: string }> }>,
): ProjectData {
  const cardsByKey = new Map<string, ProjectCard>();
  const assignedServiceIDs = new Set<string>();

  for (const project of projects) {
    cardsByKey.set(`project:${project.id}`, {
      key: `project:${project.id}`,
      kind: 'project',
      projectId: project.id,
      folderId: project.folder_id || undefined,
      editable: true,
      name: project.name || 'Untitled Project',
      services: [],
      createdAt: project.created_at,
    });
  }

  for (const service of services) {
    if (!service.project_id) {
      continue;
    }

    const key = `project:${service.project_id}`;
    const existing = cardsByKey.get(key);
    if (existing) {
      existing.services.push(service);
    } else {
      cardsByKey.set(key, {
        key,
        kind: 'project',
        projectId: service.project_id,
        editable: false,
        name: 'Untitled Project',
        services: [service],
      });
    }

    assignedServiceIDs.add(service.id);
  }

  const projectNameSet = new Set(projects.map((p) => p.name.toLowerCase().trim()).filter(Boolean));

  for (const blueprint of blueprints) {
    const serviceResourceIDs = new Set(
      (blueprint.resources || [])
        .filter((res) => res.resource_type === 'service' && res.resource_id)
        .map((res) => res.resource_id),
    );

    const blueprintServices = services.filter((svc) => serviceResourceIDs.has(svc.id) && !assignedServiceIDs.has(svc.id));
    if (blueprintServices.length === 0 && projectNameSet.has((blueprint.name || '').toLowerCase().trim())) {
      continue;
    }

    const key = `blueprint:${blueprint.id}`;
    cardsByKey.set(key, {
      key,
      kind: 'blueprint',
      blueprintId: blueprint.id,
      name: blueprint.name || 'Untitled Project',
      services: blueprintServices,
      createdAt: blueprint.created_at,
    });

    for (const service of blueprintServices) {
      assignedServiceIDs.add(service.id);
    }
  }

  const cards = Array.from(cardsByKey.values()).sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    if (tb !== ta) return tb - ta;
    return a.name.localeCompare(b.name);
  });

  const ungroupedServices = services
    .filter((svc) => !assignedServiceIDs.has(svc.id))
    .sort((a, b) => {
      const ta = new Date(a.updated_at || a.created_at).getTime();
      const tb = new Date(b.updated_at || b.created_at).getTime();
      return tb - ta;
    });

  return { cards, ungroupedServices };
}

function isSuspended(svc: Service): boolean {
  return svc.is_suspended || svc.status === 'suspended';
}

function effectiveStatus(svc: Service): ServiceStatus {
  return isSuspended(svc) ? 'suspended' : svc.status;
}

export function Projects() {
  const navigate = useNavigate();

  const [loading, setLoading] = useState(true);
  const [cards, setCards] = useState<ProjectCard[]>([]);
  const [folders, setFolders] = useState<ProjectFolder[]>([]);
  const [ungroupedServices, setUngroupedServices] = useState<Service[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [editingCard, setEditingCard] = useState<ProjectCard | null>(null);
  const [editName, setEditName] = useState('');
  const [editFolderID, setEditFolderID] = useState(ROOT_FOLDER_VALUE);
  const [savingEdit, setSavingEdit] = useState(false);

  const [deletingProjectCard, setDeletingProjectCard] = useState<ProjectCard | null>(null);
  const [deletingProject, setDeletingProject] = useState(false);

  const [createFolderOpen, setCreateFolderOpen] = useState(false);
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [renamingFolder, setRenamingFolder] = useState<ProjectFolder | null>(null);
  const [renameFolderName, setRenameFolderName] = useState('');
  const [savingFolderRename, setSavingFolderRename] = useState(false);
  const [deletingFolder, setDeletingFolder] = useState<ProjectFolder | null>(null);
  const [deletingFolderPending, setDeletingFolderPending] = useState(false);

  const [serviceTab, setServiceTab] = useState<ServiceTab>('active');
  const [serviceSearch, setServiceSearch] = useState('');
  const [deletingService, setDeletingService] = useState<Service | null>(null);
  const [deletingServicePending, setDeletingServicePending] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setLoadError(null);

    try {
      const [projectList, serviceList, blueprintList, folderList] = await Promise.all([
        projectsApi.list().catch(() => [] as Project[]),
        servicesApi.list().catch(() => [] as Service[]),
        blueprintsApi.list().catch(() => [] as Blueprint[]),
        projectFoldersApi.list().catch(() => [] as ProjectFolder[]),
      ]);

      const blueprintDetails = await Promise.all(
        blueprintList.map(async (bp) => {
          const detailed = await blueprintsApi.get(bp.id).catch(() => null);
          if (!detailed) {
            return { ...bp, resources: [] };
          }
          return { ...bp, resources: detailed.resources || [] };
        }),
      );

      const data = buildProjectData(projectList, serviceList, blueprintDetails);
      setCards(data.cards);
      setFolders(folderList);
      setUngroupedServices(data.ungroupedServices);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to load projects');
      setCards([]);
      setFolders([]);
      setUngroupedServices([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const openCard = (card: ProjectCard) => {
    if (card.kind === 'project' && card.projectId) {
      navigate(`/projects/${card.projectId}`);
      return;
    }
    if (card.kind === 'blueprint' && card.blueprintId) {
      navigate(`/blueprints/${card.blueprintId}`);
    }
  };

  const beginEdit = (card: ProjectCard) => {
    setEditingCard(card);
    setEditName(card.name);
    setEditFolderID(card.folderId || ROOT_FOLDER_VALUE);
  };

  const saveEdit = async () => {
    if (!editingCard?.projectId) return;

    const nextName = editName.trim();
    if (!nextName) return;
    const currentFolderID = editingCard.folderId || ROOT_FOLDER_VALUE;
    const nextFolderID = editFolderID === ROOT_FOLDER_VALUE ? null : editFolderID;
    const folderChanged = currentFolderID !== editFolderID;
    const nameChanged = nextName !== editingCard.name;

    if (!nameChanged && !folderChanged) {
      setEditingCard(null);
      setEditName('');
      setEditFolderID(ROOT_FOLDER_VALUE);
      return;
    }

    setSavingEdit(true);
    try {
      await projectsApi.update(editingCard.projectId, {
        ...(nameChanged ? { name: nextName } : {}),
        ...(folderChanged ? { folder_id: nextFolderID } : {}),
      });
      toast.success('Project updated');
      setEditingCard(null);
      setEditName('');
      setEditFolderID(ROOT_FOLDER_VALUE);
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to update project');
    } finally {
      setSavingEdit(false);
    }
  };

  const confirmDeleteProject = async () => {
    if (!deletingProjectCard?.projectId) return;

    setDeletingProject(true);
    try {
      await projectsApi.delete(deletingProjectCard.projectId);
      toast.success('Project deleted');
      setDeletingProjectCard(null);
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to delete project');
    } finally {
      setDeletingProject(false);
    }
  };

  const createFolder = async () => {
    const name = newFolderName.trim();
    if (!name) return;

    setCreatingFolder(true);
    try {
      await projectFoldersApi.create({ name });
      toast.success('Folder created');
      setNewFolderName('');
      setCreateFolderOpen(false);
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to create folder');
    } finally {
      setCreatingFolder(false);
    }
  };

  const beginRenameFolder = (folder: ProjectFolder) => {
    setRenamingFolder(folder);
    setRenameFolderName(folder.name);
  };

  const saveFolderRename = async () => {
    if (!renamingFolder) return;

    const nextName = renameFolderName.trim();
    if (!nextName) return;
    if (nextName === renamingFolder.name) {
      setRenamingFolder(null);
      setRenameFolderName('');
      return;
    }

    setSavingFolderRename(true);
    try {
      await projectFoldersApi.update(renamingFolder.id, { name: nextName });
      toast.success('Folder renamed');
      setRenamingFolder(null);
      setRenameFolderName('');
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to rename folder');
    } finally {
      setSavingFolderRename(false);
    }
  };

  const confirmDeleteFolder = async () => {
    if (!deletingFolder) return;

    setDeletingFolderPending(true);
    try {
      await projectFoldersApi.delete(deletingFolder.id);
      toast.success('Folder deleted');
      setDeletingFolder(null);
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to delete folder');
    } finally {
      setDeletingFolderPending(false);
    }
  };

  const confirmDeleteService = async () => {
    if (!deletingService) return;

    setDeletingServicePending(true);
    try {
      await servicesApi.delete(deletingService.id);
      toast.success('Service deleted');
      setDeletingService(null);
      await loadData();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to delete service');
    } finally {
      setDeletingServicePending(false);
    }
  };

  const groupedCards = useMemo(() => {
    const rootCards: ProjectCard[] = [];
    const cardsByFolder = new Map<string, ProjectCard[]>();

    for (const folder of folders) {
      cardsByFolder.set(folder.id, []);
    }

    for (const card of cards) {
      if (card.kind !== 'project') {
        rootCards.push(card);
        continue;
      }
      if (card.folderId && cardsByFolder.has(card.folderId)) {
        cardsByFolder.get(card.folderId)?.push(card);
      } else {
        rootCards.push(card);
      }
    }

    return {
      rootCards,
      folderSections: folders.map((folder) => ({
        folder,
        cards: cardsByFolder.get(folder.id) || [],
      })),
    };
  }, [cards, folders]);

  const folderOptions = useMemo(
    () => [
      { value: ROOT_FOLDER_VALUE, label: 'No folder' },
      ...folders.map((folder) => ({ value: folder.id, label: folder.name })),
    ],
    [folders],
  );

  const renderProjectCard = (card: ProjectCard) => {
    const health = summarizeHealth(card.services);
    const clickable = card.kind === 'project' || card.kind === 'blueprint';
    const canManage = card.kind === 'project' && !!card.projectId && !!card.editable;

    return (
      <Card
        key={card.key}
        hover={clickable}
        onClick={clickable ? () => openCard(card) : undefined}
        className="group min-h-[160px] rounded-none bg-transparent border-border-default/80"
      >
        <div className="h-full flex flex-col justify-between">
          <div className="flex items-start justify-between gap-2">
            <div className="text-content-primary leading-none">
              <FolderKanban className="w-5 h-5" />
            </div>

            {canManage && (
              <div onClick={(e) => e.stopPropagation()}>
                <Dropdown
                  align="right"
                  trigger={
                    <button
                      type="button"
                      className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary"
                      aria-label={`Project actions for ${card.name}`}
                    >
                      <MoreVertical className="w-4 h-4" />
                    </button>
                  }
                  items={[
                    {
                      label: 'Edit Project',
                      icon: <Pencil className="w-3.5 h-3.5" />,
                      onClick: () => beginEdit(card),
                    },
                    {
                      label: 'Delete Project',
                      icon: <Trash2 className="w-3.5 h-3.5" />,
                      onClick: () => setDeletingProjectCard(card),
                      danger: true,
                    },
                  ]}
                />
              </div>
            )}
          </div>

          <div className="text-lg font-semibold text-content-primary leading-tight truncate mt-2">{card.name}</div>

          <span
            className="inline-flex items-center gap-2 mt-6"
            title={health.label}
            aria-label={health.label}
          >
            <span
              className={cn(
                'h-2.5 w-2.5 rounded-full border border-white/40 shadow-[0_0_0_4px_rgba(255,255,255,0.18)]',
                health.tone === 'healthy' ? 'bg-emerald-500' : 'bg-rose-500',
              )}
            />
          </span>
        </div>
      </Card>
    );
  };

  const activeUngrouped = useMemo(
    () => ungroupedServices.filter((svc) => !isSuspended(svc) && svc.status !== 'deactivated'),
    [ungroupedServices],
  );
  const suspendedUngrouped = useMemo(
    () => ungroupedServices.filter((svc) => isSuspended(svc)),
    [ungroupedServices],
  );

  const tabbedServices = useMemo(() => {
    if (serviceTab === 'active') return activeUngrouped;
    if (serviceTab === 'suspended') return suspendedUngrouped;
    return ungroupedServices;
  }, [serviceTab, activeUngrouped, suspendedUngrouped, ungroupedServices]);

  const visibleUngrouped = useMemo(() => {
    const q = serviceSearch.trim().toLowerCase();
    if (!q) return tabbedServices;

    return tabbedServices.filter((svc) =>
      svc.name.toLowerCase().includes(q)
      || serviceTypeLabel(svc.type).toLowerCase().includes(q)
      || svc.runtime.toLowerCase().includes(q)
      || effectiveStatus(svc).toLowerCase().includes(q),
    );
  }, [serviceSearch, tabbedServices]);

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-4xl leading-none font-semibold text-content-primary">Overview</h1>

        <button
          type="button"
          onClick={() => navigate('/new/blueprint')}
          className="inline-flex items-center gap-2 px-4 py-2 rounded-sm bg-white text-black text-sm font-medium hover:bg-zinc-200 transition-colors"
        >
          <Plus className="w-4 h-4" />
          New
          <ChevronDown className="w-4 h-4" />
        </button>
      </div>

      {loading ? (
        <Card padding="lg">
          <div className="text-sm text-content-secondary">Loading project summary...</div>
        </Card>
      ) : loadError ? (
        <Card padding="lg">
          <div className="text-sm text-status-error mb-3">{loadError}</div>
          <Button variant="secondary" onClick={loadData}>Retry</Button>
        </Card>
      ) : (
        <>
          <div className="mb-12">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-2xl font-semibold text-content-primary">Projects</h2>
              <Button variant="secondary" size="sm" onClick={() => setCreateFolderOpen(true)}>
                <Plus className="w-3.5 h-3.5" />
                New Folder
              </Button>
            </div>

            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4 mb-8">
              {groupedCards.rootCards.map((card) => renderProjectCard(card))}

              <button
                type="button"
                onClick={() => navigate('/new/blueprint')}
                className="min-h-[160px] rounded-none border border-dashed border-border-default/70 hover:border-border-hover text-content-secondary hover:text-content-primary transition-colors flex items-center justify-center text-lg"
              >
                + Create new project
              </button>
            </div>

            {groupedCards.folderSections.map((section) => (
              <div key={section.folder.id} className="mb-8">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-2">
                    <FolderKanban className="w-4 h-4 text-content-tertiary" />
                    <h3 className="text-base font-semibold text-content-primary">{section.folder.name}</h3>
                    <span className="text-xs text-content-tertiary">{section.cards.length} projects</span>
                  </div>

                  <Dropdown
                    align="right"
                    trigger={
                      <button
                        type="button"
                        className="p-1 rounded text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary"
                        aria-label={`Folder actions for ${section.folder.name}`}
                      >
                        <MoreVertical className="w-4 h-4" />
                      </button>
                    }
                    items={[
                      {
                        label: 'Rename Folder',
                        icon: <Pencil className="w-3.5 h-3.5" />,
                        onClick: () => beginRenameFolder(section.folder),
                      },
                      {
                        label: 'Delete Folder',
                        icon: <Trash2 className="w-3.5 h-3.5" />,
                        onClick: () => setDeletingFolder(section.folder),
                        danger: true,
                      },
                    ]}
                  />
                </div>

                <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
                  {section.cards.length > 0 ? (
                    section.cards.map((card) => renderProjectCard(card))
                  ) : (
                    <div className="min-h-[120px] border border-dashed border-border-default/60 rounded-none flex items-center px-5 text-sm text-content-tertiary">
                      No projects in this folder yet
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>

          <div>
            <h2 className="text-2xl font-semibold text-content-primary mb-5">Ungrouped Services</h2>

            <div className="inline-flex items-center border border-border-default mb-5">
              {[
                { id: 'active' as ServiceTab, label: `Active (${activeUngrouped.length})` },
                { id: 'suspended' as ServiceTab, label: `Suspended (${suspendedUngrouped.length})` },
                { id: 'all' as ServiceTab, label: `All (${ungroupedServices.length})` },
              ].map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setServiceTab(tab.id)}
                  className={cn(
                    'px-3 py-1.5 text-sm border-r border-border-default last:border-r-0 transition-colors',
                    serviceTab === tab.id
                      ? 'bg-[#3A1A73] text-white'
                      : 'text-content-secondary hover:text-content-primary hover:bg-surface-tertiary',
                  )}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            <div className="relative mb-5">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
              <input
                type="text"
                value={serviceSearch}
                onChange={(e) => setServiceSearch(e.target.value)}
                placeholder="Search services"
                className="w-full bg-transparent border border-border-default rounded-none pl-10 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-border-hover"
              />
            </div>

            <div className="border border-border-default rounded-none overflow-x-auto overflow-y-visible">
              <div className="grid grid-cols-[2.3fr_0.8fr_0.9fr_0.8fr_0.7fr_50px] px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-content-tertiary border-b border-border-default">
                <div>SERVICE NAME</div>
                <div>STATUS</div>
                <div>RUNTIME</div>
                <div>REGION</div>
                <div>UPDATED</div>
                <div />
              </div>

              {visibleUngrouped.length === 0 ? (
                <div className="px-4 py-6 text-sm text-content-secondary">No ungrouped services.</div>
              ) : (
                visibleUngrouped.map((svc) => (
                  <div
                    key={svc.id}
                    className="grid grid-cols-[2.3fr_0.8fr_0.9fr_0.8fr_0.7fr_50px] items-center px-4 py-3 border-b border-border-subtle last:border-b-0 hover:bg-surface-tertiary/40"
                  >
                    <button
                      type="button"
                      onClick={() => navigate(`/services/${svc.id}`)}
                      className="inline-flex items-center gap-2 min-w-0 text-left"
                    >
                      <ServiceIcon type={svc.type} />
                      <span className="text-sm text-content-primary truncate underline decoration-content-tertiary/60 underline-offset-4">
                        {svc.name}
                      </span>
                    </button>

                    <div>
                      <StatusBadge status={effectiveStatus(svc)} size="sm" />
                    </div>

                    <div>
                      <span className="inline-flex items-center px-2 py-1 text-[11px] rounded-md border border-brand/20 bg-brand/10 text-brand font-semibold capitalize">
                        {svc.runtime || serviceTypeLabel(svc.type)}
                      </span>
                    </div>

                    <div className="text-sm text-content-secondary">N/A</div>
                    <div className="text-sm text-content-secondary">{timeAgo(svc.updated_at || svc.created_at)}</div>

                    <div className="justify-self-end">
                      <Dropdown
                        align="right"
                        side="top"
                        trigger={
                          <button
                            type="button"
                            className="p-1 rounded text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary"
                            aria-label={`Actions for ${svc.name}`}
                          >
                            <MoreVertical className="w-4 h-4" />
                          </button>
                        }
                        items={[
                          {
                            label: 'Open Service',
                            onClick: () => navigate(`/services/${svc.id}`),
                          },
                          {
                            label: 'Delete Service',
                            icon: <Trash2 className="w-3.5 h-3.5" />,
                            onClick: () => setDeletingService(svc),
                            danger: true,
                          },
                        ]}
                      />
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </>
      )}

      <Modal
        open={!!editingCard}
        onClose={() => {
          if (savingEdit) return;
          setEditingCard(null);
          setEditName('');
          setEditFolderID(ROOT_FOLDER_VALUE);
        }}
        title="Edit Project"
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => {
                setEditingCard(null);
                setEditName('');
                setEditFolderID(ROOT_FOLDER_VALUE);
              }}
              disabled={savingEdit}
            >
              Cancel
            </Button>
            <Button onClick={saveEdit} loading={savingEdit} disabled={!editName.trim()}>
              Save
            </Button>
          </>
        }
      >
        <Input
          label="Project name"
          value={editName}
          onChange={(e) => setEditName(e.target.value)}
          placeholder="Project name"
        />
        <div className="mt-4 space-y-1.5">
          <label className="block text-sm font-medium text-content-primary">Folder</label>
          <select
            value={editFolderID}
            onChange={(e) => setEditFolderID(e.target.value)}
            className="w-full bg-surface-tertiary border border-border-default rounded-md px-3 py-2 text-sm text-content-primary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15"
          >
            {folderOptions.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
        </div>
      </Modal>

      <Modal
        open={createFolderOpen}
        onClose={() => {
          if (creatingFolder) return;
          setCreateFolderOpen(false);
          setNewFolderName('');
        }}
        title="Create Folder"
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => {
                setCreateFolderOpen(false);
                setNewFolderName('');
              }}
              disabled={creatingFolder}
            >
              Cancel
            </Button>
            <Button onClick={createFolder} loading={creatingFolder} disabled={!newFolderName.trim()}>
              Create
            </Button>
          </>
        }
      >
        <Input
          label="Folder name"
          value={newFolderName}
          onChange={(e) => setNewFolderName(e.target.value)}
          placeholder="Folder name"
        />
      </Modal>

      <Modal
        open={!!renamingFolder}
        onClose={() => {
          if (savingFolderRename) return;
          setRenamingFolder(null);
          setRenameFolderName('');
        }}
        title="Rename Folder"
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => {
                setRenamingFolder(null);
                setRenameFolderName('');
              }}
              disabled={savingFolderRename}
            >
              Cancel
            </Button>
            <Button onClick={saveFolderRename} loading={savingFolderRename} disabled={!renameFolderName.trim()}>
              Save
            </Button>
          </>
        }
      >
        <Input
          label="Folder name"
          value={renameFolderName}
          onChange={(e) => setRenameFolderName(e.target.value)}
          placeholder="Folder name"
        />
      </Modal>

      <Modal
        open={!!deletingFolder}
        onClose={() => {
          if (deletingFolderPending) return;
          setDeletingFolder(null);
        }}
        title="Delete Folder"
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeletingFolder(null)} disabled={deletingFolderPending}>
              Cancel
            </Button>
            <Button variant="danger" onClick={confirmDeleteFolder} loading={deletingFolderPending}>
              Delete
            </Button>
          </>
        }
      >
        <p className="text-sm text-content-secondary">
          Delete <span className="text-content-primary font-medium">{deletingFolder?.name}</span>? Projects in this folder will
          remain and move to root.
        </p>
      </Modal>

      <Modal
        open={!!deletingProjectCard}
        onClose={() => {
          if (deletingProject) return;
          setDeletingProjectCard(null);
        }}
        title="Delete Project"
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeletingProjectCard(null)} disabled={deletingProject}>
              Cancel
            </Button>
            <Button variant="danger" onClick={confirmDeleteProject} loading={deletingProject}>
              Delete
            </Button>
          </>
        }
      >
        <p className="text-sm text-content-secondary">
          Delete <span className="text-content-primary font-medium">{deletingProjectCard?.name}</span>? Services will remain,
          but they will be removed from this project.
        </p>
      </Modal>

      <Modal
        open={!!deletingService}
        onClose={() => {
          if (deletingServicePending) return;
          setDeletingService(null);
        }}
        title="Delete Service"
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeletingService(null)} disabled={deletingServicePending}>
              Cancel
            </Button>
            <Button variant="danger" onClick={confirmDeleteService} loading={deletingServicePending}>
              Delete
            </Button>
          </>
        }
      >
        <p className="text-sm text-content-secondary">
          Delete <span className="text-content-primary font-medium">{deletingService?.name}</span>? This action cannot be undone.
        </p>
      </Modal>
    </div>
  );
}
