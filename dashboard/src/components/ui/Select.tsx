import { cn } from '../../lib/utils';
import { ChevronDown } from 'lucide-react';
import type { SelectHTMLAttributes } from 'react';

interface Props extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  options: { value: string; label: string }[];
}

export function Select({ label, options, className, ...props }: Props) {
  return (
    <div className="space-y-1.5">
      {label && <label className="block text-sm font-medium text-content-primary">{label}</label>}
      <div className="relative">
        <select
          className={cn(
            'w-full bg-surface-tertiary border border-border-default rounded-md px-3 py-2 text-sm text-content-primary',
            'appearance-none pr-9 focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15',
            'transition-all duration-150',
            className
          )}
          {...props}
        >
          {options.map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </select>
        <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-content-tertiary pointer-events-none" />
      </div>
    </div>
  );
}
