import { cn, statusConfig } from '../../lib/utils';
import type { ServiceStatus, DeployStatus } from '../../types';

interface Props {
  status: ServiceStatus | DeployStatus;
  size?: 'sm' | 'md';
  pulse?: boolean;
}

export function StatusBadge({ status, size = 'md', pulse = true }: Props) {
  const config = statusConfig(status);

  // Map legacy status colors to new CSS variables if needed, 
  // or rely on the utility function returning hex codes that match our theme.
  // For now, we assume statusConfig returns hex codes. We'll use inline styles to apply them dynamically 
  // but wrap them in our glass/glow containers.

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full font-medium border border-transparent',
        size === 'sm' ? 'px-2 py-0.5 text-[10px] tracking-wide uppercase' : 'px-2.5 py-0.5 text-xs',
        'transition-all duration-300'
      )}
      style={{
        color: config.color,
        backgroundColor: `${config.color}15`, // 15 = ~8% opacity
        borderColor: `${config.color}20`
      }}
    >
      <span className="relative flex h-2 w-2">
        {pulse && config.pulse && (
          <span
            className="animate-ping absolute inline-flex h-full w-full rounded-full opacity-75"
            style={{ backgroundColor: config.color }}
          ></span>
        )}
        <span
          className={cn('relative inline-flex rounded-full h-2 w-2')}
          style={{ backgroundColor: config.color }}
        ></span>
      </span>
      {config.label}
    </span>
  );
}
