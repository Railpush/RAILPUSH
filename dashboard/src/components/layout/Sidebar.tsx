import { useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  LayoutDashboard, Layers, Link2, FolderKanban,
  Globe, Globe2, FileText, Lock, Cog, Clock, Database, Key,
  Settings, BookOpen, MessageSquare, CreditCard, LogOut, LifeBuoy, Coins, Server,
  ArrowLeft, BarChart3, ScrollText, Network, HardDrive, TrendingUp,
  ChevronDown, Search, Info, ShieldCheck, Save, AppWindow, List,
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
}

function NavItem({ icon, label, path, active, collapsed }: SidebarItem & { active: boolean; collapsed?: boolean }) {
  const navigate = useNavigate();
  return (
    <button
      onClick={() => navigate(path)}
      title={collapsed ? label : undefined}
      className={cn(
        'w-full flex items-center gap-2.5 rounded-md text-[13px] transition-colors duration-100 cursor-pointer border',
        collapsed ? 'justify-center px-0 py-2' : 'px-3 py-[9px]',
        active
          ? 'bg-surface-tertiary text-content-primary font-semibold border-border-default'
          : 'text-content-secondary hover:bg-surface-tertiary hover:text-content-primary border-transparent'
      )}
    >
      <span className="w-4 h-4 flex-shrink-0 opacity-70">{icon}</span>
      {!collapsed && label}
    </button>
  );
}

function SectionLabel({ label, collapsed }: { label: string; collapsed?: boolean }) {
  if (collapsed) return <div className="my-2 border-t border-border-subtle" />;
  return (
    <div className="px-3 pt-4 pb-1 text-[11px] font-semibold uppercase tracking-wider text-content-tertiary">
      {label}
    </div>
  );
}

