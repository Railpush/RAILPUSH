import { useState, useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { Search, Maximize2, Minimize2, AlertTriangle, XCircle, RefreshCw } from 'lucide-react';
import { logs as logsApi, connectLogStream } from '../lib/api';
import { formatTime } from '../lib/utils';
import type { LogEntry } from '../types';

interface DeployLog {
  id: string;
  status: string;
  log: string;
  started_at: string;
}

export function ServiceLogs() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [deployLogs, setDeployLogs] = useState<DeployLog[]>([]);
  const [search, setSearch] = useState('');
  const [isLive, setIsLive] = useState(true);
  const [fullscreen, setFullscreen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [logType, setLogType] = useState<'runtime' | 'deploy'>('runtime');
  const containerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);

  const fetchLogs = () => {
    if (!serviceId) return;
    setLoading(true);

    if (logType === 'runtime') {
      logsApi.query(serviceId, { limit: 200 })
        .then((data) => {
          if (Array.isArray(data) && data.length > 0) {
            setEntries(data);
          } else {
            setEntries([]);
          }
        })
        .catch(() => {})
        .finally(() => setLoading(false));
    } else {
      logsApi.query(serviceId, { limit: 10, type: 'deploy' })
        .then((data: unknown) => {
          const logs = data as DeployLog[];
          if (Array.isArray(logs)) {
            setDeployLogs(logs);
          } else {
            setDeployLogs([]);
          }
        })
        .catch(() => {})
        .finally(() => setLoading(false));
    }
  };

  useEffect(() => {
    fetchLogs();

    // Connect WebSocket for live streaming (runtime logs only)
    if (serviceId && logType === 'runtime') {
      try {
        wsRef.current = connectLogStream(serviceId, (entry) => {
          setEntries((prev) => [...prev, entry].slice(-1000));
        });
      } catch {}
    }

    return () => { wsRef.current?.close(); };
  }, [serviceId, logType]);

  useEffect(() => {
    if (isLive && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [entries, deployLogs, isLive]);

  const filtered = search
    ? entries.filter((e) => e.message.toLowerCase().includes(search.toLowerCase()))
    : entries;

  const filteredDeployLogs = search
    ? deployLogs.filter((d) => d.log.toLowerCase().includes(search.toLowerCase()))
    : deployLogs;

  const levelIcon = (level: string) => {
    switch (level) {
      case 'warn': return <AlertTriangle className="w-3.5 h-3.5 text-status-warning flex-shrink-0" />;
      case 'error': return <XCircle className="w-3.5 h-3.5 text-status-error flex-shrink-0" />;
      default: return <span className="w-3.5 h-3.5 flex-shrink-0" />;
    }
  };

  const levelColor = (level: string) => {
    switch (level) {
      case 'warn': return 'text-status-warning';
      case 'error': return 'text-status-error';
      case 'debug': return 'text-content-tertiary';
      default: return 'text-content-primary';
    }
  };

  const deployLineColor = (line: string) => {
    const lower = line.toLowerCase();
    if (lower.includes('error') || lower.includes('fatal') || lower.includes('failed')) return 'text-status-error';
    if (lower.includes('warn')) return 'text-status-warning';
    if (lower.includes('success') || lower.includes('passed') || lower.includes('live at')) return 'text-status-success';
    if (lower.includes('detected runtime') || lower.includes('cloning')) return 'text-status-info';
    return 'text-content-secondary';
  };

  return (
    <div className={fullscreen ? 'fixed inset-0 z-50 bg-surface-primary p-4' : ''}>
      <div className="flex items-center justify-between mb-4">
        <div>
          <p className="text-[11px] uppercase tracking-[0.22em] text-content-tertiary font-semibold">Observability</p>
          <h1 className="text-2xl font-semibold text-content-primary">Logs</h1>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={fetchLogs}
            className="p-2 rounded-lg text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors border border-transparent hover:border-border-default"
            title="Refresh logs"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            onClick={() => setFullscreen(!fullscreen)}
            className="p-2 rounded-lg text-content-tertiary hover:text-content-primary hover:bg-surface-tertiary transition-colors border border-transparent hover:border-border-default"
          >
            {fullscreen ? <Minimize2 className="w-4 h-4" /> : <Maximize2 className="w-4 h-4" />}
          </button>
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center gap-3 mb-3">
        {/* Log type toggle */}
        <div className="flex rounded-lg border border-border-default overflow-hidden bg-surface-secondary">
          <button
            onClick={() => setLogType('runtime')}
            className={`px-3 py-2 text-sm font-medium transition-colors ${
              logType === 'runtime'
                ? 'bg-surface-tertiary text-content-primary shadow-inner'
                : 'text-content-tertiary hover:text-content-secondary'
            }`}
          >
            Runtime
          </button>
          <button
            onClick={() => setLogType('deploy')}
            className={`px-3 py-2 text-sm font-medium transition-colors border-l border-border-default ${
              logType === 'deploy'
                ? 'bg-surface-tertiary text-content-primary shadow-inner'
                : 'text-content-tertiary hover:text-content-secondary'
            }`}
          >
            Deploy
          </button>
        </div>

        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-content-tertiary" />
          <input
            type="text"
            placeholder="Search logs..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-surface-secondary border border-border-default rounded-lg pl-9 pr-3 py-2 text-sm text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all font-mono shadow-[0_1px_2px_rgba(15,23,42,0.05)]"
          />
        </div>
        {logType === 'runtime' && (
          <button
            onClick={() => setIsLive(!isLive)}
            className={`inline-flex items-center gap-1.5 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
              isLive
                ? 'bg-status-success-bg text-status-success border border-status-success/20'
                : 'bg-surface-tertiary text-content-secondary border border-border-default'
            }`}
          >
            <span className={`w-2 h-2 rounded-full ${isLive ? 'bg-status-success animate-pulse-dot' : 'bg-content-tertiary'}`} />
            Live
          </button>
        )}
      </div>

      {/* Log viewer */}
      <div
        ref={containerRef}
        className={`rounded-xl border border-border-default overflow-auto font-mono text-xs bg-surface-secondary shadow-inner ${
          fullscreen ? 'h-[calc(100vh-140px)]' : 'h-[600px]'
        }`}
      >
        {logType === 'runtime' ? (
          filtered.length === 0 ? (
            <div className="flex items-center justify-center h-full text-content-tertiary text-sm">
              {loading ? 'Loading logs...' : search ? 'No matching log entries' : 'No logs available. Deploy a service to see logs.'}
            </div>
          ) : (
            <div className="p-3 space-y-0">
              {filtered.map((entry, i) => (
                <div key={i} className="flex items-start gap-2 py-0.5 hover:bg-surface-tertiary px-2 -mx-2 rounded group transition-colors">
                  {levelIcon(entry.level)}
                  <span className="text-content-tertiary flex-shrink-0 select-none text-[11px]">
                    {formatTime(entry.timestamp)}
                  </span>
                  <span className="text-brand/60 flex-shrink-0 text-[11px]">
                    [{entry.instance_id}]
                  </span>
                  <span className={`${levelColor(entry.level)} break-all leading-relaxed`}>
                    {entry.message}
                  </span>
                </div>
              ))}
            </div>
          )
        ) : (
          filteredDeployLogs.length === 0 ? (
            <div className="flex items-center justify-center h-full text-content-tertiary text-sm">
              {loading ? 'Loading deploy logs...' : 'No deploy logs available.'}
            </div>
          ) : (
            <div className="p-3 space-y-4">
              {filteredDeployLogs.map((deploy) => (
                <div key={deploy.id}>
                  <div className="flex items-center gap-2 mb-2 px-2">
                    <span className={`inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium ${
                      deploy.status === 'live'
                        ? 'bg-status-success-bg text-status-success'
                        : deploy.status === 'failed'
                        ? 'bg-status-error-bg text-status-error'
                        : 'bg-status-warning-bg text-status-warning'
                    }`}>
                      {deploy.status}
                    </span>
                    <span className="text-content-tertiary text-[11px]">
                      {deploy.started_at ? formatTime(deploy.started_at) : ''}
                    </span>
                    <span className="text-content-tertiary text-[11px]">
                      {deploy.id.slice(0, 8)}
                    </span>
                  </div>
                  {deploy.log ? (
                    <div className="space-y-0">
                      {deploy.log.split('\n').filter(Boolean).map((line, j) => (
                        <div key={j} className="py-0.5 hover:bg-surface-tertiary px-2 -mx-2 rounded transition-colors">
                          <span className={`${deployLineColor(line)} break-all leading-relaxed`}>
                            {line}
                          </span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="px-2 text-content-tertiary">No build output</div>
                  )}
                </div>
              ))}
            </div>
          )
        )}
      </div>

      {/* Keyboard shortcuts */}
      <div className="flex items-center gap-4 mt-2 text-[11px] text-content-tertiary">
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">M</kbd> Fullscreen</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">/</kbd> Focus search</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">Home</kbd> Jump to start</span>
        <span><kbd className="px-1 py-0.5 bg-surface-tertiary rounded border border-border-default">End</kbd> Jump to end</span>
      </div>
    </div>
  );
}
