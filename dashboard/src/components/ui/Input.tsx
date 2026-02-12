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
          'w-full bg-surface-secondary border rounded-lg px-3 py-2.5 text-sm text-content-primary shadow-[0_1px_2px_rgba(15,23,42,0.06)]',
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
