import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import { SidebarProvider, useSidebar } from '../../lib/sidebar';

function LayoutInner() {
  const { collapsed } = useSidebar();
  const year = new Date().getFullYear();

  return (
    <div className="min-h-screen bg-surface-primary relative">
      <div className="pointer-events-none fixed inset-0 bg-[radial-gradient(circle_at_20%_20%,rgba(37,99,235,0.08),transparent_25%),radial-gradient(circle_at_80%_0%,rgba(14,165,233,0.1),transparent_25%)]" />
      <Sidebar />
      <div className={`relative transition-all duration-200 ${collapsed ? 'ml-[68px]' : 'ml-[248px]'}`}>
        <TopBar />
        <main className="px-4 sm:px-6 lg:px-10 py-6 max-w-[1320px] mx-auto">
          <div className="page-shell p-4 sm:p-6 flex flex-col">
            <div className="flex-1">
              <Outlet />
            </div>
            <footer className="mt-10 pt-5 border-t border-border-default/60 text-xs text-content-tertiary text-center">
              RailPush {year}
            </footer>
          </div>
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
