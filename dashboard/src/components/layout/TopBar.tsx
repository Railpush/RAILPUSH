import { useNavigate } from 'react-router-dom';
import { Plus, ChevronDown, Globe, FileText, Lock, Cog, Clock, Database, Key, Layers, Moon, Sun, LogOut, BookOpen } from 'lucide-react';
import { Dropdown } from '../ui/Dropdown';
import { useTheme } from '../../lib/theme';
import { useSession } from '../../lib/session';
import { auth } from '../../lib/api';

export function TopBar() {
  const navigate = useNavigate();
  const { theme, toggleTheme } = useTheme();
  const { user } = useSession();

  const newItems = [
    { sectionLabel: 'Services', label: '', onClick: () => {} },
    { label: 'Web Service', icon: <Globe size={14} />, onClick: () => navigate('/new/web') },
    { label: 'Static Site', icon: <FileText size={14} />, onClick: () => navigate('/new/static') },
    { label: 'Private Service', icon: <Lock size={14} />, onClick: () => navigate('/new/pserv') },
    { label: 'Worker', icon: <Cog size={14} />, onClick: () => navigate('/new/worker') },
    { label: 'Cron Job', icon: <Clock size={14} />, onClick: () => navigate('/new/cron') },
    { sectionLabel: 'Datastores', label: '', onClick: () => {} },
    { label: 'PostgreSQL', icon: <Database size={14} />, onClick: () => navigate('/new/postgres') },
    { label: 'Key Value', icon: <Key size={14} />, onClick: () => navigate('/new/keyvalue') },
    { divider: true, label: '', onClick: () => {} },
    { label: 'Blueprint', icon: <Layers size={14} />, onClick: () => navigate('/new/blueprint') },
  ];

  const userLabel = user?.username || user?.email || 'User';
  const userInitial = (userLabel || 'U').slice(0, 1).toUpperCase();

  const userItems = [
    { label: 'Docs', icon: <BookOpen size={14} />, onClick: () => navigate('/docs') },
    { divider: true, label: '', onClick: () => {} },
    { label: theme === 'dark' ? 'Light mode' : 'Dark mode', icon: theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />, onClick: toggleTheme },
    { divider: true, label: '', onClick: () => {} },
    { label: 'Log out', icon: <LogOut size={14} />, danger: true, onClick: () => { auth.logout(); } },
  ];

  return (
    <header className="app-topbar h-[52px] border-b border-border-default flex items-center justify-end px-4 lg:px-6 gap-3 bg-surface-secondary sticky top-0 z-30">
      <Dropdown
        trigger={
          <button className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md border border-border-default bg-surface-secondary hover:bg-surface-tertiary text-sm font-medium transition-colors cursor-pointer">
            <Plus className="w-4 h-4 text-content-secondary" />
            New
            <ChevronDown className="w-3.5 h-3.5 text-content-tertiary" />
          </button>
        }
        items={newItems}
        align="right"
      />

      <Dropdown
        trigger={
          <button className="flex items-center gap-2 px-2 py-1 rounded-md hover:bg-surface-tertiary transition-colors border border-transparent">
            <div className="w-7 h-7 rounded-full bg-surface-tertiary border border-border-subtle flex items-center justify-center text-xs font-semibold text-content-secondary">
              {userInitial}
            </div>
            <ChevronDown className="w-4 h-4 text-content-tertiary" />
          </button>
        }
        items={userItems}
        align="right"
      />
    </header>
  );
}
