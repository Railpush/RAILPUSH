import { useState, useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Globe, FileText, Lock, Cog, Clock, Database, Key, ArrowLeft, Search, GitBranch, Lock as LockIcon, Link } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Select } from '../components/ui/Select';
import { services as servicesApi, databases as dbApi, keyvalue as kvApi, github as githubApi, ApiError } from '../lib/api';
import { PLAN_SPECS } from '../lib/plans';
import { buildDefaultServiceHostname } from '../lib/serviceUrl';
import { toast } from 'sonner';
import type { ServiceType, Runtime, GitHubRepo, GitHubBranch } from '../types';

const serviceTypes = [
  { type: 'web' as ServiceType, icon: Globe, label: 'Web Service', desc: 'HTTP service with public URL', color: '#4351E8' },
  { type: 'static' as ServiceType, icon: FileText, label: 'Static Site', desc: 'HTML/CSS/JS served by CDN', color: '#59FFA4' },
  { type: 'pserv' as ServiceType, icon: Lock, label: 'Private Service', desc: 'Internal service, no public URL', color: '#8A05FF' },
  { type: 'worker' as ServiceType, icon: Cog, label: 'Background Worker', desc: 'Continuously running process', color: '#FFBB33' },
  { type: 'cron' as ServiceType, icon: Clock, label: 'Cron Job', desc: 'Scheduled task runner', color: '#38BDF8' },
];

const dbTypes = [
  { type: 'postgres', icon: Database, label: 'PostgreSQL', desc: 'Managed PostgreSQL database', color: '#336791' },
  { type: 'keyvalue', icon: Key, label: 'Key Value', desc: 'Managed Redis-compatible store', color: '#DC382D' },
];

const runtimes = [
  { value: 'node', label: 'Node' },
  { value: 'python', label: 'Python' },
  { value: 'go', label: 'Go' },
  { value: 'ruby', label: 'Ruby' },
  { value: 'rust', label: 'Rust' },
  { value: 'elixir', label: 'Elixir' },
  { value: 'docker', label: 'Docker' },
  { value: 'image', label: 'Pre-built Image' },
];

