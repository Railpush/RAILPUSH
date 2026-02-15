import { cn } from '../../lib/utils';
import type { ReactNode, HTMLAttributes } from 'react';

interface Props extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  hover?: boolean;
  padding?: 'sm' | 'md' | 'lg';
}

const paddings = { sm: 'p-3', md: 'p-4', lg: 'p-6' };

export function Card({ children, hover, padding = 'md', className, ...props }: Props) {
  return (
    <div
      className={cn(
        'glass-panel rounded-md transition-colors duration-150',
        hover && 'hover:border-border-hover hover:bg-surface-tertiary cursor-pointer',
        paddings[padding],
        className
      )}
      {...props}
    >
      {children}
    </div>
  );
}
