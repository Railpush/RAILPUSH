import { useState, useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowLeft, Search, GitBranch, Lock as LockIcon, Link } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Select } from '../components/ui/Select';
import { blueprints as bpApi, github as githubApi } from '../lib/api';
import { toast } from 'sonner';
import type { GitHubRepo, GitHubBranch } from '../types';

export function CreateBlueprint() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({
    name: '',
    repo_url: '',
    branch: 'main',
    file_path: 'render.yaml',
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

  useEffect(() => {
    if (repoMode === 'github') loadRepos();
  }, [repoMode]);

  async function loadRepos() {
    setReposLoading(true);
    setReposError(false);
    try {
      const data = await githubApi.listRepos();
      setRepos(data);
      if (data.length === 0) setRepoMode('manual');
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
    const [owner, repoName] = repo.full_name.split('/');
    loadBranches(owner, repoName);
  }

  const filteredRepos = useMemo(() => {
    if (!repoSearch) return repos;
    const q = repoSearch.toLowerCase();
    return repos.filter((r) => r.full_name.toLowerCase().includes(q));
  }, [repos, repoSearch]);

  const handleCreate = async () => {
    if (!form.name.trim() || !form.repo_url.trim()) return;
    setLoading(true);
    try {
      const bp = await bpApi.create(form);
      toast.success('Blueprint created — syncing resources...');
      navigate(`/blueprints/${bp.id}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to create blueprint');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <button
        onClick={() => navigate('/blueprints')}
        className="inline-flex items-center gap-1.5 text-sm text-content-secondary hover:text-content-primary transition-colors mb-4"
      >
        <ArrowLeft className="w-4 h-4" />
        Back to Blueprints
      </button>

      <h1 className="text-2xl font-semibold text-content-primary mb-6">New Blueprint</h1>

      <Card padding="lg">
        <div className="space-y-5 max-w-xl">
          <Input
            label="Name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="my-infrastructure"
          />

          {/* Repository section */}
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
                hint="The repository containing your render.yaml file"
              />
            )}
          </div>

          {/* Branch + YAML path */}
          <div className="grid grid-cols-2 gap-4">
            {repoMode === 'github' && branches.length > 0 ? (
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
                placeholder="main"
              />
            )}
            <Input
              label="YAML File Path"
              value={form.file_path}
              onChange={(e) => setForm({ ...form, file_path: e.target.value })}
              placeholder="render.yaml"
            />
          </div>

          <Button
            size="lg"
            className="w-full"
            onClick={handleCreate}
            loading={loading}
            disabled={!form.name.trim() || !form.repo_url.trim()}
          >
            Create Blueprint
          </Button>
        </div>
      </Card>
    </div>
  );
}
