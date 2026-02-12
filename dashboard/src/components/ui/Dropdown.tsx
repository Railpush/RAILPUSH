import { useState, useRef, useEffect, type CSSProperties, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../../lib/utils';

interface DropdownItem {
  label: string;
  icon?: ReactNode;
  onClick: () => void;
  danger?: boolean;
  divider?: boolean;
  sectionLabel?: string;
}

interface Props {
  trigger: ReactNode;
  items: DropdownItem[];
  align?: 'left' | 'right';
  side?: 'bottom' | 'top';
}

export function Dropdown({ trigger, items, align = 'left', side = 'bottom' }: Props) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({});

  useEffect(() => {
    if (!open) return;

    const updatePosition = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const gap = 6;
      setMenuStyle({
        position: 'fixed',
        left: align === 'right' ? rect.right : rect.left,
        top: side === 'top' ? rect.top - gap : rect.bottom + gap,
      });
    };

    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);

    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [open, align, side]);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      const insideTrigger = !!triggerRef.current && triggerRef.current.contains(target);
      const insideMenu = !!menuRef.current && menuRef.current.contains(target);
      if (!insideTrigger && !insideMenu) {
        setOpen(false);
      }
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [open]);

  const menu = open ? (
    <div
      ref={menuRef}
      className={cn(
        'z-[120] min-w-[170px] bg-surface-elevated border border-border-default rounded-lg py-1 px-0.5 shadow-lg animate-slide-up transform',
        align === 'right' && '-translate-x-full',
        side === 'top' && '-translate-y-full',
      )}
      style={menuStyle}
    >
      {items.map((item, i) =>
        item.divider ? (
          <div key={i} className="h-px bg-border-subtle my-1" />
        ) : item.sectionLabel ? (
          <div key={i} className="px-2.5 pt-1.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-content-tertiary">{item.sectionLabel}</div>
        ) : (
          <button
            key={i}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => { setOpen(false); item.onClick(); }}
            className={cn(
              'w-full flex items-center gap-2 px-2.5 py-[5px] rounded-md text-[13px] transition-colors duration-100',
              item.danger
                ? 'text-status-error hover:bg-status-error-bg'
                : 'text-content-primary hover:bg-surface-tertiary'
            )}
          >
            {item.icon && <span className="flex-shrink-0">{item.icon}</span>}
            {item.label}
          </button>
        )
      )}
    </div>
  ) : null;

  return (
    <div ref={triggerRef} className="relative inline-block">
      <div onClick={() => setOpen(!open)}>{trigger}</div>
      {typeof document !== 'undefined' && menu ? createPortal(menu, document.body) : null}
    </div>
  );
}