export function CreateService() {
  const navigate = useNavigate();
  const { type: preselectedType } = useParams<{ type: string }>();
  const [step, setStep] = useState(preselectedType ? 2 : 1);
  const [selectedType, setSelectedType] = useState<string>(preselectedType || '');
  const [form, setForm] = useState({
    name: '',
    repo_url: '',
    image_url: '',
    branch: 'main',
    runtime: 'node',
    build_command: '',
    start_command: '',
    port: '10000',
    auto_deploy: true,
    plan: 'free',
    schedule: '',
    static_publish_path: './dist',
    pg_version: '16',
    maxmemory_policy: 'allkeys-lru',
  });

  // GitHub repo picker state
  const [repoMode, setRepoMode] = useState<'github' | 'manual'>('github');
  const [repos, setRepos] = useState<GitHubRepo[]>([]);
  const [reposLoading, setReposLoading] = useState(false);
  const [reposError, setReposError] = useState(false);
  const [repoSearch, setRepoSearch] = useState('');
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepo | null>(null);
  const [branches, setBranches] = useState<GitHubBranch[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(false);

  const isDatabase = selectedType === 'postgres' || selectedType === 'keyvalue';
  const isImageRuntime = !isDatabase && form.runtime === 'image';
  const previewHostname = buildDefaultServiceHostname(form.name || 'my-service');
  const previewUrl = previewHostname ? `https://${previewHostname}` : 'http://localhost:<assigned-port>';

  // Sync URL param to state when navigating between /new/:type routes
  useEffect(() => {
    if (preselectedType && preselectedType !== selectedType) {
      setSelectedType(preselectedType);
      setStep(2);
      setSelectedRepo(null);
      setBranches([]);
      setRepoSearch('');
      setForm((f) => ({ ...f, name: '', repo_url: '', branch: 'main' }));
    }
  }, [preselectedType]);

  // Load repos when entering step 2 for a non-database type
  useEffect(() => {
    if (step === 2 && !isDatabase && repoMode === 'github') {
      loadRepos();
    }
  }, [step, repoMode, isDatabase]);

  async function loadRepos() {
    setReposLoading(true);
    setReposError(false);
    try {
      const data = await githubApi.listRepos();
      setRepos(data);
      if (data.length === 0) {
        setRepoMode('manual');
      }
    } catch {
      setReposError(true);
      setRepoMode('manual');
    } finally {
      setReposLoading(false);
    }
  }

  async function loadBranches(owner: string, repo: string) {
    setBranchesLoading(true);
    try {
      const data = await githubApi.listBranches(owner, repo);
      setBranches(data);
    } catch {
      setBranches([]);
    } finally {
      setBranchesLoading(false);
    }
  }

  function handleSelectRepo(repo: GitHubRepo) {
    setSelectedRepo(repo);
    setForm((f) => ({
      ...f,
      repo_url: repo.clone_url,
      branch: repo.default_branch,
      name: f.name || repo.name,
    }));
    // Load branches
    const [owner, repoName] = repo.full_name.split('/');
    loadBranches(owner, repoName);
  }

  const filteredRepos = useMemo(() => {
    if (!repoSearch) return repos;
    const q = repoSearch.toLowerCase();
    return repos.filter((r) => r.full_name.toLowerCase().includes(q));
  }, [repos, repoSearch]);

  const handleCreate = async () => {
    try {
      if (selectedType === 'postgres') {
        await dbApi.create({ name: form.name, plan: form.plan, pg_version: parseInt(form.pg_version) });
        toast.success('Database created');
        navigate('/');
        return;
      }
      if (selectedType === 'keyvalue') {
        await kvApi.create({ name: form.name, plan: form.plan, maxmemory_policy: form.maxmemory_policy });
        toast.success('Key Value store created');
        navigate('/');
        return;
      }

      if (isImageRuntime && !form.image_url.trim()) {
        toast.error('Image URL is required for pre-built image runtime');
        return;
      }

      await servicesApi.create({
        name: form.name,
        type: selectedType as ServiceType,
        runtime: form.runtime as Runtime,
        repo_url: isImageRuntime ? '' : form.repo_url,
        image_url: isImageRuntime ? form.image_url : undefined,
        branch: form.branch,
        build_command: form.build_command,
        start_command: form.start_command,
        port: parseInt(form.port, 10) || 10000,
        auto_deploy: form.auto_deploy,
        plan: form.plan,
        schedule: selectedType === 'cron' ? form.schedule : undefined,
        static_publish_path: selectedType === 'static' ? form.static_publish_path : undefined,
      });
      toast.success('Service created');
      navigate('/');
    } catch (err) {
      if (err instanceof ApiError && err.status === 402) {
        toast.error('Payment method required. Redirecting to billing...');
        setTimeout(() => navigate('/billing'), 1500);
        return;
      }
      toast.error(err instanceof Error ? err.message : 'Failed to create service');
    }
  };

  // Step 1: Choose type
  if (step === 1) {
    return (
      <div>
        <h1 className="text-2xl font-semibold text-content-primary mb-6">Create a New Resource</h1>

        <div className="mb-6">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Services</h2>
          <div className="grid grid-cols-3 gap-3">
            {serviceTypes.map((st) => (
              <Card
                key={st.type}
                hover
                onClick={() => { setSelectedType(st.type); setStep(2); }}
                className="text-center py-6"
              >
                <div className="w-10 h-10 rounded-lg mx-auto mb-3 flex items-center justify-center" style={{ backgroundColor: `${st.color}15` }}>
                  <st.icon size={22} style={{ color: st.color }} />
                </div>
                <div className="text-sm font-semibold text-content-primary">{st.label}</div>
                <div className="text-xs text-content-secondary mt-1">{st.desc}</div>
              </Card>
            ))}
          </div>
        </div>

        <div>
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Datastores</h2>
          <div className="grid grid-cols-3 gap-3">
            {dbTypes.map((dt) => (
              <Card
                key={dt.type}
                hover
                onClick={() => { setSelectedType(dt.type); setStep(2); }}
                className="text-center py-6"
              >
                <div className="w-10 h-10 rounded-lg mx-auto mb-3 flex items-center justify-center" style={{ backgroundColor: `${dt.color}15` }}>
                  <dt.icon size={22} style={{ color: dt.color }} />
                </div>
                <div className="text-sm font-semibold text-content-primary">{dt.label}</div>
                <div className="text-xs text-content-secondary mt-1">{dt.desc}</div>
              </Card>
            ))}
          </div>
        </div>
      </div>
    );
  }

  // Step 2: Configure
  return (
    <div>
      <button
        onClick={() => setStep(1)}
        className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        Back
      </button>

      <h1 className="text-2xl font-semibold text-content-primary mb-6">
        {isDatabase
          ? `Create ${selectedType === 'postgres' ? 'PostgreSQL Database' : 'Key Value Store'}`
          : `Create ${serviceTypes.find((s) => s.type === selectedType)?.label || 'Service'}`}
      </h1>

      <Card padding="lg">
        <div className="space-y-5 max-w-xl">
          <Input
            label="Name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder={isDatabase ? 'my-database' : 'my-service'}
            hint={!isDatabase ? `Your service will be available at ${previewUrl}` : undefined}
          />

          {!isDatabase && (
            <>
              <div className="grid grid-cols-2 gap-4">
                <Select label="Runtime" options={runtimes} value={form.runtime} onChange={(e) => setForm({ ...form, runtime: e.target.value })} />
                <Input
                  label="Port"
                  type="number"
                  value={form.port}
                  onChange={(e) => setForm({ ...form, port: e.target.value })}
                  placeholder="10000"
                  hint="Container port your app listens on"
                />
              </div>

              {isImageRuntime && (
                <Input
                  label="Image URL"
                  value={form.image_url}
                  onChange={(e) => setForm({ ...form, image_url: e.target.value })}
                  placeholder="nginxdemos/hello:plain-text"
                  hint="Public image reference, or your private registry image (requires pull credentials)."
                />
              )}

              {/* Repository section */}
              {!isImageRuntime && (
                <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="block text-sm font-medium text-content-primary">Repository</label>
                  <button
                    type="button"
                    onClick={() => {
                      if (repoMode === 'github') {
                        setRepoMode('manual');
                        setSelectedRepo(null);
                      } else {
                        setRepoMode('github');
                        if (repos.length === 0 && !reposLoading) loadRepos();
                      }
                    }}
                    className="inline-flex items-center gap-1 text-xs text-brand hover:text-brand/80 transition-colors"
                  >
                    {repoMode === 'github' ? (
                      <><Link className="w-3 h-3" /> Enter URL manually</>
                    ) : (
                      <><GitBranch className="w-3 h-3" /> Pick from GitHub</>
                    )}
                  </button>
                </div>

                {repoMode === 'github' ? (
                  <div>
                    {reposLoading ? (
                      <div className="flex items-center justify-center py-8 text-sm text-content-tertiary">
                        Loading repositories...
                      </div>
                    ) : reposError ? (
                      <div className="text-center py-6 space-y-2">
                        <p className="text-sm text-content-secondary">Could not load GitHub repos.</p>
                        <a
                          href="/api/v1/auth/github"
                          className="inline-flex items-center gap-1.5 text-sm text-brand hover:text-brand/80 transition-colors"
                        >
                          Connect GitHub
                        </a>
                      </div>
                    ) : selectedRepo ? (
                      <div className="flex items-center gap-3 px-3 py-2.5 border border-border-default rounded-md bg-surface-tertiary">
                        <div className="flex-1 min-w-0">
                          <div className="text-sm font-medium text-content-primary truncate">{selectedRepo.full_name}</div>
                          <div className="text-xs text-content-tertiary">{selectedRepo.default_branch}{selectedRepo.private ? ' · private' : ''}</div>
                        </div>
                        <button
                          type="button"
                          onClick={() => { setSelectedRepo(null); setBranches([]); setForm((f) => ({ ...f, repo_url: '', branch: 'main' })); }}
                          className="text-xs text-brand hover:text-brand/80 transition-colors flex-shrink-0"
                        >
                          Change
                        </button>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        <div className="relative">
                          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
                          <input
                            type="text"
                            value={repoSearch}
                            onChange={(e) => setRepoSearch(e.target.value)}
                            placeholder="Search repositories..."
                            className="w-full bg-surface-tertiary border border-border-default rounded-md pl-9 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all duration-150"
                          />
                        </div>
                        <div className="max-h-56 overflow-y-auto border border-border-default rounded-md divide-y divide-border-default">
                          {filteredRepos.length === 0 ? (
                            <div className="py-6 text-center text-sm text-content-tertiary">No repositories found</div>
                          ) : (
                            filteredRepos.map((repo) => (
                              <button
                                key={repo.id}
                                type="button"
                                onClick={() => handleSelectRepo(repo)}
                                className="w-full text-left px-3 py-2.5 flex items-center gap-2.5 hover:bg-surface-secondary transition-colors"
                              >
                                <div className="flex-1 min-w-0">
                                  <div className="text-sm font-medium text-content-primary truncate">{repo.full_name}</div>
                                  <div className="text-xs text-content-tertiary">
                                    {repo.default_branch} &middot; updated {new Date(repo.updated_at).toLocaleDateString()}
                                  </div>
                                </div>
                                {repo.private && (
                                  <LockIcon className="w-3.5 h-3.5 text-content-tertiary flex-shrink-0" />
                                )}
                              </button>
                            ))
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                ) : (
                  <Input
                    value={form.repo_url}
                    onChange={(e) => setForm({ ...form, repo_url: e.target.value })}
                    placeholder="https://github.com/user/repo"
                  />
                )}
              </div>
              )}

              {/* Branch */}
              {!isImageRuntime && (
                repoMode === 'github' && branches.length > 0 ? (
                  <Select
                    label="Branch"
                    options={branches.map((b) => ({ value: b.name, label: b.name }))}
                    value={form.branch}
                    onChange={(e) => setForm({ ...form, branch: e.target.value })}
                    disabled={branchesLoading}
                  />
                ) : (
                  <Input
                    label="Branch"
                    value={form.branch}
                    onChange={(e) => setForm({ ...form, branch: e.target.value })}
                  />
                )
              )}

              {!isImageRuntime && (
                <label className="flex items-start gap-3 p-3 rounded-md border border-border-default bg-surface-tertiary">
                  <input
                    type="checkbox"
                    checked={form.auto_deploy}
                    onChange={(e) => setForm({ ...form, auto_deploy: e.target.checked })}
                    className="mt-0.5 accent-brand"
                  />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium text-content-primary">Auto deploy on push</span>
                    <span className="block text-xs text-content-tertiary mt-0.5">
                      When enabled, pushes to <span className="font-mono">{form.branch || 'main'}</span> trigger a deploy.
                    </span>
                  </span>
                </label>
              )}

              {!isImageRuntime && (
                <Input
                  label="Build Command"
                  value={form.build_command}
                  onChange={(e) => setForm({ ...form, build_command: e.target.value })}
                  placeholder="npm install && npm run build"
                />
              )}

              {selectedType !== 'static' && !isImageRuntime && (
                <Input
                  label="Start Command"
                  value={form.start_command}
                  onChange={(e) => setForm({ ...form, start_command: e.target.value })}
                  placeholder="npm start"
                />
              )}

              {selectedType === 'static' && (
                <Input
                  label="Publish Directory"
                  value={form.static_publish_path}
                  onChange={(e) => setForm({ ...form, static_publish_path: e.target.value })}
                  placeholder="./dist"
                />
              )}

              {selectedType === 'cron' && (
                <Input
                  label="Schedule (cron syntax)"
                  value={form.schedule}
                  onChange={(e) => setForm({ ...form, schedule: e.target.value })}
                  placeholder="0 3 * * *"
                  hint="Standard cron syntax. Example: '0 3 * * *' = 3 AM daily"
                />
              )}
            </>
          )}

          {selectedType === 'postgres' && (
            <Select
              label="PostgreSQL Version"
              options={[{ value: '16', label: 'PostgreSQL 16' }, { value: '15', label: 'PostgreSQL 15' }]}
              value={form.pg_version}
              onChange={(e) => setForm({ ...form, pg_version: e.target.value })}
            />
          )}

          {/* Instance Type */}
          <div>
            <label className="block text-sm font-medium text-content-primary mb-2">
              {isDatabase ? 'Plan' : 'Instance Type'}
            </label>
            <div className="space-y-2">
              {PLAN_SPECS.map((p) => (
                <label
                  key={p.id}
                  className={`flex items-center gap-3 p-3 rounded-lg border cursor-pointer transition-all ${
                    form.plan === p.id
                      ? 'border-brand bg-brand/5'
                      : 'border-border-default hover:border-border-hover'
                  }`}
                >
                  <input
                    type="radio"
                    name="plan"
                    checked={form.plan === p.id}
                    onChange={() => setForm({ ...form, plan: p.id })}
                    className="accent-brand"
                  />
                  <div className="flex-1">
                    <div className="text-sm font-medium text-content-primary">{p.name}</div>
                    <div className="text-xs text-content-secondary">{p.cpu}, {p.mem}</div>
                  </div>
                  <span className="text-sm font-medium text-content-primary">{p.priceLabel}</span>
                </label>
              ))}
            </div>
          </div>

          <Button size="lg" className="w-full" onClick={handleCreate} disabled={!form.name.trim()}>
            {isDatabase
              ? `Create ${selectedType === 'postgres' ? 'Database' : 'Key Value Store'}`
              : `Create ${serviceTypes.find((s) => s.type === selectedType)?.label || 'Service'}`}
          </Button>
        </div>
      </Card>
    </div>
  );
}
