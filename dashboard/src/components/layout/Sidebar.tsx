import { useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  LayoutDashboard, Layers, Link2, FolderKanban,
  Globe, Globe2, FileText, Lock, Cog, Clock, Database, Key,
  Settings, BookOpen, MessageSquare, CreditCard, LogOut, LifeBuoy, Coins, Server,
  ArrowLeft, BarChart3, ScrollText, Network, HardDrive, TrendingUp,
  Info, ShieldCheck, Save, AppWindow, List,
  PanelLeftClose, PanelLeftOpen, Siren, Users, Mail
} from 'lucide-react';
import { cn } from '../../lib/utils';
import { useSidebar } from '../../lib/sidebar';
import { useSession } from '../../lib/session';
import { LogoMark } from '../Logo';
import { auth } from '../../lib/api';

interface SidebarItem {
  icon: React.ReactNode;
  label: string;
  path: string;
  active?: boolean;
  tone?: 'default' | 'ops';
}

function NavItem({ icon, label, path, active, collapsed, tone = 'default' }: SidebarItem & { active: boolean; collapsed?: boolean }) {
  const navigate = useNavigate();
  const isOpsTone = tone === 'ops';
  return (
    <button
      onClick={() => navigate(path)}
      title={collapsed ? label : undefined}
      className={cn(
        'w-full flex items-center gap-3 rounded-lg text-[13px] transition-all duration-200 cursor-pointer border relative overflow-hidden group',
        collapsed ? 'justify-center p-2' : 'px-3 py-2',
        active
          ? (isOpsTone ? 'bg-surface-tertiary/70 text-content-primary font-medium border-border-hover' : 'bg-brand/10 text-brand font-medium border-brand/20')
          : (isOpsTone ? 'text-content-secondary hover:bg-surface-tertiary/60 hover:text-content-primary border-transparent' : 'text-content-secondary hover:bg-surface-tertiary hover:text-content-primary border-transparent')
      )}
    >
      {active && <div className={cn("absolute left-0 top-0 bottom-0 w-0.5", isOpsTone ? "bg-content-tertiary" : "bg-brand")} />}
      <span
        className={cn(
          "w-4 h-4 flex-shrink-0 transition-transform duration-200",
          active ? "scale-110" : "opacity-70 group-hover:scale-110 group-hover:opacity-100"
        )}
      >
        {icon}
      </span>
      {!collapsed && <span className="truncate">{label}</span>}
    </button>
  );
}

function SectionLabel({ label, collapsed }: { label: string; collapsed?: boolean }) {
  if (collapsed) return <div className="my-2 border-t border-border-default/50" />;
  return (
    <div className={cn(
      "px-3 pt-5 pb-2 text-[10px] font-bold uppercase tracking-widest opacity-80",
      "text-content-tertiary",
    )}>
      {label}
    </div>
  );
}

function OpsPanel({ collapsed, children }: { collapsed?: boolean; children: React.ReactNode }) {
  return (
    <div
      className={cn(
        'mt-4 rounded-xl border border-brand/10 bg-surface-tertiary/20 p-2 relative overflow-hidden',
        "before:content-[''] before:absolute before:inset-0 before:pointer-events-none before:bg-[radial-gradient(circle_at_30%_-20%,rgba(59,130,246,0.14),transparent_60%)] before:opacity-90",
        "after:content-[''] after:absolute after:inset-x-0 after:top-0 after:h-px after:bg-gradient-to-r after:from-transparent after:via-brand/25 after:to-transparent after:opacity-80"
      )}
    >
      <div className="relative">
        <div className={cn('flex items-center gap-2 px-2 pt-2 pb-1', collapsed && 'justify-center px-0')}>
          <ShieldCheck className="w-4 h-4 text-brand/70" />
          {!collapsed && (
            <>
              <span className="text-[10px] font-bold uppercase tracking-widest text-content-tertiary">Ops Mode</span>
              <span className="ml-auto text-[10px] px-2 py-0.5 rounded-full border border-brand/15 bg-brand/5 text-brand/80">
                Restricted
              </span>
            </>
          )}
        </div>
        <div className="mt-1 space-y-1">
          {children}
        </div>
      </div>
    </div>
  );
}

