import { Suspense, lazy, useCallback, useEffect, useMemo, useState, type ReactElement } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useParams } from 'react-router-dom';
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
import { BillingPlans } from './pages/BillingPlans';
import { Domains } from './pages/Domains';
import { DomainSearch } from './pages/DomainSearch';
import { DomainDetail } from './pages/DomainDetail';
import { KeyValueDetail } from './pages/KeyValueDetail';
import { Projects } from './pages/Projects';
import { ProjectDetail } from './pages/ProjectDetail';
import { Settings } from './pages/Settings';
import { Community } from './pages/Community';
import { auth } from './lib/api';
import { ThemeProvider } from './lib/theme';
import { SessionProvider } from './lib/session';
import type { User } from './types';
import { SupportPage } from './pages/Support';
import { SupportTicketDetailPage } from './pages/SupportTicketDetail';
import { VerifyEmail } from './pages/VerifyEmail';

const OpsRoutes = lazy(() => import('./ops/OpsRoutes'));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30000,
    },
  },
});

function IncidentsRedirect() {
  const { incidentId } = useParams<{ incidentId?: string }>();
  const target = incidentId ? `/ops/incidents/${encodeURIComponent(incidentId)}` : '/ops/incidents';
  return <Navigate to={target} replace />;
}

function App() {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean | null>(null);
  const [session, setSession] = useState<{ user: User; workspace?: { id: string; name: string } } | null>(null);

  useEffect(() => {
    let mounted = true;
    auth
      .getUser()
      .then((data) => {
        if (!mounted) return;
        setSession(data);
        setIsAuthenticated(true);
      })
      .catch(() => {
        if (!mounted) return;
        setSession(null);
        setIsAuthenticated(false);
      });
    return () => {
      mounted = false;
    };
  }, []);

  const refreshSession = useCallback(async () => {
    const data = await auth.getUser();
    setSession(data);
  }, []);

  const sessionValue = useMemo(() => {
    const user = session?.user || null;
    const workspace = session?.workspace || null;
    const role = (user?.role || '').toLowerCase().trim();
    return {
      session,
      user,
      workspace,
      isOps: role === 'admin' || role === 'ops',
      refresh: refreshSession,
    };
  }, [refreshSession, session]);

  const requireOps = useCallback(
    (element: ReactElement) => (sessionValue.isOps ? element : <Navigate to="/" replace />),
    [sessionValue.isOps]
  );

  if (isAuthenticated === null) {
    return (
      <ThemeProvider>
        <QueryClientProvider client={queryClient}>
          <div className="min-h-screen bg-surface-primary flex items-center justify-center text-content-secondary text-sm">
            Loading...
          </div>
          <ToastProvider />
        </QueryClientProvider>
      </ThemeProvider>
    );
  }

  if (!isAuthenticated) {
    return (
      <ThemeProvider>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <Routes>
              <Route path="/" element={<Landing />} />
              <Route path="/login" element={<Login />} />
              <Route path="/verify" element={<VerifyEmail />} />
              <Route path="/docs" element={<Docs />} />
              <Route path="/privacy" element={<Privacy />} />
              <Route path="*" element={<Navigate to="/" />} />
            </Routes>
          </BrowserRouter>
          <ToastProvider />
        </QueryClientProvider>
      </ThemeProvider>
    );
  }

  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <SessionProvider value={sessionValue}>
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
                <Route path="/billing/plans" element={<BillingPlans />} />

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

                {/* Ops (lazy-loaded route tree) */}
                <Route
                  path="/ops/*"
                  element={requireOps(
                    <Suspense
                      fallback={
                        <div className="min-h-[40vh] flex items-center justify-center text-sm text-content-tertiary">
                          Loading ops...
                        </div>
                      }
                    >
                      <OpsRoutes />
                    </Suspense>
                  )}
                />
                <Route path="/incidents" element={requireOps(<Navigate to="/ops/incidents" replace />)} />
                <Route path="/incidents/:incidentId" element={requireOps(<IncidentsRedirect />)} />

                {/* Support */}
                <Route path="/support" element={<SupportPage />} />
                <Route path="/support/:ticketId" element={<SupportTicketDetailPage />} />

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
        </SessionProvider>
        <ToastProvider />
      </QueryClientProvider>
    </ThemeProvider>
  );
}

export default App;
