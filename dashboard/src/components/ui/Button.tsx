import { cn } from '../../lib/utils';
import { Loader2 } from 'lucide-react';
import type { ButtonHTMLAttributes, ReactNode } from 'react';

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  children: ReactNode;
}

const variants = {
  primary: 'bg-brand text-white hover:bg-brand-hover active:bg-brand-active',
  secondary: 'bg-transparent text-content-primary border border-border-default hover:bg-surface-tertiary',
  danger: 'bg-status-error text-white hover:opacity-90',
  ghost: 'bg-transparent text-content-secondary hover:text-content-primary hover:bg-surface-tertiary',
  outline: 'bg-transparent text-brand border border-brand hover:bg-brand/10',
};

const sizes = {
  sm: 'px-3 py-1 text-xs',
  md: 'px-4 py-2 text-sm',
  lg: 'px-6 py-3 text-base',
};

export function Button({ variant = 'primary', size = 'md', loading, children, className, disabled, ...props }: Props) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-md font-medium transition-all duration-150 cursor-pointer',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        variants[variant],
        sizes[size],
        className
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <Loader2 className="h-4 w-4 animate-spin" />}
      {children}
    </button>
  );
}
