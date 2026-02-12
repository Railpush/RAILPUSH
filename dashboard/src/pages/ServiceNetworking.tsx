import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Plus, Trash2, CheckCircle, AlertCircle, ExternalLink, Globe2, Link } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { CopyButton } from '../components/ui/CopyButton';
import { domains as domainsApi, registeredDomains, services as servicesApi } from '../lib/api';
import { buildDefaultServiceHostname, buildDefaultServiceUrl, hostnameFromUrl } from '../lib/serviceUrl';
import type { CustomDomain, RegisteredDomain, Service } from '../types';
import { toast } from 'sonner';

export function ServiceNetworking() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [domainList, setDomainList] = useState<CustomDomain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [, setLoading] = useState(true);
  const [ownedDomains, setOwnedDomains] = useState<RegisteredDomain[]>([]);
  const [selectedOwnedDomain, setSelectedOwnedDomain] = useState('');
  const [linking, setLinking] = useState(false);

  useEffect(() => {
    if (!serviceId) return;
    servicesApi.get(serviceId)
      .then(setService)
      .catch(() => {});

    domainsApi.list(serviceId)
      .then(setDomainList)
      .catch(() => {})
      .finally(() => setLoading(false));

    registeredDomains.list()
      .then((d) => setOwnedDomains(d.filter((dom) => dom.status === 'active')))
      .catch(() => {});
  }, [serviceId]);

  const addDomain = async () => {
    if (!serviceId || !newDomain.trim()) return;
    try {
      const d = await domainsApi.add(serviceId, newDomain);
      setDomainList([...domainList, d]);
      setNewDomain('');
      toast.success('Domain added');
    } catch {
      toast.error('Failed to add domain');
    }
  };

  const removeDomain = async (domain: string) => {
    if (!serviceId) return;
    try {
      await domainsApi.remove(serviceId, domain);
      setDomainList(domainList.filter((d) => d.domain !== domain));
      toast.success('Domain removed');
    } catch {
      toast.error('Failed to remove domain');
    }
  };

  const linkOwnedDomain = async () => {
    if (!serviceId || !selectedOwnedDomain) return;
    setLinking(true);
    try {
      const d = await domainsApi.add(serviceId, selectedOwnedDomain);
      setDomainList([...domainList, d]);
      setSelectedOwnedDomain('');
      toast.success(`${selectedOwnedDomain} linked and DNS auto-configured`);
    } catch {
      toast.error('Failed to link domain');
    } finally {
      setLinking(false);
    }
  };

  const defaultUrl = service ? buildDefaultServiceUrl(service) : '';
  const defaultHost = defaultUrl
    ? hostnameFromUrl(defaultUrl)
    : (service ? buildDefaultServiceHostname(service.name) : '');

  // Filter out already-linked domains
  const linkedNames = new Set(domainList.map((d) => d.domain));
  const availableToLink = ownedDomains.filter((d) => !linkedNames.has(d.domain_name));

  return (
    <div>
      <h1 className="text-2xl font-semibold text-content-primary mb-6">Networking</h1>

      {/* Default URL */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Default URL
        </h2>
        <Card>
          {defaultUrl ? (
            <>
              <div className="flex items-center justify-between gap-2">
                <a href={defaultUrl} target="_blank" rel="noopener noreferrer" className="text-sm text-brand hover:text-brand-hover transition-colors flex items-center gap-1 break-all">
                  {defaultUrl}
                  <ExternalLink className="w-3.5 h-3.5 shrink-0" />
                </a>
                <CopyButton text={defaultUrl} />
              </div>
              <p className="text-xs text-content-tertiary mt-2">
                This URL is generated from your service name and will auto-issue TLS when routed through your deploy domain.
              </p>
            </>
          ) : (
            <p className="text-sm text-content-secondary">
              Your working URL will appear after this service completes its first deploy.
            </p>
          )}
        </Card>
      </div>

      {/* Link RailPush Domain */}
      {availableToLink.length > 0 && (
        <div className="mb-8">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
            Link RailPush Domain
          </h2>
          <Card>
            <div className="flex items-center gap-2 mb-2">
              <Globe2 className="w-4 h-4 text-content-tertiary" />
              <p className="text-sm text-content-secondary">
                Link a domain you've registered through RailPush. DNS will be auto-configured.
              </p>
            </div>
            <div className="flex items-center gap-2">
              <select
                value={selectedOwnedDomain}
                onChange={(e) => setSelectedOwnedDomain(e.target.value)}
                className="flex-1 bg-surface-tertiary border border-border-default rounded-md px-3 py-2 text-sm text-content-primary appearance-none focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all duration-150"
              >
                <option value="">Select a domain...</option>
                {availableToLink.map((d) => (
                  <option key={d.id} value={d.domain_name}>{d.domain_name}</option>
                ))}
              </select>
              <Button onClick={linkOwnedDomain} disabled={!selectedOwnedDomain} loading={linking}>
                <Link className="w-4 h-4" />
                Link Domain
              </Button>
            </div>
          </Card>
        </div>
      )}

      {/* Custom Domains */}
      <div className="mb-8">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-content-tertiary mb-3">
          Custom Domains
        </h2>

        {domainList.length > 0 && (
          <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden mb-4">
            {domainList.map((d) => (
              <div key={d.id} className="flex items-center justify-between px-4 py-3 border-b border-border-subtle last:border-0">
                <div className="flex items-center gap-3">
                  {d.verified ? (
                    <CheckCircle className="w-4 h-4 text-status-success" />
                  ) : (
                    <AlertCircle className="w-4 h-4 text-status-warning" />
                  )}
                  <div>
                    <a href={`https://${d.domain}`} target="_blank" rel="noopener noreferrer" className="text-sm font-medium text-content-primary hover:text-brand transition-colors">
                      {d.domain}
                    </a>
                    <div className="text-xs text-content-tertiary mt-0.5">
                      {d.verified ? 'Verified' : 'Pending verification'} &middot;
                      TLS: {d.tls_provisioned ? 'Active' : 'Provisioning'}
                    </div>
                  </div>
                </div>
                <button
                  onClick={() => removeDomain(d.domain)}
                  className="p-1.5 rounded-md text-content-tertiary hover:text-status-error hover:bg-status-error-bg transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>
        )}

        <Card>
          <p className="text-sm text-content-secondary mb-3">
            Add a custom domain and point a CNAME record to <code className="text-xs font-mono bg-surface-tertiary px-1.5 py-0.5 rounded">{defaultHost || 'your-service-domain'}</code>
          </p>
          <div className="flex items-center gap-2">
            <Input
              placeholder="example.com"
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
            />
            <Button onClick={addDomain} disabled={!newDomain.trim()}>
              <Plus className="w-4 h-4" />
              Add Domain
            </Button>
          </div>
        </Card>
      </div>
    </div>
  );
}
