import type { ServiceType, ServiceStatus, DeployStatus } from '../types';

export function cn(...classes: (string | boolean | undefined | null)[]): string {
  return classes.filter(Boolean).join(' ');
}

export function timeAgo(date: string): string {
  const now = Date.now();
  const then = new Date(date).getTime();
  const diff = now - then;
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  if (seconds < 60) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 30) return `${days}d ago`;
  return new Date(date).toLocaleDateString();
}

export function formatDate(date: string): string {
  return new Date(date).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

export function formatTime(date: string): string {
  return new Date(date).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  return `${m}m ${rs}s`;
}

export function serviceTypeLabel(type: ServiceType): string {
  const labels: Record<ServiceType, string> = {
    web: 'Web Service',
    pserv: 'Private Service',
    worker: 'Background Worker',
    cron: 'Cron Job',
    static: 'Static Site',
    keyvalue: 'Key Value',
  };
  return labels[type] || type;
}

export function serviceTypeColor(type: ServiceType): string {
  const colors: Record<ServiceType, string> = {
    web: '#2563EB',
    pserv: '#7C3AED',
    worker: '#F59E0B',
    cron: '#0EA5E9',
    static: '#10B981',
    keyvalue: '#DC2626',
  };
  return colors[type] || '#8B8BA0';
}

export function statusConfig(status: ServiceStatus | DeployStatus): { color: string; bg: string; label: string; pulse: boolean } {
  const configs: Record<string, { color: string; bg: string; label: string; pulse: boolean }> = {
    created: { color: '#475569', bg: '#E4EBF5', label: 'Created', pulse: false },
    building: { color: '#0EA5E9', bg: '#E0F2FE', label: 'Building', pulse: true },
    deploying: { color: '#0EA5E9', bg: '#E0F2FE', label: 'Deploying', pulse: true },
    live: { color: '#15803D', bg: '#D9F7E6', label: 'Live', pulse: true },
    failed: { color: '#DC2626', bg: '#FDE8E8', label: 'Failed', pulse: false },
    suspended: { color: '#B45309', bg: '#FFF7ED', label: 'Suspended', pulse: false },
    deactivated: { color: '#94A3B8', bg: '#E4EBF5', label: 'Deactivated', pulse: false },
    pending: { color: '#475569', bg: '#E4EBF5', label: 'Pending', pulse: false },
    cancelled: { color: '#94A3B8', bg: '#E4EBF5', label: 'Cancelled', pulse: false },
  };
  return configs[status] || configs.created;
}

export function copyToClipboard(text: string): Promise<void> {
  return navigator.clipboard.writeText(text);
}

export function truncate(str: string, len: number): string {
  return str.length > len ? str.slice(0, len) + '...' : str;
}
