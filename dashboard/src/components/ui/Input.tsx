import { cn } from '../../lib/utils';
import type { InputHTMLAttributes } from 'react';

interface Props extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
}

export function Input({ label, error, hint, className, ...props }: Props) {
  return (
    <div className="space-y-1.5">
      {label && <label className="block text-sm font-medium text-content-primary">{label}</label>}
      <input
        className={cn(
          'w-full bg-surface-tertiary border rounded-md px-3 py-2 text-sm text-content-primary',
          'placeholder:text-content-tertiary',
          'focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15',
          'transition-all duration-150',
          error ? 'border-status-error' : 'border-border-default',
          className
        )}
        {...props}
      />
      {hint && !error && <p className="text-xs text-content-tertiary">{hint}</p>}
      {error && <p className="text-xs text-status-error">{error}</p>}
    </div>
  );
}
