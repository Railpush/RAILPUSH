import { cn, statusConfig } from '../../lib/utils';
import type { ServiceStatus, DeployStatus } from '../../types';

interface Props {
  status: ServiceStatus | DeployStatus;
  size?: 'sm' | 'md';
}

export function StatusBadge({ status, size = 'md' }: Props) {
  const config = statusConfig(status);
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full font-medium',
        size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-0.5 text-xs'
      )}
      style={{ color: config.color, backgroundColor: config.bg }}
    >
      <span
        className={cn('rounded-full', config.pulse && 'animate-pulse-dot', size === 'sm' ? 'h-1.5 w-1.5' : 'h-2 w-2')}
        style={{ backgroundColor: config.color }}
      />
      {config.label}
    </span>
  );
}
