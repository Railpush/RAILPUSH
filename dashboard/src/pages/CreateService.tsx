import { useState, useEffect, useMemo, type ComponentType } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Globe, FileText, Lock, Cog, Clock, Database, Key, ArrowLeft, Search, GitBranch, Link, ChevronRight, Terminal, Code, Box, Layers } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Select } from '../components/ui/Select';
import { services as servicesApi, databases as dbApi, keyvalue as kvApi, github as githubApi, ApiError } from '../lib/api';
import { PLAN_SPECS } from '../lib/plans';
import { buildDefaultServiceHostname } from '../lib/serviceUrl';
import { toast } from 'sonner';
import type { ServiceType, Runtime, GitHubRepo } from '../types';
import { cn } from '../lib/utils';
import { UpgradePromptModal } from '../components/billing/UpgradePromptModal';

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

type RuntimeIcon = ComponentType<{ className?: string }>;

const runtimes = [
  { value: 'node', label: 'Node.js', icon: Terminal as RuntimeIcon },
  { value: 'python', label: 'Python', icon: Code as RuntimeIcon },
  { value: 'go', label: 'Go', icon: Code as RuntimeIcon },
  { value: 'ruby', label: 'Ruby', icon: Code as RuntimeIcon },
  { value: 'rust', label: 'Rust', icon: Code as RuntimeIcon },
  { value: 'elixir', label: 'Elixir', icon: Code as RuntimeIcon },
  { value: 'docker', label: 'Docker', icon: Box as RuntimeIcon },
  { value: 'image', label: 'Pre-built Image', icon: Layers as RuntimeIcon },
];

