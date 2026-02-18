import { Navigate, Route, Routes } from 'react-router-dom';
import { OpsOverviewPage } from '../pages/OpsOverview';
import { OpsCustomersPage } from '../pages/OpsCustomers';
import { OpsServicesPage } from '../pages/OpsServices';
import { OpsServiceLogsPage } from '../pages/OpsServiceLogs';
import { OpsDeploymentsPage } from '../pages/OpsDeployments';
import { OpsEmailOutboxPage } from '../pages/OpsEmailOutbox';
import { OpsBillingPage } from '../pages/OpsBilling';
import { OpsBillingCustomerPage } from '../pages/OpsBillingCustomer';
import { OpsTicketsPage } from '../pages/OpsTickets';
import { OpsTicketDetailPage } from '../pages/OpsTicketDetail';
import { OpsCreditsPage } from '../pages/OpsCredits';
import { OpsCreditsWorkspacePage } from '../pages/OpsCreditsWorkspace';
import { OpsTechnicalPage } from '../pages/OpsTechnical';
import { OpsClusterPage } from '../pages/OpsCluster';
import { OpsPerformancePage } from '../pages/OpsPerformance';
import { OpsSettingsPage } from '../pages/OpsSettings';
import { OpsDatastoresPage } from '../pages/OpsDatastores';
import { OpsAuditLogsPage } from '../pages/OpsAuditLogs';
import { Incidents } from '../pages/Incidents';
import { IncidentDetailPage } from '../pages/IncidentDetail';

export default function OpsRoutes() {
  return (
    <Routes>
      <Route path="/" element={<OpsOverviewPage />} />
      <Route path="customers" element={<OpsCustomersPage />} />
      <Route path="services" element={<OpsServicesPage />} />
      <Route path="services/:serviceId/logs" element={<OpsServiceLogsPage />} />
      <Route path="incidents" element={<Incidents />} />
      <Route path="incidents/:incidentId" element={<IncidentDetailPage />} />
      <Route path="deployments" element={<OpsDeploymentsPage />} />
      <Route path="email" element={<OpsEmailOutboxPage />} />
      <Route path="billing" element={<OpsBillingPage />} />
      <Route path="billing/:customerId" element={<OpsBillingCustomerPage />} />
      <Route path="tickets" element={<OpsTicketsPage />} />
      <Route path="tickets/:ticketId" element={<OpsTicketDetailPage />} />
      <Route path="credits" element={<OpsCreditsPage />} />
      <Route path="credits/:workspaceId" element={<OpsCreditsWorkspacePage />} />
      <Route path="technical" element={<OpsTechnicalPage />} />
      <Route path="cluster" element={<OpsClusterPage />} />
      <Route path="performance" element={<OpsPerformancePage />} />
      <Route path="settings" element={<OpsSettingsPage />} />

      {/* Must-have before launch */}
      <Route path="datastores" element={<OpsDatastoresPage />} />
      <Route path="audit" element={<OpsAuditLogsPage />} />

      <Route path="*" element={<Navigate to="/ops" replace />} />
    </Routes>
  );
}
