import { useState, useEffect, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { Plus, Trash2, CheckCircle, Globe2, Link, ArrowUpDown, MoreHorizontal, Shield, ShieldCheck, Clock, ArrowRight } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { CopyButton } from '../components/ui/CopyButton';
import { domains as domainsApi, registeredDomains, services as servicesApi, rewriteRules as rewriteRulesApi } from '../lib/api';
import { buildDefaultServiceHostname, buildDefaultServiceUrl, hostnameFromUrl } from '../lib/serviceUrl';
import type { CustomDomain, RegisteredDomain, Service, RewriteRule } from '../types';
import { toast } from 'sonner';

export function ServiceNetworking() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [domainList, setDomainList] = useState<CustomDomain[]>([]);
  const [newDomain, setNewDomain] = useState('');
  const [redirectTarget, setRedirectTarget] = useState('');
  const [showRedirect, setShowRedirect] = useState(false);
  const [, setLoading] = useState(true);
  const [ownedDomains, setOwnedDomains] = useState<RegisteredDomain[]>([]);
  const [selectedOwnedDomain, setSelectedOwnedDomain] = useState('');
  const [linking, setLinking] = useState(false);
  const [adding, setAdding] = useState(false);
  const [sortAsc, setSortAsc] = useState(true);
  const [menuOpen, setMenuOpen] = useState<string | null>(null);
  const [renderSubdomainEnabled, setRenderSubdomainEnabled] = useState(true);
  const [rules, setRules] = useState<RewriteRule[]>([]);
  const [workspaceServices, setWorkspaceServices] = useState<Service[]>([]);
  const [newRule, setNewRule] = useState({ source_path: '', dest_service_id: '', dest_path: '' });
  const [addingRule, setAddingRule] = useState(false);

  const refreshDomains = useCallback(() => {
    if (!serviceId) return;
    domainsApi.list(serviceId)
      .then(setDomainList)
      .catch(() => {});
  }, [serviceId]);

  const refreshRules = useCallback(() => {
    if (!serviceId) return;
    rewriteRulesApi.list(serviceId)
      .then(setRules)
      .catch(() => {});
  }, [serviceId]);

  useEffect(() => {
    if (!serviceId) return;
    servicesApi.get(serviceId)
      .then((s) => {
        setService(s);
        // Load workspace services for rewrite rule destination picker.
        servicesApi.list()
          .then((all) => setWorkspaceServices(all.filter((svc: Service) => svc.id !== serviceId && (svc.type === 'web' || svc.type === 'worker'))))
          .catch(() => {});
      })
      .catch(() => {});
    refreshDomains();
    refreshRules();
    registeredDomains.list()
      .then((d) => setOwnedDomains(d.filter((dom) => dom.status === 'active')))
      .catch(() => {});
    setLoading(false);
  }, [serviceId, refreshDomains, refreshRules]);

  // Auto-refresh domain status every 15s while any domain is pending
  useEffect(() => {
    const hasPending = domainList.some(d => !d.verified || !d.tls_provisioned);
    if (!hasPending) return;
    const interval = setInterval(refreshDomains, 15000);
    return () => clearInterval(interval);
  }, [domainList, refreshDomains]);

  const addDomain = async () => {
    if (!serviceId || !newDomain.trim()) return;
    setAdding(true);
    try {
      const d = await domainsApi.add(serviceId, newDomain, showRedirect ? redirectTarget || undefined : undefined);
      setDomainList([...domainList, d]);
      setNewDomain('');
      setRedirectTarget('');
      setShowRedirect(false);
      toast.success('Domain added');
      // Refresh after a short delay so cert-manager status can update
      setTimeout(refreshDomains, 3000);
    } catch {
      toast.error('Failed to add domain');
    } finally {
      setAdding(false);
    }
  };

  const removeDomain = async (domain: string) => {
    if (!serviceId) return;
    try {
      await domainsApi.remove(serviceId, domain);
      setDomainList(domainList.filter((d) => d.domain !== domain));
      setMenuOpen(null);
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

  // Sort domains
  const sortedDomains = [...domainList].sort((a, b) => {
    const cmp = a.domain.localeCompare(b.domain);
    return sortAsc ? cmp : -cmp;
  });

  return (
    <div>
      <h1 className="text-2xl font-semibold text-content-primary mb-6">Networking</h1>

      {/* Custom Domains — Render-style table */}
      <div className="mb-8">
        <Card className="!p-0 overflow-hidden">
          <div className="px-6 pt-6 pb-4">
            <h2 className="text-lg font-semibold text-content-primary">Custom Domains</h2>
            <p className="text-sm text-content-secondary mt-1">
              You can point <a href="#" className="text-brand hover:text-brand-hover underline">custom domains</a> you own to this service.
            </p>
          </div>

          {sortedDomains.length > 0 && (
            <div className="border-t border-border-subtle">
              {/* Table header */}
              <div className="grid grid-cols-[1fr_160px_160px_48px] px-6 py-3 border-b border-border-subtle">
                <button
                  className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-content-tertiary hover:text-content-secondary transition-colors"
                  onClick={() => setSortAsc(!sortAsc)}
                >
                  Name
                  <span className="inline-flex items-center justify-center w-5 h-5 rounded bg-surface-tertiary text-[10px] font-bold">
                    {domainList.length}
                  </span>
                  <ArrowUpDown className="w-3 h-3" />
                </button>
                <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary">
                  Verified Status
                </div>
                <div className="text-xs font-semibold uppercase tracking-wider text-content-tertiary">
                  Certificate Status
                </div>
                <div />
              </div>

              {/* Table rows */}
              {sortedDomains.map((d) => (
                <div
                  key={d.id}
                  className="grid grid-cols-[1fr_160px_160px_48px] items-center px-6 py-3.5 border-b border-border-subtle last:border-b-0 hover:bg-surface-secondary/50 transition-colors"
                >
                  {/* Domain name + redirect badge */}
                  <div className="flex items-center gap-2 min-w-0">
                    <a
                      href={`https://${d.domain}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm font-medium text-brand hover:text-brand-hover transition-colors truncate"
                    >
                      {d.domain}
                    </a>
                    {d.redirect_target && (
                      <span className="shrink-0 inline-flex items-center px-2 py-0.5 rounded-md bg-surface-tertiary text-[11px] text-content-secondary font-medium">
                        redirects to {d.redirect_target}
                      </span>
                    )}
                  </div>

                  {/* Verified status */}
                  <div>
                    {d.verified ? (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-status-success-bg text-status-success text-xs font-medium">
                        <CheckCircle className="w-3.5 h-3.5" />
                        Verified
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-status-warning-bg text-status-warning text-xs font-medium">
                        <Clock className="w-3.5 h-3.5" />
                        Pending
                      </span>
                    )}
                  </div>

                  {/* Certificate status */}
                  <div>
                    {d.tls_provisioned ? (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-status-success-bg text-status-success text-xs font-medium">
                        <ShieldCheck className="w-3.5 h-3.5" />
                        Certificate Issued
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-status-warning-bg text-status-warning text-xs font-medium">
                        <Shield className="w-3.5 h-3.5" />
                        Provisioning
                      </span>
                    )}
                  </div>

                  {/* Actions menu */}
                  <div className="relative flex justify-end">
                    <button
                      onClick={() => setMenuOpen(menuOpen === d.id ? null : d.id)}
                      className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
                    >
                      <MoreHorizontal className="w-4 h-4" />
                    </button>
                    {menuOpen === d.id && (
                      <>
                        <div className="fixed inset-0 z-10" onClick={() => setMenuOpen(null)} />
                        <div className="absolute right-0 top-full mt-1 z-20 w-44 bg-surface-primary border border-border-default rounded-lg shadow-xl py-1">
                          <button
                            onClick={() => removeDomain(d.domain)}
                            className="w-full text-left px-3 py-2 text-sm text-status-error hover:bg-status-error-bg transition-colors flex items-center gap-2"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                            Remove Domain
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Add custom domain form */}
          <div className={`px-6 py-4 ${sortedDomains.length > 0 ? 'border-t border-border-subtle' : ''}`}>
            <div className="flex items-center gap-2">
              <div className="flex-1">
                <Input
                  placeholder="example.com"
                  value={newDomain}
                  onChange={(e) => setNewDomain(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && addDomain()}
                />
              </div>
              {showRedirect && (
                <div className="flex-1">
                  <Input
                    placeholder="Redirect to (e.g. www.example.com)"
                    value={redirectTarget}
                    onChange={(e) => setRedirectTarget(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && addDomain()}
                  />
                </div>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => { setShowRedirect(!showRedirect); setRedirectTarget(''); }}
                className="text-xs shrink-0"
              >
                {showRedirect ? 'Cancel Redirect' : 'Add as Redirect'}
              </Button>
              <Button onClick={addDomain} disabled={!newDomain.trim()} loading={adding} className="shrink-0">
                <Plus className="w-4 h-4" />
                Add Custom Domain
              </Button>
            </div>
            {showRedirect && (
              <p className="text-xs text-content-tertiary mt-2">
                The domain will 301-redirect all traffic to the target domain. Useful for redirecting apex to www (or vice versa).
              </p>
            )}
          </div>
        </Card>
      </div>

      {/* Render Subdomain toggle */}
      <div className="mb-8">
        <Card>
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-base font-semibold text-content-primary">Render Subdomain</h3>
              <p className="text-sm text-content-secondary mt-1">
                If enabled, your service remains reachable at its <code className="text-xs font-mono bg-surface-tertiary px-1.5 py-0.5 rounded">onrender.com</code> subdomain in addition to all custom domains. Disable to serve exclusively from custom domains.
              </p>
            </div>
            <div className="flex items-center gap-3 shrink-0">
              <button
                onClick={() => setRenderSubdomainEnabled(!renderSubdomainEnabled)}
                className={`relative w-11 h-6 rounded-full transition-colors ${renderSubdomainEnabled ? 'bg-brand' : 'bg-surface-tertiary'}`}
              >
                <div className={`absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform ${renderSubdomainEnabled ? 'left-[22px]' : 'left-0.5'}`} />
              </button>
              <span className="text-sm font-medium text-content-primary">
                {renderSubdomainEnabled ? 'Enabled' : 'Disabled'}
              </span>
            </div>
          </div>
          {renderSubdomainEnabled && defaultUrl && (
            <div className="mt-3 pt-3 border-t border-border-subtle">
              <p className="text-sm text-content-secondary">
                Your service <strong>is</strong> reachable at{' '}
                <a href={defaultUrl} target="_blank" rel="noopener noreferrer" className="text-brand hover:text-brand-hover">
                  {defaultUrl}
                </a>.
              </p>
            </div>
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

      {/* DNS Instructions */}
      <div className="mb-8">
        <Card>
          <h3 className="text-sm font-semibold text-content-primary mb-2">DNS Configuration</h3>
          <p className="text-sm text-content-secondary mb-3">
            Point your domain's DNS to this service using one of the following methods:
          </p>
          <div className="space-y-2 text-sm">
            <div className="flex items-start gap-3 bg-surface-tertiary rounded-lg px-4 py-3">
              <span className="text-xs font-semibold text-content-tertiary uppercase mt-0.5 w-14 shrink-0">CNAME</span>
              <div className="flex-1 min-w-0">
                <code className="text-xs font-mono text-brand break-all">{defaultHost || 'your-service.apps.railpush.com'}</code>
                <p className="text-xs text-content-tertiary mt-1">Recommended for subdomains (e.g. www, app, api)</p>
              </div>
              {defaultHost && <CopyButton text={defaultHost} />}
            </div>
            <div className="flex items-start gap-3 bg-surface-tertiary rounded-lg px-4 py-3">
              <span className="text-xs font-semibold text-content-tertiary uppercase mt-0.5 w-14 shrink-0">A Record</span>
              <div className="flex-1 min-w-0">
                <code className="text-xs font-mono text-brand">65.21.134.49</code>
                <p className="text-xs text-content-tertiary mt-1">Required for apex/root domains (e.g. example.com)</p>
              </div>
              <CopyButton text="65.21.134.49" />
            </div>
          </div>
        </Card>
      </div>

      {/* Rewrite & Proxy Rules */}
      <div className="mb-8">
        <Card className="!p-0 overflow-hidden">
          <div className="px-6 pt-6 pb-4">
            <h2 className="text-lg font-semibold text-content-primary">Rewrite & Proxy Rules</h2>
            <p className="text-sm text-content-secondary mt-1">
              Route specific URL paths to other services. For example, proxy <code className="text-xs font-mono bg-surface-tertiary px-1 py-0.5 rounded">/api/*</code> to a backend service while serving static files from this service.
            </p>
          </div>

          {rules.length > 0 && (
            <div className="border-t border-border-subtle">
              {rules.map((rule) => (
                <div
                  key={rule.id}
                  className="flex items-center gap-3 px-6 py-3.5 border-b border-border-subtle last:border-b-0 hover:bg-surface-secondary/50 transition-colors"
                >
                  <code className="text-sm font-mono text-brand bg-surface-tertiary px-2 py-1 rounded">{rule.source_path}*</code>
                  <ArrowRight className="w-4 h-4 text-content-tertiary shrink-0" />
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-content-primary">{rule.dest_service_name || rule.dest_service_id}</span>
                    <code className="text-xs font-mono text-content-tertiary bg-surface-tertiary px-1.5 py-0.5 rounded">{rule.dest_path}*</code>
                  </div>
                  <span className="ml-auto inline-flex items-center px-2 py-0.5 rounded-md bg-brand/10 text-brand text-[11px] font-medium uppercase">
                    {rule.rule_type}
                  </span>
                  <button
                    onClick={async () => {
                      if (!serviceId) return;
                      try {
                        await rewriteRulesApi.remove(serviceId, rule.id);
                        setRules(rules.filter((r) => r.id !== rule.id));
                        toast.success('Rule removed');
                      } catch {
                        toast.error('Failed to remove rule');
                      }
                    }}
                    className="p-1.5 rounded-md text-content-tertiary hover:text-status-error hover:bg-status-error-bg transition-colors"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className={`px-6 py-4 ${rules.length > 0 ? 'border-t border-border-subtle' : ''}`}>
            <div className="flex items-center gap-2">
              <Input
                placeholder="Source path (e.g. /api/)"
                value={newRule.source_path}
                onChange={(e) => setNewRule({ ...newRule, source_path: e.target.value })}
                className="flex-1"
              />
              <ArrowRight className="w-4 h-4 text-content-tertiary shrink-0" />
              <select
                value={newRule.dest_service_id}
                onChange={(e) => setNewRule({ ...newRule, dest_service_id: e.target.value })}
                className="flex-1 bg-surface-tertiary border border-border-default rounded-md px-3 py-2 text-sm text-content-primary appearance-none focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all duration-150"
              >
                <option value="">Destination service...</option>
                {workspaceServices.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
              <Input
                placeholder="Dest path (e.g. /api/)"
                value={newRule.dest_path}
                onChange={(e) => setNewRule({ ...newRule, dest_path: e.target.value })}
                className="flex-1"
              />
              <Button
                onClick={async () => {
                  if (!serviceId || !newRule.source_path || !newRule.dest_service_id) return;
                  setAddingRule(true);
                  try {
                    const rule = await rewriteRulesApi.add(serviceId, {
                      source_path: newRule.source_path,
                      dest_service_id: newRule.dest_service_id,
                      dest_path: newRule.dest_path || newRule.source_path,
                    });
                    setRules([...rules, rule]);
                    setNewRule({ source_path: '', dest_service_id: '', dest_path: '' });
                    toast.success('Rewrite rule added');
                  } catch {
                    toast.error('Failed to add rule');
                  } finally {
                    setAddingRule(false);
                  }
                }}
                disabled={!newRule.source_path || !newRule.dest_service_id}
                loading={addingRule}
                className="shrink-0"
              >
                <Plus className="w-4 h-4" />
                Add Rule
              </Button>
            </div>
            <p className="text-xs text-content-tertiary mt-2">
              All requests matching the source path will be proxied server-side to the destination service. No CORS headers needed.
            </p>
          </div>
        </Card>
      </div>
    </div>
  );
}
