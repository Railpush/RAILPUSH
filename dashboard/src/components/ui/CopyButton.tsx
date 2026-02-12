import { useState } from 'react';
import { Copy, Check } from 'lucide-react';
import { copyToClipboard } from '../../lib/utils';

interface Props {
  text: string;
  className?: string;
}

export function CopyButton({ text, className }: Props) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await copyToClipboard(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      onClick={handleCopy}
      className={`p-1.5 rounded-md text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors ${className}`}
      title="Copy to clipboard"
    >
      {copied ? <Check className="h-4 w-4 text-status-success" /> : <Copy className="h-4 w-4" />}
    </button>
  );
}
