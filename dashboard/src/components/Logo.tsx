interface LogoMarkProps {
  size?: number;
  className?: string;
}

export function LogoMark({ size = 32, className = '' }: LogoMarkProps) {
  return (
    <img
      src="/railpush-mark-contrast.svg"
      alt="railpush"
      width={size}
      height={size}
      className={className}
      decoding="async"
    />
  );
}

interface LogoProps {
  size?: number;
  showText?: boolean;
  className?: string;
  textClassName?: string;
}

export function Logo({ size = 28, showText = true, className = '', textClassName = '' }: LogoProps) {
  if (!showText) {
    return <LogoMark size={size} className={className} />;
  }

  const width = Math.round((300 / 72) * size);
  const combinedClass = [className, textClassName].filter(Boolean).join(' ');

  return (
    <img
      src="/railpush-logo-contrast.svg"
      alt="railpush"
      width={width}
      height={size}
      className={combinedClass}
      decoding="async"
    />
  );
}
