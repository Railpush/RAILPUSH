import { cn } from '../../lib/utils';
import { Loader2 } from 'lucide-react';
import type { ButtonHTMLAttributes, ReactNode } from 'react';

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline' | 'glass';
  size?: 'sm' | 'md' | 'lg' | 'icon';
  loading?: boolean;
  children: ReactNode;
}

const variants = {
  primary: 'bg-brand text-white hover:bg-brand-hover active:bg-brand-active border border-transparent',
  secondary: 'bg-surface-tertiary text-content-primary border border-border-default hover:border-border-hover hover:bg-surface-elevated',
  danger: 'bg-status-error/10 text-status-error border border-status-error/20 hover:bg-status-error/20',
  ghost: 'bg-transparent text-content-secondary hover:text-content-primary hover:bg-surface-tertiary',
  outline: 'bg-transparent text-brand border border-brand/50 hover:bg-brand/10 hover:border-brand',
  glass: 'glass-panel text-content-primary hover:bg-surface-elevated hover:border-brand/30',
};

const sizes = {
  sm: 'px-3 py-1.5 text-xs',
  md: 'px-4 py-2 text-sm',
  lg: 'px-6 py-3 text-base',
  icon: 'p-2',
};

export function Button({ variant = 'primary', size = 'md', loading, children, className, disabled, ...props }: Props) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-md font-medium transition-all duration-200 cursor-pointer relative overflow-hidden',
        'disabled:opacity-50 disabled:cursor-not-allowed disabled:shadow-none',
        variants[variant],
        sizes[size],
        className
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <Loader2 className="h-4 w-4 animate-spin" />}
      <span className="relative z-10 flex items-center gap-2">{children}</span>
    </button>
  );
}
