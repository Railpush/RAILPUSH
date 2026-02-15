import { cn } from '../../lib/utils';
import type { ReactNode, HTMLAttributes } from 'react';

interface Props extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  hover?: boolean;
  padding?: 'none' | 'sm' | 'md' | 'lg';
  glass?: boolean;
}

const paddings = { none: 'p-0', sm: 'p-3', md: 'p-5', lg: 'p-7' };

export function Card({ children, hover, padding = 'md', glass = true, className, ...props }: Props) {
  return (
    <div
      className={cn(
        glass ? 'glass-panel' : 'bg-surface-secondary border border-border-default',
        'rounded-xl transition-all duration-200',
        hover && 'hover:border-border-hover hover:bg-surface-grad/5 hover:neon-border-glow cursor-pointer',
        paddings[padding],
        className
      )}
      {...props}
    >
      {children}
    </div>
  );
}
