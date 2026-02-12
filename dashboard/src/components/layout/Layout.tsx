import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { SidebarProvider, useSidebar } from '../../lib/sidebar';

function LayoutInner() {
  const { collapsed } = useSidebar();

  return (
    <div className="min-h-screen bg-surface-primary">
      <Sidebar />
      <div className={`transition-all duration-200 ${collapsed ? 'ml-[64px]' : 'ml-[240px]'}`}>
        <TopBar />
        <main className="p-6 max-w-[1200px] mx-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export function Layout() {
  return (
    <SidebarProvider>
      <LayoutInner />
    </SidebarProvider>
  );
}
