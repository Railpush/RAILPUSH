import { useNavigate } from 'react-router-dom';
import { CreditCard, FileText, Shield } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { auth } from '../lib/api';

export function Settings() {
  const navigate = useNavigate();

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Settings</h1>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Billing & Plans</div>
          <div className="flex items-center gap-2 text-content-primary mb-3">
            <CreditCard className="w-4 h-4" />
            <span className="text-sm">Manage payment method, plans, and invoices.</span>
          </div>
          <Button variant="secondary" onClick={() => navigate('/billing')}>Open Billing</Button>
        </Card>

        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Platform</div>
          <div className="flex items-center gap-2 text-content-primary mb-3">
            <FileText className="w-4 h-4" />
            <span className="text-sm">Read docs and deployment behavior details.</span>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="secondary" onClick={() => navigate('/docs')}>Docs</Button>
            <Button variant="secondary" onClick={() => navigate('/privacy')}>Privacy</Button>
          </div>
        </Card>
      </div>

      <Card className="mt-4">
        <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Session</div>
        <div className="flex items-center gap-2 text-content-primary mb-3">
          <Shield className="w-4 h-4" />
          <span className="text-sm">Sign out from this browser session.</span>
        </div>
        <Button
          variant="danger"
          onClick={() => { auth.logout(); }}
        >
          Log Out
        </Button>
      </Card>
    </div>
  );
}