export function CreateService() {
  const navigate = useNavigate();
  const { type: preselectedType } = useParams<{ type: string }>();
  const [step, setStep] = useState(preselectedType ? 2 : 1);
  const [selectedType, setSelectedType] = useState<string>(preselectedType || '');
  const [upgradePrompt, setUpgradePrompt] = useState<{ open: boolean; message: string }>({ open: false, message: '' });
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
  const [repoSearch, setRepoSearch] = useState('');
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepo | null>(null);

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
      setRepoSearch('');
      setForm((f) => ({ ...f, name: '', repo_url: '', branch: 'main' }));
    }
  }, [preselectedType, selectedType]);

  // Load repos when entering step 2 for a non-database type
  useEffect(() => {
    if (step === 2 && !isDatabase && repoMode === 'github') {
      loadRepos();
    }
  }, [step, repoMode, isDatabase]);

  async function loadRepos() {
    setReposLoading(true);
    try {
      const data = await githubApi.listRepos();
      setRepos(data);
      if (data.length === 0) {
        setRepoMode('manual');
      }
    } catch {
      setRepoMode('manual');
    } finally {
      setReposLoading(false);
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
      const msg = err instanceof Error ? err.message : 'Failed to create service';

      if (err instanceof ApiError) {
        if (err.status === 402) {
          setUpgradePrompt({ open: true, message: msg || 'Payment method required.' });
          return;
        }
        if (err.status === 400 && msg.toLowerCase().includes('free tier limit')) {
          setUpgradePrompt({ open: true, message: msg });
          return;
        }
        if (msg.toLowerCase().includes('billing error') || msg.toLowerCase().includes('stripe price')) {
          setUpgradePrompt({ open: true, message: msg });
          return;
        }
      }

      toast.error(msg);
    }
  };

  const handleSelectRuntime = (value: string) => {
    setForm({ ...form, runtime: value });
  }

  // Step 1: Choose type
  if (step === 1) {
    return (
      <div className="max-w-5xl mx-auto animate-enter pb-10">
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-content-primary mb-2">Create a New Resource</h1>
          <p className="text-content-secondary">Select the type of service or datastore you want to deploy.</p>
        </div>

        <div className="space-y-8">
          <div>
            <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-4 px-1">Services</h2>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {serviceTypes.map((st) => (
                <button
                  key={st.type}
                  onClick={() => { setSelectedType(st.type); setStep(2); }}
                  className="glass-panel p-6 text-left hover:border-brand/50 transition-all group relative overflow-hidden"
                >
                  <div className="absolute top-0 right-0 p-4 opacity-0 group-hover:opacity-100 transition-opacity">
                    <ChevronRight className="w-5 h-5 text-brand" />
                  </div>
                  <div className="w-12 h-12 rounded-xl mb-4 flex items-center justify-center transition-transform group-hover:scale-110 duration-300" style={{ backgroundColor: `${st.color}20`, color: st.color }}>
                    <st.icon size={24} />
                  </div>
                  <div className="font-bold text-lg text-content-primary mb-1">{st.label}</div>
                  <div className="text-sm text-content-secondary">{st.desc}</div>
                </button>
              ))}
            </div>
          </div>

          <div>
            <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-4 px-1">Datastores</h2>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {dbTypes.map((dt) => (
                <button
                  key={dt.type}
                  onClick={() => { setSelectedType(dt.type); setStep(2); }}
                  className="glass-panel p-6 text-left hover:border-brand/50 transition-all group relative overflow-hidden"
                >
                  <div className="absolute top-0 right-0 p-4 opacity-0 group-hover:opacity-100 transition-opacity">
                    <ChevronRight className="w-5 h-5 text-brand" />
                  </div>
                  <div className="w-12 h-12 rounded-xl mb-4 flex items-center justify-center transition-transform group-hover:scale-110 duration-300" style={{ backgroundColor: `${dt.color}20`, color: dt.color }}>
                    <dt.icon size={24} />
                  </div>
                  <div className="font-bold text-lg text-content-primary mb-1">{dt.label}</div>
                  <div className="text-sm text-content-secondary">{dt.desc}</div>
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Step 2: Configure
  return (
    <div className="max-w-4xl mx-auto animate-enter pb-12">
      <Button
        variant="ghost"
        onClick={() => setStep(1)}
        className="mb-6 pl-0 hover:pl-2 transition-all"
      >
        <ArrowLeft className="w-4 h-4 mr-2" />
        Back to Selection
      </Button>

      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold text-content-primary">
            {isDatabase
              ? `Create ${selectedType === 'postgres' ? 'PostgreSQL Database' : 'Key Value Store'}`
              : `Create ${serviceTypes.find((s) => s.type === selectedType)?.label || 'Service'}`}
          </h1>
          <p className="text-content-secondary mt-1">Configure your new resource.</p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[1fr_320px] gap-8">
        <div className="space-y-6">
          <Card className="glass-panel p-6 space-y-6">
            <div>
              <h3 className="text-lg font-medium text-white mb-4">General</h3>
              <Input
                label="Name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder={isDatabase ? 'my-database' : 'my-service'}
                hint={!isDatabase ? `Your service will be available at ${previewUrl}` : undefined}
              />
            </div>

            {!isDatabase && (
              <>
                <div className="pt-4 border-t border-border-default/50">
                  <h3 className="text-lg font-medium text-white mb-4">Environment</h3>

                  <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
                    {runtimes.map((rt) => {
                      const Icon = rt.icon;
                      return (
                        <button
                          key={rt.value}
                          onClick={() => handleSelectRuntime(rt.value)}
                          className={cn(
                            "flex flex-col items-center justify-center p-3 rounded-lg border transition-all",
                            form.runtime === rt.value
                              ? "bg-brand/10 border-brand text-white"
                              : "bg-surface-tertiary/30 border-border-default text-content-secondary hover:border-border-hover"
                          )}
                        >
                          <Icon className="w-6 h-6 mb-2" />
                          <span className="text-xs font-medium">{rt.label}</span>
                        </button>
                      );
                    })}
                  </div>

                  <div className="grid grid-cols-2 gap-4">
                    <Input
                      label="Port"
                      type="number"
                      value={form.port}
                      onChange={(e) => setForm({ ...form, port: e.target.value })}
                      placeholder="10000"
                    />
                  </div>

                  {isImageRuntime && (
                    <div className="mt-4">
                      <Input
                        label="Image URL"
                        value={form.image_url}
                        onChange={(e) => setForm({ ...form, image_url: e.target.value })}
                        placeholder="nginxdemos/hello:plain-text"
                        hint="Public image reference, or your private registry image (requires pull credentials)."
                      />
                    </div>
                  )}
                </div>

                {!isImageRuntime && (
                  <div className="pt-4 border-t border-border-default/50">
                    <h3 className="text-lg font-medium text-white mb-4">Source Code</h3>

                    <div className="space-y-4">
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

                      {/* GitHub Repo Picker (Simplified styling) */}
                      {repoMode === 'github' ? (
                        <div className="space-y-2">
                          {!selectedRepo ? (
                            <div className="relative">
                              <Search className="absolute left-3 top-2.5 w-4 h-4 text-content-tertiary" />
                              <input
                                type="text"
                                value={repoSearch}
                                onChange={(e) => setRepoSearch(e.target.value)}
                                placeholder="Search repositories..."
                                className="w-full bg-surface-tertiary border border-border-default rounded-md pl-9 pr-3 py-2 text-sm text-content-primary focus:outline-none focus:border-brand focus:ring-1 focus:ring-brand"
                                onClick={() => { if (repos.length === 0) loadRepos() }}
                              />
                              {reposLoading && <div className="text-xs text-content-tertiary mt-1 ml-1">Loading...</div>}
                              {filteredRepos.length > 0 && !selectedRepo && (
                                <div className="mt-2 max-h-48 overflow-y-auto border border-border-default rounded-md bg-surface-tertiary/50 backdrop-blur-sm">
                                  {filteredRepos.map(repo => (
                                    <button key={repo.id} onClick={() => handleSelectRepo(repo)} className="w-full text-left px-3 py-2 text-sm hover:bg-white/5 transition-colors truncate">
                                      {repo.full_name}
                                    </button>
                                  ))}
                                </div>
                              )}
                            </div>
                          ) : (
                            <div className="flex items-center justify-between p-3 bg-surface-tertiary/30 border border-border-default rounded-md">
                              <span className="text-sm font-medium">{selectedRepo.full_name}</span>
                              <button onClick={() => { setSelectedRepo(null); setForm((f) => ({ ...f, repo_url: '', branch: 'main' })); }} className="text-xs text-content-secondary hover:text-content-primary">Change</button>
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

                      <div className="grid grid-cols-2 gap-4">
                        <Input
                          label="Branch"
                          value={form.branch}
                          onChange={(e) => setForm({ ...form, branch: e.target.value })}
                          placeholder="main"
                        />
                        <div className="flex items-end mb-1">
                          <label className="flex items-center gap-2 cursor-pointer select-none">
                            <input
                              type="checkbox"
                              checked={form.auto_deploy}
                              onChange={(e) => setForm({ ...form, auto_deploy: e.target.checked })}
                              className="w-4 h-4 rounded border-border-default text-brand bg-surface-tertiary focus:ring-brand"
                            />
                            <span className="text-sm text-content-secondary">Auto-deploy</span>
                          </label>
                        </div>
                      </div>

                      <div className="space-y-4">
                        <Input
                          label="Build Command"
                          value={form.build_command}
                          onChange={(e) => setForm({ ...form, build_command: e.target.value })}
                          placeholder="npm install && npm run build"
                          icon={<Terminal className="w-4 h-4" />}
                        />
                        {selectedType !== 'static' && (
                          <Input
                            label="Start Command"
                            value={form.start_command}
                            onChange={(e) => setForm({ ...form, start_command: e.target.value })}
                            placeholder="npm start"
                            icon={<Terminal className="w-4 h-4" />}
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
                      </div>
                    </div>
                  </div>
                )}
              </>
            )}

            {selectedType === 'postgres' && (
              <div className="pt-4 border-t border-border-default/50">
                <Select
                  label="PostgreSQL Version"
                  options={[{ value: '16', label: 'PostgreSQL 16' }, { value: '15', label: 'PostgreSQL 15' }]}
                  value={form.pg_version}
                  onChange={(e) => setForm({ ...form, pg_version: e.target.value })}
                />
              </div>
            )}
          </Card>
        </div>

        {/* Right Column: Plan Selection & Summary */}
        <div className="space-y-6">
          <Card className="glass-panel p-5">
            <h3 className="text-sm font-semibold text-white mb-4">{isDatabase ? 'Database Plan' : 'Instance Plan'}</h3>
            <div className="space-y-3">
              {PLAN_SPECS.map((p) => (
                <label
                  key={p.id}
                  className={`block relative p-3 rounded-lg border cursor-pointer transition-all ${form.plan === p.id
                    ? 'border-brand bg-brand/5 ring-1 ring-brand/50'
                    : 'border-border-default hover:border-border-hover bg-surface-tertiary/20'
                    }`}
                >
                  <input
                    type="radio"
                    name="plan"
                    checked={form.plan === p.id}
                    onChange={() => setForm({ ...form, plan: p.id })}
                    className="sr-only"
                  />
                  <div className="flex justify-between items-start mb-1">
                    <span className="font-medium text-sm text-content-primary">{p.name}</span>
                    <span className="font-bold text-sm text-content-primary">{p.priceLabel}</span>
                  </div>
                  <div className="text-xs text-content-secondary flex gap-2">
                    <span>{p.cpu}</span>
                    <span>•</span>
                    <span>{p.mem}</span>
                  </div>
                </label>
              ))}
            </div>
          </Card>

          <div className="sticky top-6">
            <Button size="lg" className="w-full shadow-lg shadow-brand/20" onClick={handleCreate} disabled={!form.name.trim()}>
              {isDatabase
                ? `Create ${selectedType === 'postgres' ? 'Database' : 'Store'}`
                : 'Deploy Service'}
            </Button>
            <p className="text-xs text-center text-content-tertiary mt-3">
              By deploying, you agree to our Terms of Service.
            </p>
          </div>
        </div>
      </div>

      <UpgradePromptModal
        open={upgradePrompt.open}
        message={upgradePrompt.message}
        onClose={() => setUpgradePrompt({ open: false, message: '' })}
      />
    </div>
  );
}
