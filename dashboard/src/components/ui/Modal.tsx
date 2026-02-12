import { X } from 'lucide-react';
import type { ReactNode } from 'react';

interface Props {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}

export function Modal({ open, onClose, title, children, footer }: Props) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center animate-fade-in" onClick={onClose}>
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
      <div
        className="relative bg-surface-secondary border border-border-default rounded-xl p-6 w-[90%] max-w-[560px] max-h-[85vh] overflow-y-auto shadow-2xl animate-slide-up"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-semibold text-content-primary">{title}</h2>
          <button onClick={onClose} className="text-content-tertiary hover:text-content-primary transition-colors p-1 rounded-md hover:bg-surface-tertiary">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div>{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 mt-6 pt-4 border-t border-border-subtle">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
