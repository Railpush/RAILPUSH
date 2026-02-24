import { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { services as servicesApi, envVars as envVarsApi } from '../lib/api';
import { buildDefaultServiceHostname, hostnameFromUrl } from '../lib/serviceUrl';
import type { Service } from '../types';
import { toast } from 'sonner';

type TermLine = { text: string; color?: string; delay?: number };
type DeployAutomationMode = 'push' | 'workflow_success' | 'off';

function parseTruthyEnv(value?: string): boolean {
  const normalized = (value || '').trim().toLowerCase();
  return ['1', 'true', 'yes', 'on', 'y'].includes(normalized);
}

function parseWorkflowNames(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of (raw || '').split(',')) {
    const trimmed = item.trim();
    if (!trimmed) continue;
    const key = trimmed.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
  }
  return out;
}

function TerminalDeleteModal({ open, onClose, service, onConfirm }: {
  open: boolean;
  onClose: () => void;
  service: Service | null;
  onConfirm: () => Promise<void>;
}) {
  const [input, setInput] = useState('');
  const [phase, setPhase] = useState<'prompt' | 'deleting' | 'done' | 'error'>('prompt');
  const [lines, setLines] = useState<TermLine[]>([]);
  const [visibleLines, setVisibleLines] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const termRef = useRef<HTMLDivElement>(null);

  const resetState = useCallback(() => {
    setInput('');
    setPhase('prompt');
    setLines([]);
    setVisibleLines(0);
  }, []);

  useEffect(() => {
    if (open) {
      resetState();
      setTimeout(() => inputRef.current?.focus(), 100);
    }
  }, [open, resetState]);

  useEffect(() => {
    if (termRef.current) {
      termRef.current.scrollTop = termRef.current.scrollHeight;
    }
  }, [visibleLines]);

  // Animate lines appearing one by one
  useEffect(() => {
    if (lines.length === 0) return;
    if (visibleLines >= lines.length) return;
    const next = lines[visibleLines];
    const timer = setTimeout(() => {
      setVisibleLines((v) => v + 1);
    }, next.delay ?? 300);
    return () => clearTimeout(timer);
  }, [lines, visibleLines]);

  // When all lines are visible and phase is deleting, execute
  useEffect(() => {
    if (phase === 'deleting' && visibleLines === lines.length && lines.length > 0) {
      // The actual API call happens after the animation finishes showing "Deleting from database..."
      const timer = setTimeout(async () => {
        try {
          await onConfirm();
          setLines((prev) => [
            ...prev,
            { text: '', delay: 100 },
            { text: '  All resources cleaned up.', color: 'text-green-400', delay: 200 },
            { text: '  Service permanently destroyed.', color: 'text-green-400', delay: 300 },
            { text: '', delay: 100 },
            { text: '  Redirecting...', color: 'text-content-tertiary', delay: 800 },
          ]);
          setPhase('done');
        } catch {
          setLines((prev) => [
            ...prev,
            { text: '', delay: 100 },
            { text: '  ERROR: Failed to delete service', color: 'text-status-error', delay: 200 },
            { text: '  Please try again or contact support.', color: 'text-content-tertiary', delay: 300 },
          ]);
          setPhase('error');
        }
      }, 400);
      return () => clearTimeout(timer);
    }
  }, [phase, visibleLines, lines.length, onConfirm]);

  const handleSubmit = () => {
    if (input !== service?.name) {
      setLines([
        { text: `  railpush delete --service "${input}"`, color: 'text-content-primary', delay: 0 },
        { text: '', delay: 100 },
        { text: `  Error: name does not match. Expected "${service?.name}"`, color: 'text-status-error', delay: 300 },
        { text: '', delay: 200 },
      ]);
      setVisibleLines(0);
      setInput('');
      return;
    }

    const svcName = service?.name || 'unknown';
    const svcId = service?.id?.slice(0, 8) || '--------';
    const routeHost = service?.public_url ? hostnameFromUrl(service.public_url) : buildDefaultServiceHostname(svcName);
    const routeDisplay = routeHost || '<service-domain>';

    setPhase('deleting');
    setLines([
      { text: `  railpush delete --service "${svcName}" --confirm`, color: 'text-content-primary', delay: 0 },
      { text: '', delay: 200 },
      { text: `  Destroying service ${svcName} (${svcId})...`, color: 'text-status-warning', delay: 400 },
      { text: '', delay: 100 },
      { text: '  [1/4] Stopping container...', color: 'text-content-secondary', delay: 600 },
      { text: '        docker stop sr-' + svcId, color: 'text-content-tertiary', delay: 300 },
      { text: '        Container stopped.', color: 'text-green-400', delay: 500 },
      { text: '', delay: 100 },
      { text: '  [2/4] Removing container...', color: 'text-content-secondary', delay: 400 },
      { text: '        docker rm -f sr-' + svcId, color: 'text-content-tertiary', delay: 300 },
      { text: '        Container removed.', color: 'text-green-400', delay: 500 },
      { text: '', delay: 100 },
      { text: '  [3/4] Removing routes...', color: 'text-content-secondary', delay: 400 },
      { text: `        DELETE /${routeDisplay}`, color: 'text-content-tertiary', delay: 300 },
      { text: '        Route removed.', color: 'text-green-400', delay: 500 },
      { text: '', delay: 100 },
      { text: '  [4/4] Deleting from database...', color: 'text-content-secondary', delay: 400 },
      { text: `        DELETE FROM services WHERE id = '${svcId}...'`, color: 'text-content-tertiary', delay: 300 },
      { text: '        Record deleted.', color: 'text-green-400', delay: 500 },
    ]);
    setVisibleLines(0);
    setInput('');
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center animate-fade-in" onClick={onClose}>
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        className="relative w-[90%] max-w-[640px] animate-slide-up"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Terminal window chrome */}
        <div className="bg-[#1a1a1a] rounded-t-lg border border-b-0 border-[#333] px-4 py-2.5 flex items-center gap-2">
          <div className="flex gap-1.5">
            <button onClick={onClose} className="w-3 h-3 rounded-full bg-[#ff5f57] hover:brightness-110 transition-all" />
            <div className="w-3 h-3 rounded-full bg-[#febc2e]" />
            <div className="w-3 h-3 rounded-full bg-[#28c840]" />
          </div>
          <span className="flex-1 text-center text-xs font-mono text-[#888]">
            railpush -- delete-service
          </span>
        </div>

        {/* Terminal body */}
        <div
          ref={termRef}
          className="bg-[#0d0d0d] rounded-b-lg border border-t-0 border-[#333] p-4 font-mono text-sm max-h-[70vh] overflow-y-auto"
        >
          {/* Warning banner */}
          <div className="mb-3 text-status-error">
            <div>  *** WARNING: DESTRUCTIVE ACTION ***</div>
          </div>
          <div className="mb-3 text-content-secondary text-xs leading-relaxed">
            <div>  This will permanently destroy the service</div>
            <div>  <span className="text-content-primary font-semibold">{service?.name}</span> and all associated resources:</div>
            <div className="text-content-tertiary mt-1">    - Docker container &amp; image</div>
            <div className="text-content-tertiary">    - Caddy routes &amp; TLS certificates</div>
            <div className="text-content-tertiary">    - Deploy history &amp; logs</div>
            <div className="text-content-tertiary">    - Environment variables</div>
          </div>

          {/* Animated output lines */}
          {lines.slice(0, visibleLines).map((line, i) => (
            <div key={i} className={`${line.color || 'text-content-primary'} text-xs leading-relaxed`}>
              {line.text || '\u00A0'}
            </div>
          ))}

          {/* Input prompt */}
          {(phase === 'prompt' || phase === 'error') && (
            <div className="flex items-center mt-3 text-xs">
              <span className="text-green-400 shrink-0">  Type "{service?.name}" to confirm:</span>
              <span className="text-content-primary ml-1 shrink-0">~$</span>
              <input
                ref={inputRef}
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleSubmit();
                  if (e.key === 'Escape') onClose();
                }}
                className="flex-1 bg-transparent border-none outline-none text-content-primary font-mono text-xs ml-1.5 caret-green-400"
                spellCheck={false}
                autoComplete="off"
              />
              <span className="w-2 h-4 bg-green-400/80 animate-blink-cursor" />
            </div>
          )}

          {/* Deleting spinner */}
          {phase === 'deleting' && visibleLines < lines.length && (
            <div className="flex items-center mt-2 text-xs text-content-tertiary">
              <span className="animate-spin inline-block mr-2">&#9676;</span>
              Processing...
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function ServiceSettings() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const navigate = useNavigate();
  const [service, setService] = useState<Service | null>(null);
  const [, setLoading] = useState(true);
  const [deleteModal, setDeleteModal] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deployAutomationMode, setDeployAutomationMode] = useState<DeployAutomationMode>('push');
  const [workflowAllowlist, setWorkflowAllowlist] = useState('');
  const [gateConfigLoaded, setGateConfigLoaded] = useState(false);
  const [gateLoadError, setGateLoadError] = useState(false);
  const [formData, setFormData] = useState({
    name: '',
    branch: 'main',
    build_command: '',
    start_command: '',
    pre_deploy_command: '',
    health_check_path: '/healthz',
    auto_deploy: true,
    docker_access: false,
  });

  useEffect(() => {
    if (!serviceId) return;
    setGateLoadError(false);
    setGateConfigLoaded(false);
    Promise.all([
      servicesApi.get(serviceId),
      envVarsApi.list(serviceId).catch(() => null),
    ])
      .then(([s, envVars]) => {
        setService(s);
        setFormData({
          name: s.name,
          branch: s.branch,
          build_command: s.build_command,
          start_command: s.start_command,
          pre_deploy_command: s.pre_deploy_command || '',
          health_check_path: s.health_check_path || '/healthz',
          auto_deploy: s.auto_deploy,
          docker_access: s.docker_access ?? false,
        });

        if (!envVars) {
          setGateConfigLoaded(false);
          setGateLoadError(true);
          setWorkflowAllowlist('');
          setDeployAutomationMode(s.auto_deploy ? 'push' : 'off');
          return;
        }

        const byKey = new Map<string, string>();
        for (const envVar of envVars) {
          const key = (envVar.key || '').trim().toUpperCase();
          if (!key) continue;
          byKey.set(key, (envVar.value || '').trim());
        }

        const githubGateEnabled =
          parseTruthyEnv(byKey.get('RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY')) ||
          parseTruthyEnv(byKey.get('RAILPUSH_GITHUB_ACTIONS_ENABLED')) ||
          parseTruthyEnv(byKey.get('RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS'));

        const workflowRaw = (byKey.get('RAILPUSH_GITHUB_ACTIONS_WORKFLOWS') || byKey.get('RAILPUSH_GITHUB_ACTIONS_WORKFLOW') || '').trim();
        setWorkflowAllowlist(parseWorkflowNames(workflowRaw).join(', '));
        setDeployAutomationMode(!s.auto_deploy ? 'off' : (githubGateEnabled ? 'workflow_success' : 'push'));
        setGateConfigLoaded(true);
        setGateLoadError(false);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [serviceId]);

  const handleSave = async () => {
    if (!serviceId) return;

    const workflowNames = parseWorkflowNames(workflowAllowlist);
    const nextAutoDeploy = deployAutomationMode !== 'off';

    try {
      setSaving(true);
      await servicesApi.update(serviceId, {
        ...formData,
        auto_deploy: nextAutoDeploy,
      });

      if (gateConfigLoaded) {
        if (deployAutomationMode === 'workflow_success' && nextAutoDeploy) {
          const env_vars: Array<{ key: string; value: string; is_secret: boolean }> = [
            { key: 'RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY', value: 'true', is_secret: false },
          ];
          if (workflowNames.length > 0) {
            env_vars.push({
              key: 'RAILPUSH_GITHUB_ACTIONS_WORKFLOWS',
              value: workflowNames.join(', '),
              is_secret: false,
            });
          }
          const deleteKeys = [
            'RAILPUSH_GITHUB_ACTIONS_ENABLED',
            'RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS',
            'RAILPUSH_GITHUB_ACTIONS_WORKFLOW',
          ];
          if (workflowNames.length === 0) {
            deleteKeys.push('RAILPUSH_GITHUB_ACTIONS_WORKFLOWS');
          }
          await envVarsApi.merge(serviceId, { env_vars, delete: deleteKeys });
        } else {
          await envVarsApi.merge(serviceId, {
            env_vars: [],
            delete: [
              'RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY',
              'RAILPUSH_GITHUB_ACTIONS_ENABLED',
              'RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS',
              'RAILPUSH_GITHUB_ACTIONS_WORKFLOW',
              'RAILPUSH_GITHUB_ACTIONS_WORKFLOWS',
            ],
          });
        }
      }

      setFormData((prev) => ({ ...prev, auto_deploy: nextAutoDeploy }));
      setWorkflowAllowlist(workflowNames.join(', '));
      toast.success(gateConfigLoaded ? 'Settings saved' : 'Settings saved (GitHub Actions gate config not loaded)');
    } catch {
      toast.error('Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = useCallback(async () => {
    if (!serviceId) return;
    await servicesApi.delete(serviceId);
  }, [serviceId]);

  return (
    <div>
      <h1 className="text-2xl font-semibold text-content-primary mb-6">Settings</h1>

      {/* Service Details */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Service Details
        </h2>
        <Card padding="lg">
          <div className="space-y-4">
            <Input
              label="Name"
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            />
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-content-primary mb-1.5">Region</label>
                <div className="px-3 py-2 bg-surface-tertiary border border-border-default rounded-md text-sm text-content-secondary">
                  Self-Hosted (read-only)
                </div>
              </div>
              <Input
                label="Branch"
                value={formData.branch}
                onChange={(e) => setFormData({ ...formData, branch: e.target.value })}
              />
            </div>
            <Button onClick={handleSave} disabled={saving}>{saving ? 'Saving...' : 'Save'}</Button>
          </div>
        </Card>
      </div>

      {/* Build & Deploy */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Build & Deploy
        </h2>
        <Card padding="lg">
          <div className="space-y-4">
            <Input
              label="Build Command"
              value={formData.build_command}
              onChange={(e) => setFormData({ ...formData, build_command: e.target.value })}
              placeholder="e.g., npm install && npm run build"
            />
            <Input
              label="Start Command"
              value={formData.start_command}
              onChange={(e) => setFormData({ ...formData, start_command: e.target.value })}
              placeholder="e.g., npm start"
            />
            <div>
              <Input
                label="Pre-Deploy Command"
                value={formData.pre_deploy_command}
                onChange={(e) => setFormData({ ...formData, pre_deploy_command: e.target.value })}
                placeholder="e.g., npx prisma migrate deploy"
                hint="Runs AFTER build completes, BEFORE the new version goes live. Use for database migrations, cache warming, etc."
              />
              {formData.pre_deploy_command && (
                <div className="mt-2 flex items-center gap-2 px-3 py-2 rounded-md bg-amber-500/10 border border-amber-500/20">
                  <span className="text-amber-500 text-xs font-semibold">PRE-DEPLOY</span>
                  <span className="text-xs text-content-secondary">
                    This command runs as a separate job before each deploy. If it fails, the deploy is aborted.
                  </span>
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-content-primary mb-2">Auto-Deploy</label>
              <div className="space-y-2">
                {[
                  {
                    value: 'push',
                    label: 'On Commit',
                    desc: 'Deploy automatically on every push to the branch',
                  },
                  {
                    value: 'workflow_success',
                    label: 'After GitHub Actions Success',
                    desc: 'Ignore push webhooks and deploy only after successful workflow_run events',
                  },
                  {
                    value: 'off',
                    label: 'Off',
                    desc: 'Only deploy manually or via API',
                  },
                ].map((opt) => (
                  <label key={opt.value} className="flex items-start gap-3 p-3 rounded-md hover:bg-surface-tertiary cursor-pointer transition-colors">
                    <input
                      type="radio"
                      name="auto_deploy_mode"
                      checked={deployAutomationMode === opt.value}
                      onChange={() => setDeployAutomationMode(opt.value as DeployAutomationMode)}
                      className="mt-0.5 accent-brand"
                    />
                    <div>
                      <div className="text-sm font-medium text-content-primary">{opt.label}</div>
                      <div className="text-xs text-content-secondary">{opt.desc}</div>
                    </div>
                  </label>
                ))}
              </div>
              {deployAutomationMode === 'workflow_success' && (
                <div className="mt-3 rounded-md border border-border-default bg-surface-tertiary/30 p-3">
                  <Input
                    label="Allowed Workflow Names (optional)"
                    value={workflowAllowlist}
                    onChange={(e) => setWorkflowAllowlist(e.target.value)}
                    placeholder="CI, Release Build"
                    hint="Comma-separated workflow names. Leave blank to deploy after any successful workflow_run for this branch."
                  />
                </div>
              )}
              {gateLoadError && (
                <div className="mt-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-400">
                  Could not load current GitHub Actions gate env vars. Save will keep existing gate env vars unchanged until they can be loaded.
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-content-primary mb-2">Docker Access</label>
              <div className="space-y-2">
                {[
                  { value: false, label: 'Disabled', desc: 'Standard container isolation (recommended)' },
                  { value: true, label: 'Enabled', desc: 'Injects Docker-in-Docker sidecar for services that need to run Docker commands' },
                ].map((opt) => (
                  <label key={String(opt.value)} className="flex items-start gap-3 p-3 rounded-md hover:bg-surface-tertiary cursor-pointer transition-colors">
                    <input
                      type="radio"
                      name="docker_access"
                      checked={formData.docker_access === opt.value}
                      onChange={() => setFormData({ ...formData, docker_access: opt.value })}
                      className="mt-0.5 accent-brand"
                    />
                    <div>
                      <div className="text-sm font-medium text-content-primary">{opt.label}</div>
                      <div className="text-xs text-content-secondary">{opt.desc}</div>
                    </div>
                  </label>
                ))}
              </div>
            </div>

            <Input
              label="Health Check Path"
              value={formData.health_check_path}
              onChange={(e) => setFormData({ ...formData, health_check_path: e.target.value })}
              placeholder="/healthz"
            />

            <Button onClick={handleSave} disabled={saving}>{saving ? 'Saving...' : 'Save'}</Button>
          </div>
        </Card>
      </div>

      {/* Deploy Hook */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Deploy Hook
        </h2>
        <Card>
          <div className="flex items-center gap-2">
            <code className="flex-1 text-xs font-mono text-content-secondary bg-surface-tertiary px-3 py-2 rounded-md overflow-x-auto">
              https://api.railpush.com/deploy/{serviceId}?key=...
            </code>
          </div>
          <p className="text-xs text-content-tertiary mt-2">
            Send a POST request to this URL to trigger a deploy.
          </p>
        </Card>
      </div>

      {/* Danger Zone */}
      <div>
        <h2 className="text-xs font-semibold uppercase tracking-wider text-status-error mb-3">
          Danger Zone
        </h2>
        <Card className="border-status-error/20">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-content-primary">Delete this service</p>
              <p className="text-xs text-content-secondary mt-0.5">
                Once deleted, this service and all its data will be permanently removed.
              </p>
            </div>
            <Button variant="danger" size="sm" onClick={() => setDeleteModal(true)}>
              Delete Service
            </Button>
          </div>
        </Card>
      </div>

      {/* Terminal-style Delete Modal */}
      <TerminalDeleteModal
        open={deleteModal}
        onClose={() => setDeleteModal(false)}
        service={service}
        onConfirm={async () => {
          await handleDelete();
          setTimeout(() => navigate('/'), 2000);
        }}
      />
    </div>
  );
}
