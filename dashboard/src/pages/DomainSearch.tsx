import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Search, Check, X, Loader2, ArrowLeft } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Card } from '../components/ui/Card';
import { registeredDomains } from '../lib/api';
import type { DomainSearchResult } from '../types';
import { toast } from 'sonner';

export function DomainSearch() {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<DomainSearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [registering, setRegistering] = useState<string | null>(null);

  const search = async () => {
    if (!query.trim()) return;
    setSearching(true);
    try {
      const res = await registeredDomains.search(query.trim());
      setResults(res);
    } catch {
      toast.error('Search failed');
    } finally {
      setSearching(false);
    }
  };

  const registerDomain = async (domain: string) => {
    setRegistering(domain);
    try {
      const d = await registeredDomains.register(domain);
      toast.success(`${domain} registered successfully!`);
      navigate(`/domains/${d.id}`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Registration failed';
      toast.error(msg);
    } finally {
      setRegistering(null);
    }
  };

  const formatPrice = (cents: number) => {
    return `$${(cents / 100).toFixed(2)}`;
  };

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => navigate('/domains')}
          className="p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <h1 className="text-2xl font-semibold text-content-primary">Register a Domain</h1>
      </div>

      <Card className="mb-6">
        <p className="text-sm text-content-secondary mb-3">
          Search for your perfect domain name. We'll check availability across all supported TLDs.
        </p>
        <div className="flex items-center gap-2">
          <div className="flex-1">
            <Input
              placeholder="Enter domain name (e.g., myapp)"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && search()}
            />
          </div>
          <Button onClick={search} loading={searching}>
            <Search className="w-4 h-4" />
            Search
          </Button>
        </div>
      </Card>

      {results.length > 0 && (
        <div className="bg-surface-secondary border border-border-default rounded-lg overflow-hidden">
          <div className="px-4 py-2.5 border-b border-border-subtle">
            <div className="grid grid-cols-12 text-xs font-medium text-content-tertiary uppercase tracking-wider">
              <div className="col-span-5">Domain</div>
              <div className="col-span-3">Status</div>
              <div className="col-span-2">Price/yr</div>
              <div className="col-span-2 text-right">Action</div>
            </div>
          </div>
          {results.map((r) => (
            <div
              key={r.domain}
              className="px-4 py-3 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50 transition-colors"
            >
              <div className="grid grid-cols-12 items-center">
                <div className="col-span-5">
                  <span className="text-sm font-medium text-content-primary">{r.domain}</span>
                </div>
                <div className="col-span-3">
                  {r.available ? (
                    <span className="inline-flex items-center gap-1 text-xs font-medium text-status-success bg-status-success-bg px-2 py-0.5 rounded-full">
                      <Check className="w-3 h-3" />
                      Available
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-xs font-medium text-status-error bg-status-error-bg px-2 py-0.5 rounded-full">
                      <X className="w-3 h-3" />
                      Taken
                    </span>
                  )}
                </div>
                <div className="col-span-2">
                  {r.available ? (
                    <span className="text-sm font-semibold text-content-primary">{formatPrice(r.price_cents)}</span>
                  ) : (
                    <span className="text-sm text-content-tertiary">-</span>
                  )}
                </div>
                <div className="col-span-2 text-right">
                  {r.available && (
                    <Button
                      size="sm"
                      onClick={() => registerDomain(r.domain)}
                      loading={registering === r.domain}
                      disabled={registering !== null}
                    >
                      Register
                    </Button>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {searching && results.length === 0 && (
        <div className="flex items-center justify-center py-16">
          <Loader2 className="w-6 h-6 text-content-tertiary animate-spin" />
        </div>
      )}
    </div>
  );
}
