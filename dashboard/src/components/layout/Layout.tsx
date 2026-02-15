import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { SidebarProvider, useSidebar } from '../../lib/sidebar';

function LayoutInner() {
  const { collapsed } = useSidebar();
  const year = new Date().getFullYear();

  return (
    <div className="min-h-screen bg-surface-primary">
      <Sidebar />
      <div className={`transition-all duration-200 flex flex-col min-h-screen ${collapsed ? 'ml-[64px]' : 'ml-[240px]'}`}>
        <TopBar />
        <main className="flex-1 px-4 sm:px-6 lg:px-8 py-6 max-w-[1280px] mx-auto w-full">
          <div className="page-shell p-4 sm:p-6">
            <Outlet />
          </div>
        </main>
        <footer className="py-5 text-xs text-content-tertiary text-center">
          RailPush {year}
        </footer>
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