export function Sidebar() {
  const location = useLocation();
  const params = useParams();
  const path = location.pathname;
  const { collapsed, toggle } = useSidebar();
  const { user, isOps } = useSession();

  // Extract route params
  const serviceId = params.serviceId;
  const dbId = params.dbId;
  const domainMatch = path.match(/^\/domains\/([0-9a-f-]{36})/);
  const domainId = params.domainId || (domainMatch ? domainMatch[1] : undefined);

  const width = collapsed ? 'w-[68px]' : 'w-[260px]';
  const asideClass = cn(
    width,
    'app-sidebar h-screen fixed left-0 top-0 bg-surface-secondary border-r border-border-default flex flex-col overflow-y-auto z-40 transition-all duration-300 ease-[cubic-bezier(0.2,0,0,1)]'
  );

  // Service detail sidebar
  if (serviceId) {
    const base = `/services/${serviceId}`;
    return (
      <aside className={asideClass}>
        <div className="h-[60px] px-4 border-b border-border-default flex items-center justify-between shrink-0">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary animate-enter">
              <span className="font-semibold text-content-primary tracking-tight">RailPush</span>
              <span className="bg-surface-tertiary px-1.5 py-0.5 rounded text-[10px] border border-border-subtle">Svc</span>
            </div>
          )}
          <button onClick={toggle} className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? "Expand sidebar" : "Collapse sidebar"}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-3">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Dashboard" path="/" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-4 py-3 border-b border-border-default/50 mx-2 mb-2 animate-enter animate-enter-delay-1">
            <div className="text-[10px] text-content-tertiary uppercase tracking-wider font-semibold mb-1">Service</div>
            <div className="text-sm font-bold text-content-primary truncate font-mono">{serviceId}</div>
          </div>
        )}

        <nav className="flex-1 px-3 py-2 space-y-1">
          <NavItem icon={<LayoutDashboard className="w-4 h-4" />} label="Overview" path={base} active={path === base} collapsed={collapsed} />
          <NavItem icon={<ScrollText className="w-4 h-4" />} label="Events" path={`${base}/events`} active={path.includes('/events')} collapsed={collapsed} />
          <NavItem icon={<BarChart3 className="w-4 h-4" />} label="Metrics" path={`${base}/metrics`} active={path.includes('/metrics')} collapsed={collapsed} />
          <NavItem icon={<ScrollText className="w-4 h-4" />} label="Logs" path={`${base}/logs`} active={path.includes('/logs')} collapsed={collapsed} />
          <NavItem icon={<Cog className="w-4 h-4" />} label="Environment" path={`${base}/environment`} active={path.includes('/environment')} collapsed={collapsed} />
          <NavItem icon={<Network className="w-4 h-4" />} label="Networking" path={`${base}/networking`} active={path.includes('/networking')} collapsed={collapsed} />
          <NavItem icon={<HardDrive className="w-4 h-4" />} label="Disks" path={`${base}/disks`} active={path.includes('/disks')} collapsed={collapsed} />
          <NavItem icon={<TrendingUp className="w-4 h-4" />} label="Scaling" path={`${base}/scaling`} active={path.includes('/scaling')} collapsed={collapsed} />
          <NavItem icon={<Settings className="w-4 h-4" />} label="Settings" path={`${base}/settings`} active={path.includes('/settings')} collapsed={collapsed} />
        </nav>

        <div className="p-3 border-t border-border-default/50 mt-auto">
          <NavItem icon={<BookOpen className="w-4 h-4" />} label="Docs" path="/docs" active={false} collapsed={collapsed} />
        </div>
      </aside>
    );
  }

  // Database detail sidebar
  if (dbId) {
    const base = `/databases/${dbId}`;
    return (
      <aside className={asideClass}>
        <div className="h-[60px] px-4 border-b border-border-default flex items-center justify-between shrink-0">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary animate-enter">
              <span className="font-semibold text-content-primary tracking-tight">RailPush</span>
              <span className="bg-surface-tertiary px-1.5 py-0.5 rounded text-[10px] border border-border-subtle">DB</span>
            </div>
          )}
          <button onClick={toggle} className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? "Expand sidebar" : "Collapse sidebar"}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-3">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Dashboard" path="/" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-4 py-3 border-b border-border-default/50 mx-2 mb-2 animate-enter animate-enter-delay-1">
            <div className="text-[10px] text-content-tertiary uppercase tracking-wider font-semibold mb-1">Database</div>
            <div className="text-sm font-bold text-content-primary truncate font-mono">{dbId}</div>
          </div>
        )}

        <nav className="flex-1 px-3 py-2 space-y-1">
          <NavItem icon={<Info className="w-4 h-4" />} label="Info" path={base} active={path === base} collapsed={collapsed} />
          <NavItem icon={<BarChart3 className="w-4 h-4" />} label="Metrics" path={`${base}/metrics`} active={path.includes('/metrics')} collapsed={collapsed} />
          <NavItem icon={<ShieldCheck className="w-4 h-4" />} label="Access Control" path={`${base}/access`} active={path.includes('/access')} collapsed={collapsed} />
          <NavItem icon={<Save className="w-4 h-4" />} label="Backups" path={`${base}/backups`} active={path.includes('/backups')} collapsed={collapsed} />
          <NavItem icon={<AppWindow className="w-4 h-4" />} label="Apps" path={`${base}/apps`} active={path.includes('/apps')} collapsed={collapsed} />
          <NavItem icon={<Settings className="w-4 h-4" />} label="Settings" path={`${base}/settings`} active={path.includes('/settings')} collapsed={collapsed} />
        </nav>
      </aside>
    );
  }

  // Domain detail sidebar
  if (domainId) {
    const base = `/domains/${domainId}`;
    return (
      <aside className={asideClass}>
        <div className="h-[60px] px-4 border-b border-border-default flex items-center justify-between shrink-0">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary animate-enter">
              <span className="font-semibold text-content-primary tracking-tight">RailPush</span>
              <span className="bg-surface-tertiary px-1.5 py-0.5 rounded text-[10px] border border-border-subtle">DNS</span>
            </div>
          )}
          <button onClick={toggle} className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? "Expand sidebar" : "Collapse sidebar"}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-3">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Domains" path="/domains" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-4 py-3 border-b border-border-default/50 mx-2 mb-2 animate-enter animate-enter-delay-1">
            <div className="text-[10px] text-content-tertiary uppercase tracking-wider font-semibold mb-1">Domain</div>
            <div className="text-sm font-bold text-content-primary truncate font-mono">{domainId}</div>
          </div>
        )}

        <nav className="flex-1 px-3 py-2 space-y-1">
          <NavItem icon={<Info className="w-4 h-4" />} label="Overview" path={base} active={path === base} collapsed={collapsed} />
          <NavItem icon={<List className="w-4 h-4" />} label="DNS Records" path={`${base}/dns`} active={path.includes('/dns')} collapsed={collapsed} />
          <NavItem icon={<Settings className="w-4 h-4" />} label="Settings" path={`${base}/settings`} active={path.endsWith('/settings')} collapsed={collapsed} />
        </nav>
      </aside>
    );
  }

  // Workspace level sidebar (default)
  const userLabel = user?.username || user?.email || 'User';
  const userInitial = (userLabel || 'U').slice(0, 1).toUpperCase();
  const userSub = user?.email ? user.email : (user?.role || '');

  return (
    <aside className={asideClass}>
      <div className="h-[60px] px-4 border-b border-border-default flex items-center justify-between shrink-0">
        {!collapsed ? (
          <div className="flex items-center gap-2.5 text-sm cursor-pointer animate-enter">
            <LogoMark size={28} />
            <div className="flex flex-col leading-tight">
              <span className="font-bold text-content-primary tracking-tight text-[15px]">RailPush</span>
              <span className="text-[11px] text-content-tertiary font-medium">Dashboard</span>
            </div>
          </div>
        ) : (
          <div className="w-full flex justify-center animate-enter">
            <LogoMark size={28} />
          </div>
        )}
        <button onClick={toggle} className={cn('p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors', collapsed && 'hidden')} title="Collapse sidebar">
          <PanelLeftClose className="w-4 h-4" />
        </button>
      </div>

      {collapsed && (
        <div className="p-3 flex justify-center">
          <button onClick={toggle} className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title="Expand sidebar">
            <PanelLeftOpen className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Quick Access / Main Nav */}
      <nav className="flex-1 px-3 py-4 space-y-1">
        <NavItem icon={<LayoutDashboard className="w-4 h-4" />} label="Dashboard" path="/" active={path === '/'} collapsed={collapsed} />
        <NavItem icon={<Layers className="w-4 h-4" />} label="Blueprints" path="/blueprints" active={path.startsWith('/blueprints')} collapsed={collapsed} />
        <NavItem icon={<Link2 className="w-4 h-4" />} label="Env Groups" path="/env-groups" active={path.startsWith('/env-groups')} collapsed={collapsed} />
        <NavItem icon={<FolderKanban className="w-4 h-4" />} label="Projects" path="/projects" active={path.startsWith('/projects')} collapsed={collapsed} />

        <SectionLabel label="Resources" collapsed={collapsed} />
        <NavItem icon={<FileText className="w-4 h-4" />} label="Static Sites" path="/static-sites" active={path.startsWith('/static-sites')} collapsed={collapsed} />
        <NavItem icon={<Globe className="w-4 h-4" />} label="Web Services" path="/web-services" active={path.startsWith('/web-services')} collapsed={collapsed} />
        <NavItem icon={<Lock className="w-4 h-4" />} label="Private Services" path="/private-services" active={path.startsWith('/private-services')} collapsed={collapsed} />
        <NavItem icon={<Cog className="w-4 h-4" />} label="Workers" path="/workers" active={path.startsWith('/workers')} collapsed={collapsed} />
        <NavItem icon={<Clock className="w-4 h-4" />} label="Cron Jobs" path="/cron-jobs" active={path.startsWith('/cron-jobs')} collapsed={collapsed} />
        <NavItem icon={<Database className="w-4 h-4" />} label="PostgreSQL" path="/postgres" active={path.startsWith('/postgres')} collapsed={collapsed} />
        <NavItem icon={<Key className="w-4 h-4" />} label="Key Value" path="/keyvalue" active={path.startsWith('/keyvalue')} collapsed={collapsed} />

        <SectionLabel label="Networking" collapsed={collapsed} />
        <NavItem icon={<Globe2 className="w-4 h-4" />} label="Domains" path="/domains" active={path.startsWith('/domains')} collapsed={collapsed} />

        {isOps && (
          <OpsPanel collapsed={collapsed}>
            <NavItem icon={<ShieldCheck className="w-4 h-4" />} label="Overview" path="/ops" active={path === '/ops'} collapsed={collapsed} tone="ops" />
            <NavItem icon={<Users className="w-4 h-4" />} label="Customers" path="/ops/customers" active={path.startsWith('/ops/customers')} collapsed={collapsed} tone="ops" />
            <NavItem icon={<AppWindow className="w-4 h-4" />} label="Services" path="/ops/services" active={path.startsWith('/ops/services')} collapsed={collapsed} tone="ops" />
            <NavItem icon={<TrendingUp className="w-4 h-4" />} label="Deployments" path="/ops/deployments" active={path.startsWith('/ops/deployments')} collapsed={collapsed} tone="ops" />
            <NavItem icon={<CreditCard className="w-4 h-4" />} label="Billing" path="/ops/billing" active={path.startsWith('/ops/billing')} collapsed={collapsed} tone="ops" />

            {!collapsed && (
              <div className="pl-3 mt-1 space-y-0.5 border-l border-border-default/30 ml-2">
                <NavItem icon={<LifeBuoy className="w-3.5 h-3.5" />} label="Tickets" path="/ops/tickets" active={path.startsWith('/ops/tickets')} collapsed={false} tone="ops" />
                <NavItem icon={<Coins className="w-3.5 h-3.5" />} label="Credits" path="/ops/credits" active={path.startsWith('/ops/credits')} collapsed={false} tone="ops" />
                <NavItem icon={<Database className="w-3.5 h-3.5" />} label="Datastores" path="/ops/datastores" active={path.startsWith('/ops/datastores')} collapsed={false} tone="ops" />
                <NavItem icon={<ScrollText className="w-3.5 h-3.5" />} label="Audit Log" path="/ops/audit" active={path.startsWith('/ops/audit')} collapsed={false} tone="ops" />
                <NavItem icon={<Server className="w-3.5 h-3.5" />} label="Technical" path="/ops/technical" active={path.startsWith('/ops/technical')} collapsed={false} tone="ops" />
                <NavItem icon={<BarChart3 className="w-3.5 h-3.5" />} label="Performance" path="/ops/performance" active={path.startsWith('/ops/performance')} collapsed={false} tone="ops" />
                <NavItem icon={<Mail className="w-3.5 h-3.5" />} label="Email" path="/ops/email" active={path.startsWith('/ops/email')} collapsed={false} tone="ops" />
                <NavItem icon={<Settings className="w-3.5 h-3.5" />} label="Settings" path="/ops/settings" active={path.startsWith('/ops/settings')} collapsed={false} tone="ops" />
                <NavItem icon={<Siren className="w-3.5 h-3.5" />} label="Incidents" path="/ops/incidents" active={path.startsWith('/ops/incidents')} collapsed={false} tone="ops" />
              </div>
            )}
            {collapsed && (
              <NavItem icon={<Siren className="w-4 h-4" />} label="Incidents" path="/ops/incidents" active={path.startsWith('/ops/incidents')} collapsed={true} tone="ops" />
            )}
          </OpsPanel>
        )}
      </nav>

      <div className="px-3 py-2 border-t border-border-default/50 space-y-1 bg-surface-primary">
        <NavItem icon={<Settings className="w-4 h-4" />} label="Settings" path="/settings" active={path === '/settings'} collapsed={collapsed} />
        <NavItem icon={<CreditCard className="w-4 h-4" />} label="Billing" path="/billing" active={path === '/billing'} collapsed={collapsed} />
        <NavItem icon={<LifeBuoy className="w-4 h-4" />} label="Support" path="/support" active={path.startsWith('/support')} collapsed={collapsed} />
        <NavItem icon={<BookOpen className="w-4 h-4" />} label="Docs" path="/docs" active={false} collapsed={collapsed} />
        <NavItem icon={<MessageSquare className="w-4 h-4" />} label="Community" path="/community" active={false} collapsed={collapsed} />
      </div>

      <div className="p-3 border-t border-border-default h-[60px] flex items-center bg-surface-secondary/50">
        <div className={cn('flex items-center w-full', collapsed ? 'justify-center' : 'gap-3')}>
          <div className="w-8 h-8 rounded-full bg-surface-tertiary border border-border-default flex items-center justify-center text-xs font-bold text-brand">
            {userInitial}
          </div>
          {!collapsed && (
            <>
              <div className="flex-1 min-w-0 flex flex-col justify-center">
                <div className="text-[13px] font-semibold text-content-primary truncate">{userLabel}</div>
                {userSub && <div className="text-[10px] text-content-tertiary truncate leading-tight">{userSub}</div>}
              </div>
              <button
                onClick={() => { auth.logout(); }}
                title="Log out"
                className="p-1.5 rounded-md text-content-tertiary hover:text-status-error hover:bg-status-error/10 transition-colors"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </>
          )}
        </div>
      </div>
    </aside>
  );
}
