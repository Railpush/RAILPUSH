import { cn } from '../../lib/utils';

type Size = 'sm' | 'md';

function pillClasses(size: Size) {
  return cn(
    'inline-flex items-center gap-1.5 rounded-full font-medium border',
    size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-0.5 text-xs'
  );
}

export function IncidentStatusBadge({ status, size = 'md' }: { status: string; size?: Size }) {
  const s = (status || '').toLowerCase();
  const cfg =
    s === 'firing'
      ? { label: 'Firing', dot: true, className: 'text-status-error bg-status-error/10 border-status-error/20' }
      : s === 'resolved'
      ? { label: 'Resolved', dot: false, className: 'text-status-success bg-status-success/10 border-status-success/20' }
      : { label: status || 'Unknown', dot: false, className: 'text-content-secondary bg-surface-tertiary border-border-default' };

  return (
    <span className={cn(pillClasses(size), cfg.className)}>
      <span className={cn('rounded-full', size === 'sm' ? 'h-1.5 w-1.5' : 'h-2 w-2', cfg.dot && 'animate-pulse-dot')}
        style={{ backgroundColor: 'currentColor' }}
      />
      {cfg.label}
    </span>
  );
}

export function SeverityBadge({ severity, size = 'md' }: { severity: string; size?: Size }) {
  const s = (severity || '').toLowerCase();
  const cfg =
    s === 'critical'
      ? { label: 'Critical', className: 'text-status-error bg-status-error/10 border-status-error/20' }
      : s === 'warning'
      ? { label: 'Warning', className: 'text-status-warning bg-status-warning/10 border-status-warning/20' }
      : s === 'info'
      ? { label: 'Info', className: 'text-status-info bg-status-info/10 border-status-info/20' }
      : s
      ? { label: severity, className: 'text-content-secondary bg-surface-tertiary border-border-default' }
      : { label: 'Normal', className: 'text-content-tertiary bg-surface-tertiary border-border-default' };

  return (
    <span className={cn(pillClasses(size), cfg.className)}>
      {cfg.label}
    </span>
  );
}

