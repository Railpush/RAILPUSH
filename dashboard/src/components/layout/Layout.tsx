import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { SidebarProvider, useSidebar } from '../../lib/sidebar';

function LayoutInner() {
  const { collapsed } = useSidebar();
  const year = new Date().getFullYear();

  return (
    <div className="min-h-screen">
      <Sidebar />
      <div className={`transition-all duration-300 ease-[cubic-bezier(0.2,0,0,1)] flex flex-col min-h-screen ${collapsed ? 'ml-[68px]' : 'ml-[260px]'}`}>
        <TopBar />
        <main className="flex-1 px-4 sm:px-6 lg:px-8 py-8 w-full mx-auto max-w-[1400px] animate-fade-in">
          <Outlet />
        </main>
        <footer className="py-6 text-xs text-content-tertiary text-center opacity-60">
          RailPush {year} &copy; All rights reserved
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
