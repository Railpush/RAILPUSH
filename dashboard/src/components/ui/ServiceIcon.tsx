import { Globe, FileText, Lock, Cog, Clock, Database, Key, Layers } from 'lucide-react';
import type { ServiceType } from '../../types';
import { serviceTypeColor } from '../../lib/utils';

interface Props {
  type: ServiceType | 'postgres' | 'blueprint';
  size?: 'sm' | 'md' | 'lg';
}

const icons = {
  web: Globe,
  static: FileText,
  pserv: Lock,
  worker: Cog,
  cron: Clock,
  keyvalue: Key,
  postgres: Database,
  blueprint: Layers,
};

const sizeClasses = {
  sm: 'w-6 h-6',
  md: 'w-8 h-8',
  lg: 'w-10 h-10',
};

const iconSizes = { sm: 14, md: 16, lg: 20 };

export function ServiceIcon({ type, size = 'md' }: Props) {
  const Icon = icons[type] || Globe;
  const color = type === 'postgres' ? '#336791' : type === 'blueprint' ? '#8A05FF' : serviceTypeColor(type as ServiceType);

  return (
    <div
      className={`${sizeClasses[size]} rounded-md flex items-center justify-center flex-shrink-0`}
      style={{ backgroundColor: `${color}15` }}
    >
      <Icon size={iconSizes[size]} style={{ color }} />
    </div>
  );
}
