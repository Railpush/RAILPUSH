import { cn } from '../../lib/utils';
import type { InputHTMLAttributes, ReactNode } from 'react';

interface Props extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  icon?: ReactNode;
}

export function Input({ label, error, hint, icon, className, ...props }: Props) {
  return (
    <div className="space-y-1.5">
      {label && <label className="block text-sm font-medium text-content-primary">{label}</label>}
      <div className="relative">
        {icon && (
          <div className="absolute left-3 top-1/2 -translate-y-1/2 text-content-tertiary pointer-events-none">
            {icon}
          </div>
        )}
        <input
          className={cn(
            'app-input w-full bg-surface-secondary border rounded-lg py-2.5 text-sm text-content-primary shadow-[0_8px_20px_rgba(15,23,42,0.05)]',
            icon ? 'pl-9 pr-3' : 'px-3',
            'placeholder:text-content-tertiary',
            'focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15',
            'transition-all duration-150',
            error ? 'border-status-error' : 'border-border-default',
            className
          )}
          {...props}
        />
      </div>
      {hint && !error && <p className="text-xs text-content-tertiary">{hint}</p>}
      {error && <p className="text-xs text-status-error">{error}</p>}
    </div>
  );
}
