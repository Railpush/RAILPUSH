import type { ReactNode } from 'react';
import { Button } from './Button';

interface Props {
  icon: ReactNode;
  title: string;
  description: string;
  action?: { label: string; onClick: () => void };
}

export function EmptyState({ icon, title, description, action }: Props) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <div className="w-12 h-12 rounded-xl bg-surface-tertiary flex items-center justify-center text-content-tertiary mb-4">
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-content-primary mb-1">{title}</h3>
      <p className="text-sm text-content-secondary max-w-sm mb-6">{description}</p>
      {action && <Button onClick={action.onClick}>{action.label}</Button>}
    </div>
  );
}
