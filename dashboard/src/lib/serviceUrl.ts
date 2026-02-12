import type { Service, ServiceType } from '../types';

const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '::1']);

function normalizeLabel(name: string): string {
  const cleaned = name
    .trim()
    .toLowerCase()
    .replace(/[_\s.]+/g, '-')
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '');

  return (cleaned || 'service').slice(0, 63).replace(/^-+|-+$/g, '') || 'service';
}

export function isPublicServiceType(type: ServiceType | string): boolean {
  return type === 'web' || type === 'static';
}

export function resolveDeployDomain(): string {
  const fromEnv = (import.meta.env.VITE_DEPLOY_DOMAIN as string | undefined)?.trim().toLowerCase();
  if (fromEnv) return fromEnv;

  const hostname = window.location.hostname.toLowerCase();
  if (!hostname || LOCAL_HOSTS.has(hostname) || hostname.endsWith('.local')) {
    return '';
  }

  return hostname;
}

export function buildDefaultServiceHostname(serviceName: string): string {
  const deployDomain = resolveDeployDomain();
  if (!deployDomain) return '';
  return `${normalizeLabel(serviceName)}.${deployDomain}`;
}

export function buildDefaultServiceUrl(service: Pick<Service, 'name' | 'type' | 'host_port' | 'public_url'>): string {
  if (!isPublicServiceType(service.type)) return '';

  if (service.public_url) return service.public_url;

  const host = buildDefaultServiceHostname(service.name);
  if (host) {
    return `https://${host}`;
  }

  if (service.host_port > 0) {
    return `http://localhost:${service.host_port}`;
  }

  return '';
}

export function hostnameFromUrl(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return '';
  }
}
