import { useState, useEffect, useRef } from 'react';
import { cn } from '../../lib/utils';
import { Terminal as TerminalIcon } from 'lucide-react';

interface TerminalLine {
    type: 'command' | 'success' | 'info' | 'error' | 'warning' | 'normal';
    content: string;
    delay?: number;
}

interface LiveTerminalProps {
    lines: TerminalLine[];
    className?: string;
    autoScroll?: boolean;
}

export function LiveTerminal({ lines, className, autoScroll = true }: LiveTerminalProps) {
    const [displayedLines, setDisplayedLines] = useState<TerminalLine[]>([]);
    const [currentLineIndex, setCurrentLineIndex] = useState(0);
    const [currentCharIndex, setCurrentCharIndex] = useState(0);
    const bottomRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (autoScroll && bottomRef.current) {
            bottomRef.current.scrollIntoView({ behavior: 'smooth' });
        }
    }, [displayedLines, currentCharIndex, autoScroll]);

    useEffect(() => {
        if (currentLineIndex >= lines.length) return;

        const line = lines[currentLineIndex];
        if (!line) return;

        const isTyping = currentCharIndex < line.content.length;

        // Calculate delay based on line type or random variation for realism
        let typingSpeed = 30; // ms per char
        if (line.type === 'command') typingSpeed = 50;

        const timeout = setTimeout(() => {
            if (isTyping) {
                setCurrentCharIndex((prev) => prev + 1);
            } else {
                // Line finished, move to next line after a pause
                const nextDelay = line.delay || 400;
                setTimeout(() => {
                    setDisplayedLines((prev) => [...prev, line]);
                    setCurrentLineIndex((prev) => prev + 1);
                    setCurrentCharIndex(0);
                }, nextDelay);
            }
        }, typingSpeed);

        return () => clearTimeout(timeout);
    }, [currentLineIndex, currentCharIndex, lines]);

    // Current line being typed (not yet in displayedLines)
    const activeLine = lines[currentLineIndex];

    return (
        <div className={cn("font-mono text-xs bg-[#0d1117] rounded-lg border border-border-default shadow-2xl overflow-hidden flex flex-col", className)}>
            {/* Terminal Header */}
            <div className="flex items-center gap-2 px-4 py-2 bg-surface-tertiary/20 border-b border-border-default/50">
                <div className="flex gap-1.5">
                    <div className="w-2.5 h-2.5 rounded-full bg-[#FF5F56]" />
                    <div className="w-2.5 h-2.5 rounded-full bg-[#FFBD2E]" />
                    <div className="w-2.5 h-2.5 rounded-full bg-[#27C93F]" />
                </div>
                <div className="flex-1 text-center text-[10px] text-content-tertiary font-medium opacity-60 flex items-center justify-center gap-1.5">
                    <TerminalIcon className="w-3 h-3" />
                    <span>railpush-cli — 80x24</span>
                </div>
            </div>

            {/* Terminal Content */}
            <div className="p-4 space-y-1.5 overflow-y-auto min-h-[200px] max-h-[400px]">
                {displayedLines.map((line, i) => (
                    <div key={i} className="break-all whitespace-pre-wrap">
                        <LineContent line={line} />
                    </div>
                ))}

                {activeLine && (
                    <div className="break-all whitespace-pre-wrap">
                        <span className={getLineColor(activeLine.type)}>
                            {activeLine.type === 'command' && <span className="text-pink-500 mr-2">$</span>}
                            {activeLine.type === 'success' && <span className="text-emerald-500 mr-2">✔</span>}
                            {activeLine.type === 'info' && <span className="text-blue-400 mr-2">ℹ</span>}
                            {activeLine.content.slice(0, currentCharIndex)}
                            <span className="inline-block w-2 h-4 align-middle bg-content-secondary animate-pulse ml-0.5" />
                        </span>
                    </div>
                )}
                <div ref={bottomRef} />
            </div>
        </div>
    );
}

function LineContent({ line }: { line: TerminalLine }) {
    return (
        <span className={getLineColor(line.type)}>
            {line.type === 'command' && <span className="text-pink-500 mr-2">$</span>}
            {line.type === 'success' && <span className="text-emerald-500 mr-2">✔</span>}
            {line.type === 'info' && <span className="text-blue-400 mr-2">ℹ</span>}
            {line.type === 'warning' && <span className="text-amber-400 mr-2">⚠</span>}
            {line.type === 'error' && <span className="text-red-500 mr-2">✖</span>}
            {line.content}
        </span>
    );
}

function getLineColor(type: TerminalLine['type']) {
    switch (type) {
        case 'command': return 'text-content-primary';
        case 'success': return 'text-emerald-400';
        case 'info': return 'text-blue-400';
        case 'warning': return 'text-amber-400';
        case 'error': return 'text-red-400';
        default: return 'text-content-secondary';
    }
}