export function Sidebar() {
  const location = useLocation();
  const params = useParams();
  const path = location.pathname;
  const { collapsed, toggle } = useSidebar();
  const { user, isOps } = useSession();

  // Extract route params — useParams works for child routes, but also parse from path as fallback
  const serviceId = params.serviceId;
  const dbId = params.dbId;
  const domainMatch = path.match(/^\/domains\/([0-9a-f-]{36})/);
  const domainId = params.domainId || (domainMatch ? domainMatch[1] : undefined);

  const width = collapsed ? 'w-[64px]' : 'w-[240px]';
  const asideClass = cn(
    width,
    'app-sidebar h-screen fixed left-0 top-0 bg-surface-secondary border-r border-border-default flex flex-col overflow-y-auto z-40 transition-all duration-200'
  );

  // Service detail sidebar
  if (serviceId) {
    const base = `/services/${serviceId}`;
    return (
      <aside className={asideClass}>
        <div className="p-4 border-b border-border-subtle flex items-center justify-between">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary">
              <span className="font-medium text-content-primary">RailPush</span>
              <ChevronDown className="w-3.5 h-3.5" />
            </div>
          )}
          <button onClick={toggle} className="p-1 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-2">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Dashboard" path="/" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-3 py-2 border-b border-border-subtle">
            <div className="text-xs text-content-tertiary uppercase tracking-wider">Service</div>
            <div className="text-sm font-semibold text-content-primary mt-0.5 truncate">{serviceId}</div>
          </div>
        )}

        <nav className="flex-1 p-2 space-y-0.5">
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

        <div className="p-2 border-t border-border-subtle">
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
        <div className="p-4 border-b border-border-subtle flex items-center justify-between">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary">
              <span className="font-medium text-content-primary">RailPush</span>
              <ChevronDown className="w-3.5 h-3.5" />
            </div>
          )}
          <button onClick={toggle} className="p-1 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-2">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Dashboard" path="/" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-3 py-2 border-b border-border-subtle">
            <div className="text-xs text-content-tertiary uppercase tracking-wider">Database</div>
            <div className="text-sm font-semibold text-content-primary mt-0.5 truncate">{dbId}</div>
          </div>
        )}

        <nav className="flex-1 p-2 space-y-0.5">
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
        <div className="p-4 border-b border-border-subtle flex items-center justify-between">
          {!collapsed && (
            <div className="flex items-center gap-2 text-sm text-content-secondary">
              <span className="font-medium text-content-primary">RailPush</span>
              <ChevronDown className="w-3.5 h-3.5" />
            </div>
          )}
          <button onClick={toggle} className="p-1 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
            {collapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
          </button>
        </div>

        <div className="p-2">
          <NavItem icon={<ArrowLeft className="w-4 h-4" />} label="Back to Domains" path="/domains" active={false} collapsed={collapsed} />
        </div>

        {!collapsed && (
          <div className="px-3 py-2 border-b border-border-subtle">
            <div className="text-xs text-content-tertiary uppercase tracking-wider">Domain</div>
            <div className="text-sm font-semibold text-content-primary mt-0.5 truncate">{domainId}</div>
          </div>
        )}

        <nav className="flex-1 p-2 space-y-0.5">
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
      <div className="p-4 border-b border-border-subtle flex items-center justify-between">
        {!collapsed ? (
          <div className="flex items-center gap-2 text-sm cursor-pointer">
            <LogoMark size={24} />
            <div className="flex flex-col leading-tight">
              <span className="font-semibold text-content-primary">RailPush</span>
              <span className="text-[11px] text-content-tertiary">Dashboard</span>
            </div>
          </div>
        ) : (
          <LogoMark size={24} />
        )}
        <button onClick={toggle} className={cn('p-1 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors', collapsed && 'hidden')} title="Collapse sidebar">
          <PanelLeftClose className="w-4 h-4" />
        </button>
      </div>

      {collapsed && (
        <div className="p-2 flex justify-center">
          <button onClick={toggle} className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors" title="Expand sidebar">
            <PanelLeftOpen className="w-4 h-4" />
          </button>
        </div>
      )}

      {!collapsed && (
        <div className="px-4 py-2 text-[11px] text-content-tertiary">
          <span className="inline-flex items-center gap-2">
            <Search className="w-3.5 h-3.5" />
            <span>Tip: use the service pages for logs, metrics, and settings.</span>
          </span>
        </div>
      )}

      <nav className="flex-1 p-2 space-y-0.5">
        <NavItem icon={<LayoutDashboard className="w-4 h-4" />} label="Dashboard" path="/" active={path === '/'} collapsed={collapsed} />
        <NavItem icon={<Layers className="w-4 h-4" />} label="Blueprints" path="/blueprints" active={path.startsWith('/blueprints')} collapsed={collapsed} />
        <NavItem icon={<Link2 className="w-4 h-4" />} label="Environment Groups" path="/env-groups" active={path.startsWith('/env-groups')} collapsed={collapsed} />
        <NavItem icon={<FolderKanban className="w-4 h-4" />} label="Projects" path="/projects" active={path.startsWith('/projects')} collapsed={collapsed} />

        <SectionLabel label="Resources" collapsed={collapsed} />
        <NavItem icon={<FileText className="w-4 h-4" />} label="Static Sites" path="/static-sites" active={path.startsWith('/static-sites')} collapsed={collapsed} />
        <NavItem icon={<Globe className="w-4 h-4" />} label="Web Services" path="/web-services" active={path.startsWith('/web-services')} collapsed={collapsed} />
        <NavItem icon={<Lock className="w-4 h-4" />} label="Private Services" path="/private-services" active={path.startsWith('/private-services')} collapsed={collapsed} />
        <NavItem icon={<Cog className="w-4 h-4" />} label="Background Workers" path="/workers" active={path.startsWith('/workers')} collapsed={collapsed} />
        <NavItem icon={<Clock className="w-4 h-4" />} label="Cron Jobs" path="/cron-jobs" active={path.startsWith('/cron-jobs')} collapsed={collapsed} />
        <NavItem icon={<Database className="w-4 h-4" />} label="PostgreSQL" path="/postgres" active={path.startsWith('/postgres')} collapsed={collapsed} />
        <NavItem icon={<Key className="w-4 h-4" />} label="Key Value" path="/keyvalue" active={path.startsWith('/keyvalue')} collapsed={collapsed} />

        <SectionLabel label="Networking" collapsed={collapsed} />
        <NavItem icon={<Globe2 className="w-4 h-4" />} label="Domains" path="/domains" active={path.startsWith('/domains')} collapsed={collapsed} />

        {isOps && (
          <>
            <SectionLabel label="Ops" collapsed={collapsed} />
            <NavItem icon={<ShieldCheck className="w-4 h-4" />} label="Overview" path="/ops" active={path === '/ops'} collapsed={collapsed} />
            <NavItem icon={<Users className="w-4 h-4" />} label="Customers" path="/ops/customers" active={path.startsWith('/ops/customers')} collapsed={collapsed} />
            <NavItem icon={<AppWindow className="w-4 h-4" />} label="Services" path="/ops/services" active={path.startsWith('/ops/services')} collapsed={collapsed} />
            <NavItem icon={<TrendingUp className="w-4 h-4" />} label="Deployments" path="/ops/deployments" active={path.startsWith('/ops/deployments')} collapsed={collapsed} />
            <NavItem icon={<CreditCard className="w-4 h-4" />} label="Billing" path="/ops/billing" active={path.startsWith('/ops/billing')} collapsed={collapsed} />
            <NavItem icon={<LifeBuoy className="w-4 h-4" />} label="Tickets" path="/ops/tickets" active={path.startsWith('/ops/tickets')} collapsed={collapsed} />
            <NavItem icon={<Coins className="w-4 h-4" />} label="Credits" path="/ops/credits" active={path.startsWith('/ops/credits')} collapsed={collapsed} />
            <NavItem icon={<Server className="w-4 h-4" />} label="Technical" path="/ops/technical" active={path.startsWith('/ops/technical')} collapsed={collapsed} />
            <NavItem icon={<BarChart3 className="w-4 h-4" />} label="Performance" path="/ops/performance" active={path.startsWith('/ops/performance')} collapsed={collapsed} />
            <NavItem icon={<Mail className="w-4 h-4" />} label="Email" path="/ops/email" active={path.startsWith('/ops/email')} collapsed={collapsed} />
            <NavItem icon={<Settings className="w-4 h-4" />} label="Ops Settings" path="/ops/settings" active={path.startsWith('/ops/settings')} collapsed={collapsed} />
            <NavItem icon={<Siren className="w-4 h-4" />} label="Incidents" path="/incidents" active={path.startsWith('/incidents')} collapsed={collapsed} />
          </>
        )}
      </nav>

      <div className="p-2 border-t border-border-subtle space-y-0.5">
        <NavItem icon={<Settings className="w-4 h-4" />} label="Settings" path="/settings" active={path === '/settings'} collapsed={collapsed} />
        <NavItem icon={<CreditCard className="w-4 h-4" />} label="Billing" path="/billing" active={path === '/billing'} collapsed={collapsed} />
        <NavItem icon={<LifeBuoy className="w-4 h-4" />} label="Support" path="/support" active={path.startsWith('/support')} collapsed={collapsed} />
        <NavItem icon={<BookOpen className="w-4 h-4" />} label="Docs" path="/docs" active={false} collapsed={collapsed} />
        <NavItem icon={<MessageSquare className="w-4 h-4" />} label="Community" path="/community" active={false} collapsed={collapsed} />
      </div>

      <div className="p-3 border-t border-border-subtle">
        <div className={cn('flex items-center', collapsed ? 'justify-center' : 'gap-2')}>
          <div className="w-7 h-7 rounded-full bg-surface-tertiary flex items-center justify-center text-xs text-content-secondary">
            {userInitial}
          </div>
          {!collapsed && (
            <>
              <div className="flex-1 min-w-0">
                <div className="text-xs font-medium text-content-primary truncate">{userLabel}</div>
                {userSub && <div className="text-[11px] text-content-tertiary truncate">{userSub}</div>}
              </div>
              <button
                onClick={() => { auth.logout(); }}
                title="Log out"
                className="p-1 rounded-md text-content-tertiary hover:text-red-400 hover:bg-surface-tertiary transition-colors"
              >
                <LogOut className="w-3.5 h-3.5" />
              </button>
            </>
          )}
        </div>
      </div>
    </aside>
  );
}
