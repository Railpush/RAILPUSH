import { useNavigate } from 'react-router-dom';
import { BookOpen, LifeBuoy, Users } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';

export function Community() {
  const navigate = useNavigate();

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Community</h1>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Documentation</div>
          <div className="flex items-center gap-2 text-content-primary mb-3">
            <BookOpen className="w-4 h-4" />
            <span className="text-sm">Platform guides, API examples, and blueprint reference.</span>
          </div>
          <Button variant="secondary" onClick={() => navigate('/docs')}>Open Docs</Button>
        </Card>

        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Platform Updates</div>
          <div className="flex items-center gap-2 text-content-primary mb-3">
            <Users className="w-4 h-4" />
            <span className="text-sm">Follow new features from dashboard release notes.</span>
          </div>
          <Button variant="secondary" onClick={() => navigate('/')}>Open Dashboard</Button>
        </Card>

        <Card>
          <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">Support</div>
          <div className="flex items-center gap-2 text-content-primary mb-3">
            <LifeBuoy className="w-4 h-4" />
            <span className="text-sm">Troubleshoot deploy, billing, and networking issues.</span>
          </div>
          <Button variant="secondary" onClick={() => navigate('/settings')}>Open Settings</Button>
        </Card>
      </div>
    </div>
  );
}
