import { useNavigate } from 'react-router-dom';
import { CreditCard, FileText, User, Bell, Key, LogOut, ChevronRight } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { auth } from '../lib/api';
import { useSession } from '../lib/session';

export function Settings() {
  const navigate = useNavigate();
  const { user, workspace } = useSession();

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
