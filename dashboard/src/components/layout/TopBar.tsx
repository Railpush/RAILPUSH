import { useNavigate } from 'react-router-dom';
import { Plus, Bell, ChevronDown, Globe, FileText, Lock, Cog, Clock, Database, Key, Layers } from 'lucide-react';
import { Dropdown } from '../ui/Dropdown';

export function TopBar() {
  const navigate = useNavigate();

  const ico = (I: any, c: string) => <I size={14} style={{ color: c }} />;

  const newItems = [
    { sectionLabel: 'Services', label: '', onClick: () => {} },
    { label: 'Web Service', icon: ico(Globe, '#4351E8'), onClick: () => navigate('/new/web') },
    { label: 'Static Site', icon: ico(FileText, '#59FFA4'), onClick: () => navigate('/new/static') },
    { label: 'Private Service', icon: ico(Lock, '#8A05FF'), onClick: () => navigate('/new/pserv') },
    { label: 'Worker', icon: ico(Cog, '#FFBB33'), onClick: () => navigate('/new/worker') },
    { label: 'Cron Job', icon: ico(Clock, '#38BDF8'), onClick: () => navigate('/new/cron') },
    { sectionLabel: 'Datastores', label: '', onClick: () => {} },
    { label: 'PostgreSQL', icon: ico(Database, '#336791'), onClick: () => navigate('/new/postgres') },
    { label: 'Key Value', icon: ico(Key, '#DC382D'), onClick: () => navigate('/new/keyvalue') },
    { divider: true, label: '', onClick: () => {} },
    { label: 'Blueprint', icon: ico(Layers, '#8A05FF'), onClick: () => navigate('/new/blueprint') },
  ];

  return (
    <header className="h-[50px] border-b border-border-default flex items-center justify-end px-4 gap-3 bg-surface-primary/80 backdrop-blur-sm sticky top-0 z-30">
      <Dropdown
        trigger={
          <button className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-brand text-white rounded-md text-sm font-medium hover:bg-brand-hover transition-colors cursor-pointer">
            <Plus className="w-4 h-4" />
            New
            <ChevronDown className="w-3.5 h-3.5" />
          </button>
        }
        items={newItems}
        align="right"
      />

      <button className="relative p-2 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors">
        <Bell className="w-4.5 h-4.5" />
        <span className="absolute top-1.5 right-1.5 w-2 h-2 bg-status-error rounded-full" />
      </button>

      <button className="flex items-center gap-2 p-1 rounded-md hover:bg-surface-tertiary transition-colors">
        <div className="w-7 h-7 rounded-full bg-brand/20 flex items-center justify-center text-xs font-medium text-brand">
          U
        </div>
      </button>
    </header>
  );
}
