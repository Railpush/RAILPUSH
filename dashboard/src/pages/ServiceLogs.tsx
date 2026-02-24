import { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useLocation } from 'react-router-dom';
import { Search, Maximize2, Minimize2, RefreshCw, Copy, Check, ChevronDown } from 'lucide-react';
import { logs as logsApi, connectLogStream } from '../lib/api';
import { copyToClipboard } from '../lib/utils';
import type { LogEntry } from '../types';

interface DeployLog {
  id: string;
  status: string;
  log: string;
  started_at: string;
}

// ── helpers ──────────────────────────────────────────────────────────────────

/** Strip ANSI escape sequences from a string */
function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\x1b\[[0-9;]*[A-Za-z]/g, '').replace(/\x1b\][^\x07]*\x07/g, '');
}

function formatTs(raw: string): string {
  try {
    const d = new Date(raw);
    if (isNaN(d.getTime())) return '';
    return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: true });
  } catch { return ''; }
}

function levelCls(level: string) {
  switch (level) {
    case 'error': return 'text-red-400';
    case 'warn': return 'text-amber-400';
    case 'debug': return 'text-content-tertiary';
    default: return 'text-content-secondary';
  }
}

function levelBadgeCls(level: string) {
  switch (level) {
    case 'error': return 'bg-red-500/15 text-red-400 border-red-500/25';
    case 'warn': return 'bg-amber-500/15 text-amber-400 border-amber-500/25';
    case 'debug': return 'bg-zinc-500/15 text-zinc-400 border-zinc-500/25';
    default: return 'bg-blue-500/15 text-blue-400 border-blue-500/25';
  }
}

/** Parse an HTTP status code from a runtime log message (e.g. "GET /api 200 3ms") */
function parseHttpStatus(msg: string): { method?: string; status?: number } {
  const httpMatch = msg.match(/\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b/);
  const statusMatch = msg.match(/\b([1-5]\d{2})\b/);
  return {
    method: httpMatch?.[1],
    status: statusMatch ? parseInt(statusMatch[1], 10) : undefined,
  };
}

