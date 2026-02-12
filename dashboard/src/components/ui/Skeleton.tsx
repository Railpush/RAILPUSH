import { cn } from '../../lib/utils';

interface Props {
  className?: string;
  count?: number;
}

export function Skeleton({ className, count = 1 }: Props) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className={cn('animate-shimmer rounded-md h-4', className)} />
      ))}
    </>
  );
}

export function ServiceSkeleton() {
  return (
    <div className="border-b border-border-subtle px-4 py-3 flex items-center gap-3">
      <Skeleton className="w-8 h-8 rounded-md" />
      <div className="flex-1 space-y-2">
        <Skeleton className="w-32 h-4" />
        <Skeleton className="w-48 h-3" />
      </div>
      <Skeleton className="w-16 h-5 rounded-full" />
    </div>
  );
}
