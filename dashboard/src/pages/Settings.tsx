import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { CreditCard, FileText, User, Bell, Key, LogOut, ChevronRight, Sparkles, Plus, Trash2, Copy, Check, Plug } from 'lucide-react';
import { toast } from 'sonner';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { auth, apiKeys, settings as settingsApi } from '../lib/api';
import { useSession } from '../lib/session';

export function Settings() {
  const navigate = useNavigate();
  const { user, workspace } = useSession();
  const [blueprintAIEnabled, setBlueprintAIEnabled] = useState<boolean>(Boolean(user?.blueprint_ai_autogen_enabled));
  const [blueprintAIAvailable, setBlueprintAIAvailable] = useState<boolean>(false);
  const [blueprintAIModel, setBlueprintAIModel] = useState<string>('minimax/minimax-m2.5');
  const [blueprintAILoading, setBlueprintAILoading] = useState<boolean>(true);
  const [blueprintAISaving, setBlueprintAISaving] = useState<boolean>(false);

  // API Keys state
  const [keys, setKeys] = useState<Array<{ id: string; name: string; created_at: string }>>([]);
  const [keysLoading, setKeysLoading] = useState(true);
  const [newKeyName, setNewKeyName] = useState('');
  const [creatingKey, setCreatingKey] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [copiedKey, setCopiedKey] = useState(false);

  const loadKeys = useCallback(async () => {
    try {
      const list = await apiKeys.list();
      setKeys(list);
    } catch {
      // ignore
    } finally {
      setKeysLoading(false);
    }
  }, []);

  const handleCreateKey = async () => {
    const name = newKeyName.trim() || 'Untitled key';
    setCreatingKey(true);
    try {
      const result = await apiKeys.create(name);
      setRevealedKey(result.key);
      setNewKeyName('');
      toast.success('API key created');
      loadKeys();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to create key');
    } finally {
      setCreatingKey(false);
    }
  };

  const handleDeleteKey = async (id: string) => {
    try {
      await apiKeys.delete(id);
      setKeys(prev => prev.filter(k => k.id !== id));
      toast.success('API key revoked');
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to delete key');
    }
  };

  const copyKey = () => {
    if (revealedKey) {
      navigator.clipboard.writeText(revealedKey);
      setCopiedKey(true);
      setTimeout(() => setCopiedKey(false), 2000);
    }
  };

  useEffect(() => {
    let mounted = true;
    setBlueprintAILoading(true);
    settingsApi
      .getBlueprintAI()
      .then((cfg) => {
        if (!mounted) return;
        setBlueprintAIEnabled(Boolean(cfg.enabled));
        setBlueprintAIAvailable(Boolean(cfg.available));
        if (cfg.model) setBlueprintAIModel(cfg.model);
      })
      .catch(() => {
        if (!mounted) return;
        setBlueprintAIAvailable(false);
      })
      .finally(() => {
        if (!mounted) return;
        setBlueprintAILoading(false);
      });
    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    loadKeys();
  }, [loadKeys]);

  const toggleBlueprintAI = async () => {
    const next = !blueprintAIEnabled;
    setBlueprintAISaving(true);
    try {
      const updated = await settingsApi.updateBlueprintAI(next);
      setBlueprintAIEnabled(Boolean(updated.enabled));
      setBlueprintAIAvailable(Boolean(updated.available));
      if (updated.model) setBlueprintAIModel(updated.model);
      toast.success(`Blueprint AI ${updated.enabled ? 'enabled' : 'disabled'}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to update Blueprint AI');
    } finally {
      setBlueprintAISaving(false);
    }
  };

  return (
    <div className="space-y-8 animate-enter">
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-content-primary mb-1">Settings</h1>
        <p className="text-content-secondary text-sm">Manage your workspace preferences and account security.</p>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        {/* Workspace & Billing */}
        <div className="space-y-6">
          <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary px-1">Workspace</h2>

          <Card className="glass-panel p-0 overflow-hidden group">
            <div className="p-5 border-b border-border-default/50">
              <div className="flex items-center gap-3 mb-2">
                <div className="p-2 rounded-lg bg-brand/10 text-brand">
                  <Sparkles className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-semibold text-content-primary">Blueprint AI Autogenerate</h3>
                  <p className="text-xs text-content-secondary">Scan repo files and generate railpush.yaml during blueprint sync</p>
                </div>
              </div>
            </div>
            <div className="bg-surface-tertiary/20 px-5 py-3 flex items-center justify-between gap-3">
              <span className="text-xs text-content-tertiary">
                {blueprintAILoading
                  ? 'Loading...'
                  : blueprintAIAvailable
                    ? `Model: ${blueprintAIModel}`
                    : 'OpenRouter API key is not configured on the server'}
              </span>
              <Button
                variant={blueprintAIEnabled ? 'secondary' : 'primary'}
                size="sm"
                loading={blueprintAISaving}
                disabled={blueprintAILoading || blueprintAISaving || (!blueprintAIAvailable && !blueprintAIEnabled)}
                onClick={toggleBlueprintAI}
              >
                {blueprintAIEnabled ? 'Enabled' : 'Disabled'}
              </Button>
            </div>
          </Card>

          <Card className="glass-panel p-0 overflow-hidden group">
            <div className="p-5 border-b border-border-default/50">
              <div className="flex items-center gap-3 mb-2">
                <div className="p-2 rounded-lg bg-brand/10 text-brand">
                  <CreditCard className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-semibold text-content-primary">Billing & Plans</h3>
                  <p className="text-xs text-content-secondary">Manage subscriptions and payment methods</p>
                </div>
              </div>
            </div>
            <div className="bg-surface-tertiary/20 px-5 py-3 flex items-center justify-between">
              <span className="text-xs text-content-tertiary">Current Plan: <span className="text-content-primary font-medium">Free</span></span>
              <Button variant="ghost" size="sm" onClick={() => navigate('/billing')} className="text-xs group-hover:text-brand transition-colors">
                Manage <ChevronRight className="w-3 h-3 ml-1" />
              </Button>
            </div>
          </Card>

          <Card className="glass-panel p-0 overflow-hidden group">
            <div className="p-5 border-b border-border-default/50">
              <div className="flex items-center gap-3 mb-2">
                <div className="p-2 rounded-lg bg-purple-500/10 text-purple-400">
                  <FileText className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-semibold text-content-primary">Platform & Legal</h3>
                  <p className="text-xs text-content-secondary">Documentation, policies, and terms</p>
                </div>
              </div>
            </div>
            <div className="bg-surface-tertiary/20 px-5 py-3 flex items-center gap-3">
              <Button variant="ghost" size="sm" onClick={() => navigate('/docs')} className="text-xs">Docs</Button>
              <Button variant="ghost" size="sm" onClick={() => navigate('/privacy')} className="text-xs">Privacy Policy</Button>
            </div>
          </Card>
        </div>

        {/* User Account */}
        <div className="space-y-6">
          <h2 className="text-sm font-bold uppercase tracking-wider text-content-tertiary px-1">Account</h2>

          <Card className="glass-panel p-6 space-y-6">
            <div className="flex items-start gap-4">
              <div className="h-12 w-12 rounded-full bg-surface-tertiary border border-border-default flex items-center justify-center text-content-secondary">
                <User className="w-6 h-6" />
              </div>
              <div className="flex-1">
                <h3 className="font-semibold text-content-primary">Profile</h3>
                <p className="text-xs text-content-secondary mt-1">
                  {user?.username || 'User'}
                  {user?.email ? ` (${user.email})` : ''}
                </p>
                <div className="mt-3 flex gap-2">
                  <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-surface-tertiary border border-border-subtle text-[10px] font-mono text-content-tertiary">
                    ID: {user?.id || '-'}
                  </span>
                  <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-surface-tertiary border border-border-subtle text-[10px] font-mono text-content-tertiary">
                    Role: {user?.role || '-'}
                  </span>
                  {workspace?.name && (
                    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-surface-tertiary border border-border-subtle text-[10px] font-mono text-content-tertiary">
                      Workspace: {workspace.name}
                    </span>
                  )}
                </div>
              </div>
            </div>

            <div className="space-y-1 pt-4 border-t border-border-default/50">
              <SettingsItem
                icon={<Key className="w-4 h-4" />}
                label="Security & Auth"
                action="Manage"
              />
              <SettingsItem
                icon={<Bell className="w-4 h-4" />}
                label="Notifications"
                action="Configure"
              />
            </div>
          </Card>

          {/* API Keys */}
          <Card className="glass-panel p-0 overflow-hidden">
            <div className="p-5 border-b border-border-default/50">
              <div className="flex items-center gap-3 mb-1">
                <div className="p-2 rounded-lg bg-brand/10 text-brand">
                  <Plug className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-semibold text-content-primary">API Keys</h3>
                  <p className="text-xs text-content-secondary">Authenticate CLI tools, MCP servers, and API integrations</p>
                </div>
              </div>
            </div>

            <div className="p-5 space-y-4">
              {/* Revealed key banner */}
              {revealedKey && (
                <div className="rounded-lg border border-status-success/30 bg-status-success/5 p-4">
                  <p className="text-xs text-status-success font-semibold mb-2">Key created — copy it now. It won't be shown again.</p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-xs font-mono text-content-primary bg-surface-tertiary rounded px-3 py-2 select-all overflow-x-auto">
                      {revealedKey}
                    </code>
                    <button
                      onClick={copyKey}
                      className="p-2 rounded-md hover:bg-surface-tertiary transition-colors text-content-secondary hover:text-content-primary"
                    >
                      {copiedKey ? <Check className="w-4 h-4 text-status-success" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
              )}

              {/* Create new key */}
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  placeholder="Key name (e.g. mcp-server)"
                  value={newKeyName}
                  onChange={(e) => setNewKeyName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleCreateKey()}
                  className="flex-1 bg-surface-tertiary border border-border-default rounded-lg px-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-1 focus:ring-brand/20 transition-all"
                />
                <Button size="sm" loading={creatingKey} onClick={handleCreateKey}>
                  <Plus className="w-3.5 h-3.5 mr-1" />
                  Create
                </Button>
              </div>

              {/* Existing keys */}
              {keysLoading ? (
                <p className="text-xs text-content-tertiary">Loading...</p>
              ) : keys.length === 0 ? (
                <p className="text-xs text-content-tertiary">No API keys yet. Create one to use with the MCP server or REST API.</p>
              ) : (
                <div className="space-y-2">
                  {keys.map((k) => (
                    <div key={k.id} className="flex items-center justify-between px-3 py-2.5 rounded-lg border border-border-default bg-surface-secondary/30 group">
                      <div className="flex-1 min-w-0">
                        <span className="text-sm font-medium text-content-primary">{k.name || 'Unnamed key'}</span>
                        <div className="flex items-center gap-3 mt-0.5">
                          <span className="text-[10px] font-mono text-content-tertiary">{k.id.slice(0, 8)}...</span>
                          <span className="text-[10px] text-content-tertiary">
                            Created {new Date(k.created_at).toLocaleDateString()}
                          </span>
                        </div>
                      </div>
                      <button
                        onClick={() => handleDeleteKey(k.id)}
                        className="p-1.5 rounded-md text-content-tertiary hover:text-status-error hover:bg-status-error/10 transition-all opacity-0 group-hover:opacity-100"
                        title="Revoke key"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </Card>

          <div className="pt-4">
            <Button
              variant="danger"
              className="w-full justify-center"
              onClick={() => auth.logout()}
            >
              <LogOut className="w-4 h-4 mr-2" />
              Sign Out
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function SettingsItem({ icon, label, action }: { icon: React.ReactNode, label: string, action: string }) {
  return (
    <button className="w-full flex items-center justify-between p-2 rounded-md hover:bg-surface-tertiary transition-colors group">
      <div className="flex items-center gap-3 text-sm text-content-secondary group-hover:text-content-primary transition-colors">
        {icon}
        <span>{label}</span>
      </div>
      <span className="text-xs text-brand opacity-0 group-hover:opacity-100 transition-opacity flex items-center">
        {action} <ChevronRight className="w-3 h-3 ml-0.5" />
      </span>
    </button>
  )
}