function parseHttpPath(msg: string): string | undefined {
  const m = msg.match(/\b(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([^\s?]+)/);
  return m?.[1];
}

function httpStatusCls(code: number) {
  if (code >= 500) return 'bg-red-500/15 text-red-400 border-red-500/25';
  if (code >= 400) return 'bg-amber-500/15 text-amber-400 border-amber-500/25';
  if (code >= 300) return 'bg-blue-500/15 text-blue-400 border-blue-500/25';
  if (code >= 200) return 'bg-emerald-500/15 text-emerald-400 border-emerald-500/25';
  return 'bg-zinc-500/15 text-zinc-400 border-zinc-500/25';
}

function deployLineLevel(line: string): 'error' | 'warn' | 'success' | 'info' | 'normal' {
  const l = line.toLowerCase();
  if (l.includes('error') || l.includes('fatal') || l.includes('failed') || l.includes('panic')) return 'error';
  if (l.includes('warn')) return 'warn';
  if (l.includes('success') || l.includes('passed') || l.includes('live at') || l.includes('pushed')) return 'success';
  if (l.includes('step') || l.includes('==>') || l.includes('detected runtime') || l.includes('cloning') || l.includes('building')) return 'info';
  return 'normal';
}

function deployLineCls(level: string) {
  switch (level) {
    case 'error': return 'text-red-400';
    case 'warn': return 'text-amber-400';
    case 'success': return 'text-emerald-400';
    case 'info': return 'text-blue-400';
    default: return 'text-content-secondary';
  }
}

/** Parse a deploy log line, extracting optional container prefix like [kaniko] or [clone] */
function parseDeployLine(raw: string): { prefix: string; message: string } {
  const clean = stripAnsi(raw).replace(/\r/g, '');
  const m = clean.match(/^\s*\[(\w+)]\s*(.*)/);
  if (m) return { prefix: m[1], message: m[2] };
  const m2 = clean.match(/^\s{4}\[(\w+)]\s*(.*)/);
  if (m2) return { prefix: m2[1], message: m2[2] };
  return { prefix: '', message: clean };
}

// ── inline copy button ──────────────────────────────────────────────────────

function LineCopy({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={(e) => { e.stopPropagation(); copyToClipboard(text); setCopied(true); setTimeout(() => setCopied(false), 1500); }}
      className="opacity-0 group-hover:opacity-100 p-0.5 rounded text-content-tertiary hover:text-content-primary transition-opacity flex-shrink-0"
      title="Copy line"
    >
      {copied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
    </button>
  );
}

// ── component ───────────────────────────────────────────────────────────────

export function ServiceLogs() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const location = useLocation();
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [deployLogs, setDeployLogs] = useState<DeployLog[]>([]);
  const [search, setSearch] = useState('');
  const [useRegex, setUseRegex] = useState(false);
  const [levelFilter, setLevelFilter] = useState<'all' | 'error' | 'warn' | 'info' | 'debug'>('all');
  const [methodFilter, setMethodFilter] = useState<'all' | 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS'>('all');
  const [statusFilter, setStatusFilter] = useState<'all' | '2xx' | '3xx' | '4xx' | '5xx'>('all');
  const [pathFilter, setPathFilter] = useState('');
  const [isLive, setIsLive] = useState(true);
  const [fullscreen, setFullscreen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [logType, setLogType] = useState<'runtime' | 'deploy'>('runtime');
  const [copyAll, setCopyAll] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const type = new URLSearchParams(location.search).get('type');
    if (type === 'deploy') setLogType('deploy');
    if (type === 'runtime') setLogType('runtime');
  }, [location.search]);

  const fetchLogs = useCallback(() => {
    if (!serviceId) return;
    setLoading(true);
    if (logType === 'runtime') {
      logsApi.query(serviceId, { limit: 200 })
        .then((data) => { if (Array.isArray(data) && data.length > 0) setEntries(data); else setEntries([]); })
        .catch(() => {})
        .finally(() => setLoading(false));
    } else {
      logsApi.query(serviceId, { limit: 10, type: 'deploy' })
        .then((data: unknown) => { const logs = data as DeployLog[]; if (Array.isArray(logs)) setDeployLogs(logs); else setDeployLogs([]); })
        .catch(() => {})
        .finally(() => setLoading(false));
    }
  }, [serviceId, logType]);

  useEffect(() => {
    fetchLogs();
    if (serviceId && logType === 'runtime') {
      try { wsRef.current = connectLogStream(serviceId, (entry) => { setEntries((prev) => [...prev, entry].slice(-1000)); }); } catch {}
    }
    return () => { wsRef.current?.close(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serviceId, logType]);

  useEffect(() => {
    if (isLive && containerRef.current) containerRef.current.scrollTop = containerRef.current.scrollHeight;
  }, [entries, deployLogs, isLive]);

  const filtered = entries.filter((e) => {
    const http = parseHttpStatus(e.message);
    const path = parseHttpPath(e.message) || '';

    if (levelFilter !== 'all' && e.level !== levelFilter) return false;
    if (methodFilter !== 'all' && http.method !== methodFilter) return false;
    if (statusFilter !== 'all') {
      if (!http.status) return false;
      if (statusFilter === '2xx' && (http.status < 200 || http.status >= 300)) return false;
      if (statusFilter === '3xx' && (http.status < 300 || http.status >= 400)) return false;
      if (statusFilter === '4xx' && (http.status < 400 || http.status >= 500)) return false;
      if (statusFilter === '5xx' && http.status < 500) return false;
    }
    if (pathFilter.trim() && !path.toLowerCase().includes(pathFilter.trim().toLowerCase())) return false;

    if (!search.trim()) return true;
    if (!useRegex) return e.message.toLowerCase().includes(search.toLowerCase());
    try {
      const re = new RegExp(search, 'i');
      return re.test(e.message);
    } catch {
      return false;
    }
  });
  const filteredDeployLogs = search ? deployLogs.filter((d) => d.log.toLowerCase().includes(search.toLowerCase())) : deployLogs;

  const handleCopyAll = async () => {
    let text = '';
    if (logType === 'runtime') {
      text = filtered.map((e) => `${formatTs(e.timestamp)} [${e.level.toUpperCase()}] [${e.instance_id}] ${e.message}`).join('\n');
    } else {
      text = filteredDeployLogs.map((d) => `=== Deploy ${d.id.slice(0, 8)} (${d.status}) ===\n${stripAnsi(d.log)}`).join('\n\n');
    }
    await copyToClipboard(text);
    setCopyAll(true);
    setTimeout(() => setCopyAll(false), 2000);
  };

  return (
    <div className={fullscreen ? 'fixed inset-0 z-50 bg-surface-primary p-4' : ''}>
      <div className="flex items-center justify-between mb-4">
        <div>
          <p className="text-[11px] uppercase tracking-[0.22em] text-content-tertiary font-semibold">Observability</p>
          <h1 className="text-2xl font-semibold text-content-primary">Logs</h1>
        </div>
        <div className="flex items-center gap-1.5">
          <button onClick={handleCopyAll} className="p-2 rounded-lg text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors border border-transparent hover:border-border-default" title="Copy all logs">
            {copyAll ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
          </button>
          <button onClick={fetchLogs} className="p-2 rounded-lg text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors border border-transparent hover:border-border-default" title="Refresh logs">
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button onClick={() => setFullscreen(!fullscreen)} className="p-2 rounded-lg text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors border border-transparent hover:border-border-default">
            {fullscreen ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center gap-3 mb-3">
        <div className="flex rounded-lg border border-border-default overflow-hidden bg-surface-secondary">
          <button onClick={() => setLogType('runtime')} className={`px-3 py-2 text-sm font-medium transition-colors ${logType === 'runtime' ? 'bg-surface-tertiary text-content-primary shadow-inner' : 'text-content-tertiary hover:text-content-secondary'}`}>Runtime</button>
          <button onClick={() => setLogType('deploy')} className={`px-3 py-2 text-sm font-medium transition-colors border-l border-border-default ${logType === 'deploy' ? 'bg-surface-tertiary text-content-primary shadow-inner' : 'text-content-tertiary hover:text-content-secondary'}`}>Deploy</button>
        </div>
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
          <input type="text" placeholder="Search logs..." value={search} onChange={(e) => setSearch(e.target.value)} className="w-full bg-surface-secondary border border-border-default rounded-lg pl-9 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all font-mono shadow-[0_1px_2px_rgba(15,23,42,0.05)]" />
        </div>
        {logType === 'runtime' && (
          <button onClick={() => setIsLive(!isLive)} className={`inline-flex items-center gap-1.5 px-3 py-2 rounded-md text-sm font-medium transition-colors ${isLive ? 'bg-status-success-bg text-status-success border border-status-success/20' : 'bg-surface-tertiary text-content-secondary border border-border-default'}`}>
            <span className={`w-2 h-2 rounded-full ${isLive ? 'bg-status-success animate-pulse-dot' : 'bg-content-tertiary'}`} />Live
          </button>
        )}
      </div>

      {logType === 'runtime' && (
        <div className="grid grid-cols-1 md:grid-cols-5 gap-2 mb-3">
          <select value={levelFilter} onChange={(e) => setLevelFilter(e.target.value as typeof levelFilter)} className="bg-surface-secondary border border-border-default rounded-lg px-3 py-2 text-sm text-content-primary">
            <option value="all">All levels</option>
            <option value="error">Error</option>
            <option value="warn">Warn</option>
            <option value="info">Info</option>
            <option value="debug">Debug</option>
          </select>
          <select value={methodFilter} onChange={(e) => setMethodFilter(e.target.value as typeof methodFilter)} className="bg-surface-secondary border border-border-default rounded-lg px-3 py-2 text-sm text-content-primary">
            <option value="all">All methods</option>
            <option value="GET">GET</option>
            <option value="POST">POST</option>
            <option value="PUT">PUT</option>
            <option value="PATCH">PATCH</option>
            <option value="DELETE">DELETE</option>
            <option value="HEAD">HEAD</option>
            <option value="OPTIONS">OPTIONS</option>
          </select>
          <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value as typeof statusFilter)} className="bg-surface-secondary border border-border-default rounded-lg px-3 py-2 text-sm text-content-primary">
            <option value="all">All status</option>
            <option value="2xx">2xx</option>
            <option value="3xx">3xx</option>
            <option value="4xx">4xx</option>
            <option value="5xx">5xx</option>
          </select>
          <input value={pathFilter} onChange={(e) => setPathFilter(e.target.value)} placeholder="Path contains (e.g. /api/users)" className="bg-surface-secondary border border-border-default rounded-lg px-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary" />
          <label className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-border-default bg-surface-secondary text-sm text-content-secondary">
            <input type="checkbox" checked={useRegex} onChange={(e) => setUseRegex(e.target.checked)} className="accent-brand" /> Regex search
          </label>
        </div>
      )}

      {/* Log viewer */}
      <div ref={containerRef} className={`rounded-xl border border-border-default overflow-auto font-mono text-xs bg-surface-secondary ${fullscreen ? 'h-[calc(100vh-140px)]' : 'h-[600px]'}`}>
        {logType === 'runtime' ? (
          filtered.length === 0 ? (
            <div className="flex items-center justify-center h-full text-content-tertiary text-sm">
              {loading ? 'Loading logs...' : search ? 'No matching log entries' : 'No logs available. Deploy a service to see logs.'}
            </div>
          ) : (
            <table className="w-full border-collapse">
              <tbody>
                {filtered.map((entry, i) => {
                  const http = parseHttpStatus(entry.message);
                  return (
                    <tr key={i} className="group hover:bg-white/[0.02] border-b border-white/[0.03]">
                      <td className="py-1.5 pl-3 pr-2 text-content-tertiary whitespace-nowrap select-none align-top w-[100px]">
                        {formatTs(entry.timestamp)}
                      </td>
                      <td className="py-1.5 px-1.5 align-top w-[52px]">
                        <span className={`inline-flex items-center justify-center px-1.5 py-0 rounded border text-[10px] font-semibold uppercase leading-relaxed ${levelBadgeCls(entry.level)}`}>
                          {entry.level === 'error' ? 'ERR' : entry.level === 'warn' ? 'WRN' : entry.level === 'debug' ? 'DBG' : 'INF'}
                        </span>
                      </td>
                      <td className={`py-1.5 px-2 ${levelCls(entry.level)} leading-relaxed align-top`}>
                        <span className="inline-flex flex-wrap items-baseline gap-1.5">
                          {http.method && (
                            <span className="inline-flex items-center px-1.5 py-0 rounded border border-violet-500/25 bg-violet-500/15 text-violet-400 text-[10px] font-semibold leading-relaxed flex-shrink-0">
                              {http.method}
                            </span>
                          )}
                          {http.status && (
                            <span className={`inline-flex items-center px-1.5 py-0 rounded border text-[10px] font-semibold tabular-nums leading-relaxed flex-shrink-0 ${httpStatusCls(http.status)}`}>
                              {http.status}
                            </span>
                          )}
                          <span className="break-all">{entry.message}</span>
                        </span>
                      </td>
                      <td className="py-1.5 pr-2 align-top w-[24px]">
                        <LineCopy text={`${formatTs(entry.timestamp)} [${entry.level.toUpperCase()}] ${entry.message}`} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )
        ) : (
          filteredDeployLogs.length === 0 ? (
            <div className="flex items-center justify-center h-full text-content-tertiary text-sm">
              {loading ? 'Loading deploy logs...' : 'No deploy logs available.'}
            </div>
          ) : (
            <div>
              {filteredDeployLogs.map((deploy) => (
                <DeployLogBlock key={deploy.id} deploy={deploy} />
              ))}
            </div>
          )
        )}
      </div>

      <div className="flex items-center gap-4 mt-2 text-[11px] text-content-tertiary">
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">M</kbd> Fullscreen</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">/</kbd> Focus search</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">Home</kbd> Jump to start</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">End</kbd> Jump to end</span>
      </div>
    </div>
  );
}

// ── deploy log block ────────────────────────────────────────────────────────

function DeployLogBlock({ deploy }: { deploy: DeployLog }) {
  const [collapsed, setCollapsed] = useState(false);
  const [copied, setCopied] = useState(false);

  const lines = (deploy.log || '').split('\n').filter(Boolean);

  const handleCopy = async () => {
    await copyToClipboard(stripAnsi(deploy.log));
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="border-b border-white/[0.04]">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2.5 bg-white/[0.02] border-b border-white/[0.04]">
        <button onClick={() => setCollapsed(!collapsed)} className="text-content-tertiary hover:text-content-primary transition-colors">
          <ChevronDown className={`w-3.5 h-3.5 transition-transform ${collapsed ? '-rotate-90' : ''}`} />
        </button>
        <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold uppercase ${
          deploy.status === 'live'
            ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/25'
            : deploy.status === 'failed'
            ? 'bg-red-500/15 text-red-400 border border-red-500/25'
            : 'bg-amber-500/15 text-amber-400 border border-amber-500/25'
        }`}>{deploy.status}</span>
        <span className="text-content-tertiary text-[11px]">{deploy.started_at ? formatTs(deploy.started_at) : ''}</span>
        <span className="text-content-tertiary text-[11px] font-mono">{deploy.id.slice(0, 8)}</span>
        <span className="text-content-tertiary text-[11px] ml-auto">{lines.length} lines</span>
        <button onClick={handleCopy} className="p-1 rounded text-content-tertiary hover:text-content-primary transition-colors" title="Copy deploy log">
          {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
        </button>
      </div>

      {/* Lines */}
      {!collapsed && (
        <table className="w-full border-collapse">
          <tbody>
            {lines.map((raw, j) => {
              const { prefix, message } = parseDeployLine(raw);
              const level = deployLineLevel(message);
              return (
                <tr key={j} className="group hover:bg-white/[0.02] border-b border-white/[0.02]">
                  <td className="py-1 pl-3 pr-1 text-content-tertiary/50 select-none align-top w-[40px] text-right tabular-nums">{j + 1}</td>
                  {prefix && (
                    <td className="py-1 px-1 align-top whitespace-nowrap w-[64px]">
                      <span className="inline-flex items-center px-1.5 py-0 rounded border border-cyan-500/25 bg-cyan-500/10 text-cyan-400 text-[10px] font-semibold leading-relaxed">
                        {prefix}
                      </span>
                    </td>
                  )}
                  <td className={`py-1 px-2 ${deployLineCls(level)} break-all leading-relaxed align-top`}>
                    {message || '\u00A0'}
                  </td>
                  <td className="py-1 pr-2 align-top w-[24px]">
                    <LineCopy text={stripAnsi(raw)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
