import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Layout } from './components/layout/Layout';
import { ToastProvider } from './components/ui/Toast';
import { Dashboard } from './pages/Dashboard';
import { ServiceDetail } from './pages/ServiceDetail';
import { ServiceEvents } from './pages/ServiceEvents';
import { ServiceLogs } from './pages/ServiceLogs';
import { ServiceEnvironment } from './pages/ServiceEnvironment';
import { ServiceMetrics } from './pages/ServiceMetrics';
import { ServiceSettings } from './pages/ServiceSettings';
import { ServiceNetworking } from './pages/ServiceNetworking';
import { ServiceScaling } from './pages/ServiceScaling';
import { ServiceDisks } from './pages/ServiceDisks';
import { CreateService } from './pages/CreateService';
import { DatabaseDetail } from './pages/DatabaseDetail';
import { Blueprints } from './pages/Blueprints';
import { CreateBlueprint } from './pages/CreateBlueprint';
import { BlueprintDetail } from './pages/BlueprintDetail';
import { EnvGroups } from './pages/EnvGroups';
import { Login } from './pages/Login';
import { Landing } from './pages/Landing';
import { Docs } from './pages/Docs';
import { Privacy } from './pages/Privacy';
import { Billing } from './pages/Billing';
import { Domains } from './pages/Domains';
import { DomainSearch } from './pages/DomainSearch';
import { DomainDetail } from './pages/DomainDetail';
import { KeyValueDetail } from './pages/KeyValueDetail';
import { Projects } from './pages/Projects';
import { ProjectDetail } from './pages/ProjectDetail';
import { Settings } from './pages/Settings';
import { Community } from './pages/Community';
import { auth } from './lib/api';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30000,
    },
  },
});

function App() {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean | null>(null);

  useEffect(() => {
    let mounted = true;
    auth
      .getUser()
      .then(() => {
        if (mounted) setIsAuthenticated(true);
      })
      .catch(() => {
        if (mounted) setIsAuthenticated(false);
      });
    return () => {
      mounted = false;
    };
  }, []);

  if (isAuthenticated === null) {
    return (
      <QueryClientProvider client={queryClient}>
        <div className="min-h-screen bg-surface-primary flex items-center justify-center text-content-secondary text-sm">
          Loading...
        </div>
        <ToastProvider />
      </QueryClientProvider>
    );
  }

  if (!isAuthenticated) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Landing />} />
            <Route path="/login" element={<Login />} />
            <Route path="/docs" element={<Docs />} />
            <Route path="/privacy" element={<Privacy />} />
            <Route path="*" element={<Navigate to="/" />} />
          </Routes>
        </BrowserRouter>
        <ToastProvider />
      </QueryClientProvider>
    );
  }

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Navigate to="/" />} />
          <Route path="/docs" element={<Docs />} />
          <Route path="/privacy" element={<Privacy />} />
          <Route element={<Layout />}>
            {/* Dashboard */}
            <Route path="/" element={<Dashboard />} />

            {/* Create */}
            <Route path="/new" element={<CreateService />} />
            <Route path="/new/:type" element={<CreateService />} />

            {/* Service detail routes */}
            <Route path="/services/:serviceId" element={<ServiceDetail />} />
            <Route path="/services/:serviceId/events" element={<ServiceEvents />} />
            <Route path="/services/:serviceId/logs" element={<ServiceLogs />} />
            <Route path="/services/:serviceId/environment" element={<ServiceEnvironment />} />
            <Route path="/services/:serviceId/metrics" element={<ServiceMetrics />} />
            <Route path="/services/:serviceId/settings" element={<ServiceSettings />} />
            <Route path="/services/:serviceId/networking" element={<ServiceNetworking />} />
            <Route path="/services/:serviceId/scaling" element={<ServiceScaling />} />
            <Route path="/services/:serviceId/disks" element={<ServiceDisks />} />

            {/* Domains */}
            <Route path="/domains" element={<Domains />} />
            <Route path="/domains/search" element={<DomainSearch />} />
            <Route path="/domains/:domainId" element={<DomainDetail />} />
            <Route path="/domains/:domainId/dns" element={<DomainDetail />} />
            <Route path="/domains/:domainId/settings" element={<DomainDetail />} />

            {/* Database */}
            <Route path="/databases/:dbId" element={<DatabaseDetail />} />
            <Route path="/databases/:dbId/*" element={<DatabaseDetail />} />

            {/* Billing */}
            <Route path="/billing" element={<Billing />} />

            {/* Blueprints */}
            <Route path="/blueprints" element={<Blueprints />} />
            <Route path="/new/blueprint" element={<CreateBlueprint />} />
            <Route path="/blueprints/:blueprintId" element={<BlueprintDetail />} />

            {/* Env Groups */}
            <Route path="/env-groups" element={<EnvGroups />} />

            {/* Projects / Settings / Community */}
            <Route path="/projects" element={<Projects />} />
            <Route path="/projects/:projectId" element={<ProjectDetail />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/community" element={<Community />} />

            {/* Resource type filtered views */}
            <Route path="/web-services" element={<Dashboard scope="web-services" />} />
            <Route path="/static-sites" element={<Dashboard scope="static-sites" />} />
            <Route path="/private-services" element={<Dashboard scope="private-services" />} />
            <Route path="/workers" element={<Dashboard scope="workers" />} />
            <Route path="/cron-jobs" element={<Dashboard scope="cron-jobs" />} />
            <Route path="/postgres" element={<Dashboard scope="postgres" />} />
            <Route path="/keyvalue" element={<Dashboard scope="keyvalue" />} />
            <Route path="/keyvalue/:kvId" element={<KeyValueDetail />} />

            {/* Catch-all */}
            <Route path="*" element={<Navigate to="/" />} />
          </Route>
        </Routes>
      </BrowserRouter>
      <ToastProvider />
    </QueryClientProvider>
  );
}

export default App;
