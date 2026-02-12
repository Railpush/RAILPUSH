interface LogoMarkProps {
  size?: number;
  className?: string;
}

/** RailPush icon mark — arrow on rails in a rounded blue square */
export function LogoMark({ size = 32, className = '' }: LogoMarkProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <rect width="32" height="32" rx="8" fill="#3B82F6" />
      <path d="M8.5 16L16 16L16 11.5L24 16L16 20.5V16Z" fill="white" />
      <rect x="7" y="22" width="12" height="2.5" rx="1.25" fill="white" opacity="0.35" />
      <rect x="7" y="25.5" width="12" height="2.5" rx="1.25" fill="white" opacity="0.2" />
    </svg>
  );
}

interface LogoProps {
  size?: number;
  showText?: boolean;
  className?: string;
  textClassName?: string;
}

/** Full RailPush logo — icon + wordmark */
export function Logo({ size = 28, showText = true, className = '', textClassName = '' }: LogoProps) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <LogoMark size={size} />
      {showText && (
        <span className={`font-semibold tracking-tight ${textClassName}`} style={{ fontSize: size * 0.57 }}>
          RailPush
        </span>
      )}
    </span>
  );
}
