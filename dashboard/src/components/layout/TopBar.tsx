import { useNavigate } from 'react-router-dom';
import { Plus, ChevronDown, Globe, FileText, Lock, Cog, Clock, Database, Key, Layers, Moon, Sun, LogOut, BookOpen, Search } from 'lucide-react';
import { Dropdown } from '../ui/Dropdown';
import { useTheme } from '../../lib/theme';
import { useSession } from '../../lib/session';
import { auth } from '../../lib/api';
import { Button } from '../ui/Button';

export function TopBar() {
  const navigate = useNavigate();
  const { theme, toggleTheme } = useTheme();
  const { user } = useSession();

  const newItems = [
    { sectionLabel: 'Services', label: '', onClick: () => { } },
    { label: 'Web Service', icon: <Globe size={14} />, onClick: () => navigate('/new/web') },
    { label: 'Static Site', icon: <FileText size={14} />, onClick: () => navigate('/new/static') },
    { label: 'Private Service', icon: <Lock size={14} />, onClick: () => navigate('/new/pserv') },
    { label: 'Worker', icon: <Cog size={14} />, onClick: () => navigate('/new/worker') },
    { label: 'Cron Job', icon: <Clock size={14} />, onClick: () => navigate('/new/cron') },
    { sectionLabel: 'Datastores', label: '', onClick: () => { } },
    { label: 'PostgreSQL', icon: <Database size={14} />, onClick: () => navigate('/new/postgres') },
    { label: 'Key Value', icon: <Key size={14} />, onClick: () => navigate('/new/keyvalue') },
    { divider: true, label: '', onClick: () => { } },
    { label: 'Blueprint', icon: <Layers size={14} />, onClick: () => navigate('/new/blueprint') },
  ];

  const userLabel = user?.username || user?.email || 'User';
  const userInitial = (userLabel || 'U').slice(0, 1).toUpperCase();

  const userItems = [
    { label: 'Docs', icon: <BookOpen size={14} />, onClick: () => navigate('/docs') },
    { divider: true, label: '', onClick: () => { } },
    { label: theme === 'dark' ? 'Light mode' : 'Dark mode', icon: theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />, onClick: toggleTheme },
    { divider: true, label: '', onClick: () => { } },
    { label: 'Log out', icon: <LogOut size={14} />, danger: true, onClick: () => { auth.logout(); } },
  ];

  return (
    <header className="app-topbar h-[60px] border-b border-border-default/50 flex items-center justify-between px-6 bg-surface-secondary/80 backdrop-blur-xl sticky top-0 z-30 transition-all duration-300">

      {/* Search Trigger Mockup */}
      <div className="flex-1 max-w-md hidden md:block">
        <button className="w-full flex items-center gap-2 px-3 py-1.5 rounded-lg border border-border-default bg-surface-tertiary/50 hover:bg-surface-tertiary/80 hover:border-border-hover transition-all text-sm text-content-tertiary group">
          <Search className="w-4 h-4 group-hover:text-brand transition-colors" />
          <span className="opacity-70">Search projects, services, docs...</span>
          <span className="ml-auto text-xs bg-surface-elevated/50 px-1.5 py-0.5 rounded border border-border-subtle">⌘K</span>
        </button>
      </div>

      <div className="flex items-center gap-4 ml-auto">
        <Dropdown
          trigger={
            <Button variant="primary" size="sm" className="pl-3 pr-2">
              <Plus className="w-4 h-4 mr-1.5" />
              New
              <div className="w-[1px] h-4 bg-white/20 mx-1.5" />
              <ChevronDown className="w-3.5 h-3.5 opacity-80" />
            </Button>
          }
          items={newItems}
          align="right"
        />

        <div className="h-6 w-[1px] bg-border-default mx-1" />

        <Dropdown
          trigger={
            <button className="flex items-center gap-2.5 pl-1 pr-2 py-1 rounded-full hover:bg-surface-tertiary transition-colors border border-transparent group">
              <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-indigo-500 to-purple-500 flex items-center justify-center text-xs font-bold text-white shadow-md ring-2 ring-transparent group-hover:ring-brand/50 transition-all">
                {userInitial}
              </div>
              <ChevronDown className="w-4 h-4 text-content-tertiary group-hover:text-content-primary transition-colors" />
            </button>
          }
          items={userItems}
          align="right"
        />
      </div>
    </header>
  );
}
