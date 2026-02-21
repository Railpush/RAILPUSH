import { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  ArrowLeft,
  BookOpen,
  Rocket,
  Globe,
  Database,
  Key,
  Layers,
  GitBranch,
  Terminal,
  FileText,
  Settings,
  Clock,
  Lock,
  HardDrive,
  Cpu,
  ArrowRight,
  Copy,
  Check,
  ChevronRight,
  Zap,
  Code,
  Shield,
  Box,
  Plug
} from 'lucide-react';
import { SEO } from '../components/SEO';
import { LogoMark } from '../components/Logo';

// ─── Mac Window ───────────────────────────────────────────────────
function MacWindow({ title, children, className = '' }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={`rounded-xl border border-border-default bg-surface-secondary/80 overflow-hidden shadow-2xl shadow-black/30 ${className}`}>
      <div className="flex items-center gap-2 px-4 py-2.5 border-b border-border-subtle bg-surface-tertiary/50">
        <div className="w-3 h-3 rounded-full bg-[#FF5F57]" />
        <div className="w-3 h-3 rounded-full bg-[#FEBC2E]" />
        <div className="w-3 h-3 rounded-full bg-[#28C840]" />
        <span className="ml-2 text-xs text-content-tertiary font-mono">{title}</span>
      </div>
      {children}
    </div>
  );
}

// ─── Code Block ───────────────────────────────────────────────────
function CodeBlock({ code, language = 'yaml', filename }: { code: string; language?: string; filename?: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <MacWindow title={filename || language}>
      <div className="relative group">
        <button
          onClick={handleCopy}
          className="absolute top-3 right-3 p-1.5 rounded-md bg-surface-tertiary/80 border border-border-subtle text-content-tertiary hover:text-content-primary transition-all opacity-0 group-hover:opacity-100"
        >
          {copied ? <Check className="w-3.5 h-3.5 text-status-success" /> : <Copy className="w-3.5 h-3.5" />}
        </button>
        <pre className="p-4 overflow-x-auto text-sm font-mono leading-relaxed">
          <code className="text-content-secondary">{code}</code>
        </pre>
      </div>
    </MacWindow>
  );
}

// ─── Terminal Block ───────────────────────────────────────────────
function TerminalBlock({ lines }: { lines: { text: string; color?: string }[] }) {
  return (
    <MacWindow title="terminal">
      <div className="p-4 font-mono text-sm leading-loose">
        {lines.map((line, i) => (
          <div key={i} className={line.color || 'text-content-secondary'}>{line.text}</div>
        ))}
      </div>
    </MacWindow>
  );
}

// ─── Sidebar Section ──────────────────────────────────────────────
const sections = [
  { id: 'getting-started', label: 'Getting Started', icon: Rocket },
  { id: 'services', label: 'Services', icon: Globe },
  { id: 'databases', label: 'PostgreSQL', icon: Database },
  { id: 'keyvalue', label: 'Key Value (Redis)', icon: Key },
  { id: 'blueprints', label: 'Blueprints', icon: Layers },
  { id: 'blueprint-spec', label: 'Blueprint Spec', icon: FileText },
  { id: 'env-vars', label: 'Environment Variables', icon: Lock },
  { id: 'domains', label: 'Custom Domains', icon: Globe },
  { id: 'deploys', label: 'Deploys', icon: GitBranch },
  { id: 'docker', label: 'Docker Deploys', icon: Box },
  { id: 'static-sites', label: 'Static Sites', icon: FileText },
  { id: 'cron-jobs', label: 'Cron Jobs', icon: Clock },
  { id: 'disks', label: 'Persistent Disks', icon: HardDrive },
  { id: 'scaling', label: 'Scaling', icon: Cpu },
  { id: 'cli', label: 'CLI & API', icon: Terminal },
  { id: 'mcp', label: 'MCP Server', icon: Plug },
];

// ─── Docs Component ───────────────────────────────────────────────
export function Docs() {
  const navigate = useNavigate();
  const location = useLocation();
  const [activeSection, setActiveSection] = useState('getting-started');
  const contentRef = useRef<HTMLDivElement>(null);

  // Scroll tracking
  useEffect(() => {
    const hash = location.hash.replace('#', '');
    if (hash && sections.some(s => s.id === hash)) {
      setActiveSection(hash);
      const el = document.getElementById(hash);
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  }, [location.hash]);

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setActiveSection(entry.target.id);
          }
        }
      },
      { rootMargin: '-80px 0px -60% 0px', threshold: 0.1 }
    );
    sections.forEach(s => {
      const el = document.getElementById(s.id);
      if (el) observer.observe(el);
    });
    return () => observer.disconnect();
  }, []);

  const scrollTo = (id: string) => {
    setActiveSection(id);
    const el = document.getElementById(id);
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    window.history.replaceState(null, '', `#${id}`);
  };

  return (
    <div className="min-h-screen bg-surface-primary text-content-primary page-shell">
      <SEO
        title="Documentation — RailPush"
        description="How RailPush works: ship from Git, wire domains, manage Postgres and Redis, schedule jobs, and define everything as code with railpush.yaml."
        canonical="https://railpush.com/docs"
      />
      {/* ── Top Nav ─────────────────────────────────────── */}
      <nav className="fixed top-0 inset-x-0 z-50 bg-surface-primary/80 backdrop-blur-xl border-b border-border-default">
        <div className="max-w-[1400px] mx-auto px-6 h-14 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={() => navigate('/')}
              className="flex items-center gap-2.5 hover:opacity-80 transition-opacity"
            >
              <LogoMark size={28} />
              <span className="text-base font-semibold tracking-tight">RailPush</span>
            </button>
            <ChevronRight className="w-4 h-4 text-content-tertiary" />
            <span className="text-sm font-medium text-content-secondary flex items-center gap-1.5">
              <BookOpen className="w-3.5 h-3.5" />
              Documentation
            </span>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={() => navigate('/')}
              className="text-sm font-medium text-content-secondary hover:text-content-primary transition-colors flex items-center gap-1.5"
            >
              <ArrowLeft className="w-3.5 h-3.5" />
              Back
            </button>
          </div>
        </div>
      </nav>

      <div className="flex max-w-[1400px] mx-auto pt-14">
        {/* ── Sidebar ──────────────────────────────── */}
        <aside className="hidden lg:block w-64 shrink-0 sticky top-14 h-[calc(100vh-3.5rem)] overflow-y-auto border-r border-border-default py-6 px-4">
          <nav className="space-y-0.5">
            {sections.map(s => (
              <button
                key={s.id}
                onClick={() => scrollTo(s.id)}
                className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-all ${
                  activeSection === s.id
                    ? 'bg-brand/10 text-brand font-medium'
                    : 'text-content-secondary hover:text-content-primary hover:bg-surface-tertiary/50'
                }`}
              >
                <s.icon className="w-4 h-4 shrink-0" />
                {s.label}
              </button>
            ))}
          </nav>
        </aside>

        {/* ── Content ─────────────────────────────── */}
        <main ref={contentRef} className="flex-1 min-w-0 px-8 lg:px-16 py-10 pb-32">
          <div className="max-w-3xl">

            {/* ── Getting Started ─────────────────────── */}
            <section id="getting-started" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Rocket className="w-5 h-5 text-brand" />
                </div>
                <h1 className="text-3xl font-bold tracking-tight">Getting Started</h1>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-8">
                RailPush keeps deploys, data, and routing under one roof. Connect GitHub, describe what you need, and we’ll build, run, and front it with defaults you can override.
              </p>

              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
                {[
                  { icon: GitBranch, title: 'Connect GitHub', desc: 'Link your account and pick a repo' },
                  { icon: Zap, title: 'Push Code', desc: 'Every push builds, checks health, and rolls out' },
                  { icon: Globe, title: 'Go Live', desc: 'Instant URL with TLS and preview links' },
                ].map(s => (
                  <div key={s.title} className="rounded-xl border border-border-default bg-surface-secondary/50 p-5">
                    <s.icon className="w-5 h-5 text-brand mb-3" />
                    <div className="text-sm font-semibold mb-1">{s.title}</div>
                    <div className="text-xs text-content-secondary">{s.desc}</div>
                  </div>
                ))}
              </div>

              <h3 className="text-lg font-semibold mb-3">Quick Start</h3>
              <p className="text-sm text-content-secondary mb-4">
                Create your first web service by connecting a GitHub repository. RailPush auto-detects your runtime and generates an optimized Dockerfile.
              </p>
              <div className="space-y-3 text-sm text-content-secondary mb-6">
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">1</span> <span>Sign up with GitHub or email at <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">railpush.com</code></span></div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">2</span> Click <strong className="text-content-primary">New</strong> &rarr; <strong className="text-content-primary">Web Service</strong></div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">3</span> Select your repository and branch</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">4</span> Configure build & start commands (auto-detected if possible)</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">5</span> Click <strong className="text-content-primary">Create Service</strong> — your app deploys automatically</div>
              </div>

              <h3 className="text-lg font-semibold mb-3">Supported Runtimes</h3>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 mb-6">
                {['Node.js', 'Python', 'Go', 'Ruby', 'Rust', 'Elixir', 'Java', 'Docker'].map(r => (
                  <div key={r} className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border-default bg-surface-secondary/30 text-sm">
                    <Code className="w-3.5 h-3.5 text-brand" />
                    {r}
                  </div>
                ))}
              </div>

              <TerminalBlock lines={[
                { text: '$ git push origin main', color: 'text-content-primary' },
                { text: '> Webhook received — starting build', color: 'text-content-secondary' },
                { text: '> Detected runtime: Node.js 20', color: 'text-status-info' },
                { text: '> npm install — 1,247 packages in 4.2s', color: 'text-content-secondary' },
                { text: '> Build succeeded', color: 'text-status-success' },
                { text: '> Health check passed (200 OK)', color: 'text-status-success' },
                { text: '> Live at https://my-app.railpush.com', color: 'text-status-success font-semibold' },
              ]} />
            </section>

            {/* ── Services ────────────────────────────────── */}
            <section id="services" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Globe className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Services</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Services are the core building block. Each service runs a single process from your repository in an isolated Docker container.
              </p>

              <h3 className="text-lg font-semibold mb-3">Service Types</h3>
              <div className="space-y-3 mb-8">
                {[
                  { type: 'Web Service', tag: 'web', desc: 'HTTP services with a public URL. Automatically gets a subdomain and TLS certificate.', color: 'text-brand' },
                  { type: 'Private Service', tag: 'pserv', desc: 'Internal services only accessible within your private network. No public URL.', color: 'text-brand-purple' },
                  { type: 'Background Worker', tag: 'worker', desc: 'Long-running processes without HTTP endpoints. Queue consumers, data processors, etc.', color: 'text-status-warning' },
                  { type: 'Cron Job', tag: 'cron', desc: 'Scheduled tasks that run on a cron expression. Spins up, runs, and shuts down.', color: 'text-status-info' },
                  { type: 'Static Site', tag: 'static', desc: 'Pre-rendered sites served via nginx with CDN-optimized caching headers. Build once, deploy instantly.', color: 'text-status-success' },
                ].map(s => (
                  <div key={s.tag} className="flex items-start gap-4 p-4 rounded-xl border border-border-default bg-surface-secondary/30">
                    <code className={`px-2 py-0.5 rounded text-xs font-mono font-semibold bg-surface-tertiary ${s.color}`}>{s.tag}</code>
                    <div>
                      <div className="text-sm font-semibold mb-0.5">{s.type}</div>
                      <div className="text-sm text-content-secondary">{s.desc}</div>
                    </div>
                  </div>
                ))}
              </div>

              <h3 className="text-lg font-semibold mb-3">Configuration</h3>
              <p className="text-sm text-content-secondary mb-4">
                Each service has these core settings:
              </p>
              <div className="overflow-x-auto mb-6">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 text-content-primary font-semibold">Setting</th>
                      <th className="text-left py-2 pr-4 text-content-primary font-semibold">Description</th>
                      <th className="text-left py-2 text-content-primary font-semibold">Default</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">buildCommand</td><td className="py-2 pr-4">Build step (e.g., <code className="text-xs bg-surface-tertiary px-1 rounded">npm run build</code>)</td><td className="py-2">Auto-detected</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">startCommand</td><td className="py-2 pr-4">Process entry point</td><td className="py-2">Auto-detected</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">port</td><td className="py-2 pr-4">HTTP port your app listens on</td><td className="py-2">10000</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">healthCheckPath</td><td className="py-2 pr-4">Endpoint for health checks</td><td className="py-2">/</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">plan</td><td className="py-2 pr-4">Resource allocation tier</td><td className="py-2">starter</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">numInstances</td><td className="py-2 pr-4">Number of running instances</td><td className="py-2">1</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs text-brand">autoDeploy</td><td className="py-2 pr-4">Auto-deploy on push</td><td className="py-2">true</td></tr>
                  </tbody>
                </table>
              </div>
            </section>

            {/* ── Databases ───────────────────────────────── */}
            <section id="databases" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-[#336791]/10 flex items-center justify-center">
                  <Database className="w-5 h-5 text-[#336791]" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">PostgreSQL</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Fully managed PostgreSQL databases provisioned in seconds. Each database runs in its own Docker container with persistent storage.
              </p>

              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
                {[
                  { label: 'Versions', value: 'PG 13 - 18' },
                  { label: 'Provisioning', value: '< 30 seconds' },
                  { label: 'Storage', value: 'Persistent volumes' },
                ].map(s => (
                  <div key={s.label} className="rounded-xl border border-border-default bg-surface-secondary/30 p-4 text-center">
                    <div className="text-lg font-bold text-content-primary">{s.value}</div>
                    <div className="text-xs text-content-secondary mt-1">{s.label}</div>
                  </div>
                ))}
              </div>

              <h3 className="text-lg font-semibold mb-3">Connection</h3>
              <p className="text-sm text-content-secondary mb-4">
                Connection details are available in the database dashboard. Use them as environment variables in your services:
              </p>
              <CodeBlock language="bash" filename=".env" code={`DATABASE_URL=postgres://mydb:password@localhost:5432/mydb

# Or use individual variables:
DB_HOST=localhost
DB_PORT=5432
DB_USER=mydb
DB_PASSWORD=password
DB_NAME=mydb`} />
            </section>

            {/* ── Key Value ───────────────────────────────── */}
            <section id="keyvalue" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-[#DC382D]/10 flex items-center justify-center">
                  <Key className="w-5 h-5 text-[#DC382D]" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Key Value (Redis)</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Managed Redis instances for caching, sessions, queues, and real-time features. Provisioned with persistent storage and password authentication.
              </p>

              <h3 className="text-lg font-semibold mb-3">Eviction Policies</h3>
              <div className="grid grid-cols-2 gap-2 mb-6">
                {[
                  { policy: 'allkeys-lru', desc: 'Evict least recently used keys (default)' },
                  { policy: 'volatile-lru', desc: 'Evict LRU keys with TTL set' },
                  { policy: 'allkeys-random', desc: 'Evict random keys' },
                  { policy: 'noeviction', desc: 'Return error when memory full' },
                ].map(p => (
                  <div key={p.policy} className="px-3 py-2 rounded-lg border border-border-default bg-surface-secondary/30">
                    <code className="text-xs font-mono text-[#DC382D]">{p.policy}</code>
                    <div className="text-xs text-content-secondary mt-0.5">{p.desc}</div>
                  </div>
                ))}
              </div>

              <CodeBlock language="bash" filename="connection" code={`REDIS_URL=redis://:password@localhost:6379`} />
            </section>

            {/* ── Blueprints ──────────────────────────────── */}
            <section id="blueprints" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand-purple/10 flex items-center justify-center">
                  <Layers className="w-5 h-5 text-brand-purple" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Blueprints</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Blueprints are Infrastructure as Code for RailPush. Define your entire stack — services, databases, Redis, environment variables — in a single <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">railpush.yaml</code> file. Push to deploy everything at once.
              </p>

              <div className="rounded-xl border border-brand-purple/30 bg-brand-purple/5 p-5 mb-8">
                <div className="flex items-start gap-3">
                  <Zap className="w-5 h-5 text-brand-purple shrink-0 mt-0.5" />
                  <div>
                    <div className="text-sm font-semibold mb-1">All-or-nothing deploys</div>
                    <div className="text-sm text-content-secondary">
                      Blueprint syncs are atomic. If any resource fails to create, the entire sync is aborted. No partial infrastructure.
                    </div>
                  </div>
                </div>
              </div>

              <h3 className="text-lg font-semibold mb-3">How it works</h3>
              <div className="space-y-3 text-sm text-content-secondary mb-8">
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand-purple/10 text-brand-purple text-xs font-bold flex items-center justify-center shrink-0">1</span> Add a <code className="text-xs bg-surface-tertiary px-1 rounded">railpush.yaml</code> to your repository root</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand-purple/10 text-brand-purple text-xs font-bold flex items-center justify-center shrink-0">2</span> Create a Blueprint in the dashboard and point it to your repo</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand-purple/10 text-brand-purple text-xs font-bold flex items-center justify-center shrink-0">3</span> RailPush clones your repo, reads the YAML, and provisions everything</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand-purple/10 text-brand-purple text-xs font-bold flex items-center justify-center shrink-0">4</span> Databases are created first, then services with resolved env vars</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand-purple/10 text-brand-purple text-xs font-bold flex items-center justify-center shrink-0">5</span> All services are deployed simultaneously after resources are ready</div>
              </div>

              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-api
    runtime: node
    buildCommand: npm install && npm run build
    startCommand: npm start
    port: 3000
    plan: starter
    numInstances: 1
    healthCheckPath: /healthz
    autoDeploy: true
    envVars:
      - key: NODE_ENV
        value: production
      - key: DATABASE_URL
        fromDatabase:
          name: my-db
          property: connectionString
      - key: REDIS_URL
        fromService:
          name: my-cache
          type: keyvalue
          property: connectionString
      - key: SECRET_KEY
        generateValue: true
      - fromGroup: shared-config

  - type: worker
    name: my-worker
    runtime: node
    buildCommand: npm install
    startCommand: npm run worker
    envVars:
      - key: DATABASE_URL
        fromDatabase:
          name: my-db
          property: connectionString

  - type: cron
    name: nightly-cleanup
    runtime: node
    buildCommand: npm install
    startCommand: node scripts/cleanup.js
    schedule: "0 3 * * *"
    plan: starter

  - type: static
    name: my-frontend
    buildCommand: npm install && npm run build
    staticPublishPath: ./dist

databases:
  - name: my-db
    plan: starter
    postgresMajorVersion: 16

keyValues:
  - name: my-cache
    plan: starter
    maxmemoryPolicy: allkeys-lru

envVarGroups:
  - name: shared-config
    envVars:
      - key: APP_ENV
        value: production`} />
            </section>

            {/* ── Blueprint Spec ──────────────────────────── */}
            <section id="blueprint-spec" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <FileText className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Blueprint Spec Reference</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Complete reference for the <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">railpush.yaml</code> file format.
              </p>

              <h3 className="text-lg font-semibold mb-3">Root Fields</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Field</th>
                      <th className="text-left py-2 pr-4 font-semibold">Type</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">services</td><td className="py-2 pr-4">Array</td><td className="py-2">Web services, workers, cron jobs, static sites</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">databases</td><td className="py-2 pr-4">Array</td><td className="py-2">PostgreSQL database instances</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">keyValues</td><td className="py-2 pr-4">Array</td><td className="py-2">Redis key-value stores</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs text-brand">envVarGroups</td><td className="py-2 pr-4">Array</td><td className="py-2">Shared environment variable groups</td></tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mb-3">Service Fields</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Field</th>
                      <th className="text-left py-2 pr-4 font-semibold">Type</th>
                      <th className="text-left py-2 pr-4 font-semibold">Required</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    {[
                      ['name', 'String', 'Yes', 'Unique service name within the blueprint'],
                      ['type', 'String', 'No', 'web (default), pserv, worker, cron, static'],
                      ['runtime', 'String', 'Yes*', 'node, python, go, ruby, rust, docker, elixir, static, image'],
                      ['repo', 'String', 'No', 'Repository URL (defaults to blueprint repo)'],
                      ['branch', 'String', 'No', 'Git branch (defaults to blueprint branch)'],
                      ['buildCommand', 'String', 'No', 'Build command'],
                      ['startCommand', 'String', 'No', 'Start command (ignored for static sites)'],
                      ['dockerCommand', 'String', 'No', 'Docker CMD override'],
                      ['dockerfilePath', 'String', 'No', 'Custom Dockerfile path'],
                      ['dockerContext', 'String', 'No', 'Docker build context directory'],
                      ['rootDir', 'String', 'No', 'Root directory for monorepos'],
                      ['port', 'Int', 'No', 'HTTP port (default: 10000, web/pserv only)'],
                      ['plan', 'String', 'No', 'free, starter (default), standard, pro'],
                      ['numInstances', 'Int', 'No', 'Instance count (default: 1, 0 = suspended)'],
                      ['healthCheckPath', 'String', 'No', 'Health check endpoint (web/pserv only)'],
                      ['preDeployCommand', 'String', 'No', 'Run before deploy (migrations, etc.)'],
                      ['staticPublishPath', 'String', 'No', 'Build output dir (required for static)'],
                      ['schedule', 'String', 'No', 'Cron expression (required for cron)'],
                      ['autoDeploy', 'Bool', 'No', 'Auto-deploy on push (default: true)'],
                      ['envVars', 'Array', 'No', 'Environment variables'],
                      ['domains', 'Array', 'No', 'Custom domain strings (web/static only)'],
                      ['disk', 'Object', 'No', 'Persistent disk: { name, mountPath, sizeGB }'],
                      ['buildFilter', 'Object', 'No', 'Build triggers: { paths, ignoredPaths }'],
                      ['image', 'Object', 'No', 'Prebuilt image: { url }'],
                      ['buildInclude', 'Array', 'No', 'Whitelist files for build context (.dockerignore)'],
                      ['buildExclude', 'Array', 'No', 'Exclude files from build context (.dockerignore)'],
                      ['baseImage', 'String', 'No', 'Override base image for auto-generated Dockerfile'],
                    ].map(([field, type, req, desc]) => (
                      <tr key={field} className="border-b border-border-subtle">
                        <td className="py-2 pr-4 font-mono text-xs text-brand">{field}</td>
                        <td className="py-2 pr-4">{type}</td>
                        <td className="py-2 pr-4">{req}</td>
                        <td className="py-2">{desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mb-3">Environment Variable Types</h3>
              <div className="space-y-3 mb-8">
                <CodeBlock filename="envVars examples" code={`envVars:
  # Static value
  - key: NODE_ENV
    value: production

  # Auto-generated secret (32 chars)
  - key: SECRET_KEY
    generateValue: true

  # Reference a database property
  - key: DATABASE_URL
    fromDatabase:
      name: my-db
      property: connectionString  # or: host, port, user, password, database

  # Reference another service
  - key: API_HOST
    fromService:
      name: my-api
      type: web
      property: host  # or: port, hostport, connectionString

  # Copy an env var from another service
  - key: SHARED_SECRET
    fromService:
      name: my-api
      type: web
      envVarKey: SECRET_KEY

  # Import a shared env group
  - fromGroup: shared-config`} />
              </div>

              <h3 className="text-lg font-semibold mb-3">Database Fields</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Field</th>
                      <th className="text-left py-2 pr-4 font-semibold">Type</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">name</td><td className="py-2 pr-4">String</td><td className="py-2">Database identifier (required, unique)</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">plan</td><td className="py-2 pr-4">String</td><td className="py-2">free (1Gi), starter (5Gi), standard (20Gi), pro (100Gi). Default: starter</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">postgresMajorVersion</td><td className="py-2 pr-4">Int</td><td className="py-2">PostgreSQL version (default: 16)</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">databaseName</td><td className="py-2 pr-4">String</td><td className="py-2">Custom DB name (defaults to resource name)</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">user</td><td className="py-2 pr-4">String</td><td className="py-2">Custom username (defaults to resource name)</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">initScript</td><td className="py-2 pr-4">String</td><td className="py-2">Inline SQL to run once on first provision (e.g. CREATE EXTENSION IF NOT EXISTS pgcrypto)</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs text-brand">initScriptPath</td><td className="py-2 pr-4">String</td><td className="py-2">Path to a .sql file in the repo (relative to root). Read at sync time, run once on first provision.</td></tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mb-3">Disk Configuration</h3>
              <CodeBlock filename="disk example" code={`services:
  - type: web
    name: my-app
    runtime: node
    disk:
      name: data
      mountPath: /var/data
      sizeGB: 10`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Image Deploy</h3>
              <CodeBlock filename="image deploy" code={`services:
  - type: web
    name: my-app
    image:
      url: docker.io/myorg/myapp:latest
    port: 3000`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Key-Value (Redis) Fields</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Field</th>
                      <th className="text-left py-2 pr-4 font-semibold">Type</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">name</td><td className="py-2 pr-4">String</td><td className="py-2">Redis instance identifier (required, unique)</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs text-brand">plan</td><td className="py-2 pr-4">String</td><td className="py-2">free (1Gi), starter (2Gi), standard (5Gi), pro (10Gi). Default: starter</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs text-brand">maxmemoryPolicy</td><td className="py-2 pr-4">String</td><td className="py-2">Redis eviction policy (default: allkeys-lru)</td></tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mt-8 mb-3">Resource Limits by Plan</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Plan</th>
                      <th className="text-left py-2 pr-4 font-semibold">CPU</th>
                      <th className="text-left py-2 pr-4 font-semibold">Memory</th>
                      <th className="text-left py-2 font-semibold">Price</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">free</td><td className="py-2 pr-4">100m - 250m</td><td className="py-2 pr-4">256Mi - 512Mi</td><td className="py-2">$0/mo</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">starter</td><td className="py-2 pr-4">500m - 1</td><td className="py-2 pr-4">512Mi - 1Gi</td><td className="py-2">$7/mo</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">standard</td><td className="py-2 pr-4">1 - 2</td><td className="py-2 pr-4">2Gi - 4Gi</td><td className="py-2">$25/mo</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs">pro</td><td className="py-2 pr-4">2 - 4</td><td className="py-2 pr-4">4Gi - 8Gi</td><td className="py-2">$85/mo</td></tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mt-8 mb-3">Defaults Applied</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Field</th>
                      <th className="text-left py-2 font-semibold">Default</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">type</td><td className="py-2">web</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">port</td><td className="py-2">10000</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">plan</td><td className="py-2">starter</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">numInstances</td><td className="py-2">1</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">autoDeploy</td><td className="py-2">true</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">disk.sizeGB</td><td className="py-2">10</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4 font-mono text-xs">postgresMajorVersion</td><td className="py-2">16</td></tr>
                    <tr><td className="py-2 pr-4 font-mono text-xs">maxmemoryPolicy</td><td className="py-2">allkeys-lru</td></tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mt-8 mb-3">Build Filter</h3>
              <CodeBlock filename="build filter example" code={`services:
  - type: web
    name: my-api
    runtime: node
    buildFilter:
      paths:
        - src/**
        - package.json
      ignoredPaths:
        - docs/**
        - "*.md"`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Per-Service Build Context</h3>
              <p className="text-sm text-content-secondary mb-4">
                Control which files each service sees during build. Useful for monorepos where multiple services share a directory.
              </p>
              <CodeBlock filename="buildInclude (whitelist)" code={`services:
  # Only include specific files in the build
  - type: worker
    name: sync-worker
    runtime: python
    buildInclude:
      - worker.py
      - requirements.txt
      - schema.sql`} />
              <div className="mt-4" />
              <CodeBlock filename="buildExclude (blacklist)" code={`services:
  # Exclude specific files from the build
  - type: web
    name: viewer
    runtime: node
    buildExclude:
      - worker.py
      - sync.log
      - "*.md"`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Custom Base Image</h3>
              <p className="text-sm text-content-secondary mb-4">
                Override the default base image for auto-generated Dockerfiles. Useful for multi-runtime builds (e.g. Python + Node.js).
              </p>
              <CodeBlock filename="baseImage example" code={`services:
  - type: web
    name: fullstack-app
    runtime: python
    baseImage: nikolaik/python-nodejs:python3.12-nodejs20
    buildCommand: pip install -r requirements.txt && npm install && npm run build
    startCommand: uvicorn api:app --host 0.0.0.0 --port $PORT`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Database Init Script</h3>
              <p className="text-sm text-content-secondary mb-4">
                Run SQL once when a managed database is first provisioned. Use <code className="text-xs bg-bg-secondary px-1 py-0.5 rounded">initScript</code> for
                short inline SQL, or <code className="text-xs bg-bg-secondary px-1 py-0.5 rounded">initScriptPath</code> for
                a full schema file from the repo. Both can be used together (inline runs first).
              </p>
              <CodeBlock filename="inline SQL" code={`databases:
  - name: my-db
    plan: starter
    initScript: CREATE EXTENSION IF NOT EXISTS pgcrypto;`} />
              <CodeBlock filename="SQL file from repo" code={`databases:
  - name: my-db
    plan: starter
    initScriptPath: db/schema.sql`} />
            </section>

            {/* ── Environment Variables ────────────────────── */}
            <section id="env-vars" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Lock className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Environment Variables</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Securely manage configuration and secrets. All values are encrypted at rest using AES-256-GCM. Env vars are injected into your container at runtime.
              </p>

              <div className="rounded-xl border border-brand/20 bg-brand/5 p-5 mb-6">
                <div className="flex items-start gap-3">
                  <Shield className="w-5 h-5 text-brand shrink-0 mt-0.5" />
                  <div>
                    <div className="text-sm font-semibold mb-1">Encrypted at rest</div>
                    <div className="text-sm text-content-secondary">
                      All environment variable values are encrypted with AES-256-GCM before storage. They are only decrypted when injected into containers.
                    </div>
                  </div>
                </div>
              </div>

              <h3 className="text-lg font-semibold mb-3">Env Groups</h3>
              <p className="text-sm text-content-secondary mb-4">
                Env groups let you share configuration across multiple services. Define them in your blueprint or create them in the dashboard. When a group is updated, all linked services receive the changes.
              </p>
              <CodeBlock filename="railpush.yaml" code={`envVarGroups:
  - name: shared-config
    envVars:
      - key: LOG_LEVEL
        value: info
      - key: SENTRY_DSN
        value: https://abc@sentry.io/123

services:
  - type: web
    name: my-api
    envVars:
      - fromGroup: shared-config
      - key: PORT
        value: "3000"`} />
            </section>

            {/* ── Custom Domains ──────────────────────────── */}
            <section id="domains" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Globe className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Custom Domains</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Every web service gets a free subdomain at <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">service-name.railpush.com</code>. You can also add custom domains with automatic TLS provisioning.
              </p>

              <h3 className="text-lg font-semibold mb-3">Setup</h3>
              <div className="space-y-3 text-sm text-content-secondary mb-6">
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">1</span> Add your domain in the service's Networking tab</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">2</span> Point your DNS CNAME record to your service's <code className="text-xs bg-surface-tertiary px-1 rounded">.railpush.com</code> subdomain</div>
                <div className="flex gap-3"><span className="w-6 h-6 rounded-full bg-brand/10 text-brand text-xs font-bold flex items-center justify-center shrink-0">3</span> TLS certificate is provisioned automatically via Let's Encrypt</div>
              </div>

              <h3 className="text-lg font-semibold mb-3">Blueprint Domains</h3>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-app
    runtime: node
    domains:
      - myapp.com
      - www.myapp.com`} />
            </section>

            {/* ── Deploys ─────────────────────────────────── */}
            <section id="deploys" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <GitBranch className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Deploys</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Every deploy follows this pipeline: clone &rarr; detect runtime &rarr; build image &rarr; run container &rarr; health check &rarr; route traffic &rarr; live.
              </p>

              <MacWindow title="deploy pipeline">
                <div className="p-6">
                  <div className="flex items-center gap-3 flex-wrap">
                    {['Clone', 'Detect', 'Build', 'Deploy', 'Health Check', 'Route', 'Live'].map((step, i) => (
                      <div key={step} className="flex items-center gap-3">
                        <div className={`px-3 py-1.5 rounded-lg text-xs font-medium border ${
                          i === 6 ? 'bg-status-success/10 border-status-success/30 text-status-success' : 'bg-surface-tertiary border-border-default text-content-primary'
                        }`}>{step}</div>
                        {i < 6 && <ArrowRight className="w-3.5 h-3.5 text-content-tertiary" />}
                      </div>
                    ))}
                  </div>
                </div>
              </MacWindow>

              <h3 className="text-lg font-semibold mt-8 mb-3">Deploy Triggers</h3>
              <div className="space-y-2 text-sm text-content-secondary">
                <div className="flex items-center gap-2"><code className="px-2 py-0.5 rounded bg-surface-tertiary text-xs font-mono text-brand">manual</code> Triggered from the dashboard</div>
                <div className="flex items-center gap-2"><code className="px-2 py-0.5 rounded bg-surface-tertiary text-xs font-mono text-brand">auto</code> Triggered by a git push</div>
                <div className="flex items-center gap-2"><code className="px-2 py-0.5 rounded bg-surface-tertiary text-xs font-mono text-brand">rollback</code> Reverts to a previous deploy's image</div>
                <div className="flex items-center gap-2"><code className="px-2 py-0.5 rounded bg-surface-tertiary text-xs font-mono text-brand">blueprint</code> Triggered by a blueprint sync</div>
              </div>
            </section>

            {/* ── Docker Deploys ──────────────────────────── */}
            <section id="docker" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Box className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Docker Deploys</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Bring your own Dockerfile or deploy a prebuilt image. RailPush gives you full Docker flexibility.
              </p>

              <h3 className="text-lg font-semibold mb-3">Custom Dockerfile</h3>
              <p className="text-sm text-content-secondary mb-4">
                If your repo has a <code className="text-xs bg-surface-tertiary px-1 rounded">Dockerfile</code>, RailPush uses it automatically. You can customize the path:
              </p>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-app
    runtime: docker
    dockerfilePath: ./docker/Dockerfile.prod
    dockerContext: .
    dockerCommand: node server.js`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Prebuilt Image</h3>
              <p className="text-sm text-content-secondary mb-4">
                Skip the build entirely and deploy from a Docker registry:
              </p>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-app
    image:
      url: ghcr.io/myorg/myapp:v1.2.3
    port: 8080`} />
            </section>

            {/* ── Static Sites ────────────────────────────── */}
            <section id="static-sites" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-status-success/10 flex items-center justify-center">
                  <FileText className="w-5 h-5 text-status-success" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Static Sites</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Deploy static sites, SPAs, and JAMstack applications. RailPush runs your build command, then serves the output directory with CDN-optimized caching — hashed assets are served with immutable 1-year cache headers, while HTML is always fresh. CORS headers are enabled for cross-origin asset loading.
              </p>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: static
    name: my-site
    buildCommand: npm run build
    staticPublishPath: dist
    envVars:
      - key: VITE_API_URL
        value: https://api.myapp.com`} />
            </section>

            {/* ── Cron Jobs ───────────────────────────────── */}
            <section id="cron-jobs" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-status-info/10 flex items-center justify-center">
                  <Clock className="w-5 h-5 text-status-info" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Cron Jobs</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Schedule recurring tasks with standard cron expressions. The container starts, runs your command, and shuts down.
              </p>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: cron
    name: nightly-cleanup
    runtime: node
    schedule: "0 2 * * *"  # Every day at 2 AM
    buildCommand: npm install
    startCommand: node scripts/cleanup.js`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Cron Expressions</h3>
              <div className="overflow-x-auto">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Expression</th>
                      <th className="text-left py-2 font-semibold">Schedule</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary font-mono text-xs">
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4">* * * * *</td><td className="py-2 font-sans text-sm">Every minute</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4">0 * * * *</td><td className="py-2 font-sans text-sm">Every hour</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4">0 0 * * *</td><td className="py-2 font-sans text-sm">Every day at midnight</td></tr>
                    <tr className="border-b border-border-subtle"><td className="py-2 pr-4">0 0 * * 0</td><td className="py-2 font-sans text-sm">Every Sunday at midnight</td></tr>
                    <tr><td className="py-2 pr-4">0 0 1 * *</td><td className="py-2 font-sans text-sm">First day of every month</td></tr>
                  </tbody>
                </table>
              </div>
            </section>

            {/* ── Persistent Disks ────────────────────────── */}
            <section id="disks" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <HardDrive className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Persistent Disks</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Attach persistent storage to your services. Data survives redeploys and container restarts. Useful for file uploads, SQLite databases, or any stateful data.
              </p>
              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-app
    runtime: node
    disk:
      name: uploads
      mountPath: /var/data/uploads
      sizeGB: 25`} />

              <div className="rounded-xl border border-status-warning/30 bg-status-warning/5 p-5 mt-6">
                <div className="flex items-start gap-3">
                  <Settings className="w-5 h-5 text-status-warning shrink-0 mt-0.5" />
                  <div>
                    <div className="text-sm font-semibold mb-1">Single-instance only</div>
                    <div className="text-sm text-content-secondary">
                      Persistent disks can only be attached to services with a single instance. You cannot use disks with auto-scaled services.
                    </div>
                  </div>
                </div>
              </div>
            </section>

            {/* ── Scaling ─────────────────────────────────── */}
            <section id="scaling" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Cpu className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">Scaling</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                Scale your services horizontally by adding more instances. Each instance gets its own container and shares traffic via load balancing.
              </p>

              <h3 className="text-lg font-semibold mb-3">Plans</h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
                {[
                  { plan: 'Free', cpu: '0.1 CPU', ram: '256 MB', price: '$0/mo' },
                  { plan: 'Starter', cpu: '0.5 CPU', ram: '512 MB', price: '$7/mo' },
                  { plan: 'Standard', cpu: '1 CPU', ram: '2 GB', price: '$25/mo' },
                  { plan: 'Pro', cpu: '2 CPU', ram: '4 GB', price: '$85/mo' },
                ].map(p => (
                  <div key={p.plan} className="rounded-xl border border-border-default bg-surface-secondary/30 p-5">
                    <div className="text-base font-semibold mb-3">{p.plan}</div>
                    <div className="space-y-2 text-sm text-content-secondary">
                      <div className="flex items-center justify-between"><span>CPU</span><span className="font-mono text-content-primary">{p.cpu}</span></div>
                      <div className="flex items-center justify-between"><span>RAM</span><span className="font-mono text-content-primary">{p.ram}</span></div>
                      <div className="flex items-center justify-between"><span>Price</span><span className="font-semibold text-content-primary">{p.price}</span></div>
                    </div>
                  </div>
                ))}
              </div>

              <CodeBlock filename="railpush.yaml" code={`services:
  - type: web
    name: my-api
    runtime: node
    plan: standard
    numInstances: 3`} />
            </section>

            {/* ── CLI & API ────────────────────────────────── */}
            <section id="cli" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Terminal className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">CLI & API Reference</h2>
              </div>

              {/* ── CLI ── */}
              <h3 className="text-lg font-semibold mt-6 mb-3">CLI Installation</h3>
              <p className="text-content-secondary text-base leading-relaxed mb-4">
                The RailPush CLI lets you manage services, deployments, databases, and more from your terminal.
              </p>
              <CodeBlock language="bash" filename="terminal" code={`# macOS (Apple Silicon)
curl -fsSL https://railpush.com/dl/railpush-darwin-arm64 -o railpush && chmod +x railpush && sudo mv railpush /usr/local/bin/

# macOS (Intel)
curl -fsSL https://railpush.com/dl/railpush-darwin-amd64 -o railpush && chmod +x railpush && sudo mv railpush /usr/local/bin/

# Linux (amd64)
curl -fsSL https://railpush.com/dl/railpush-linux-amd64 -o railpush && chmod +x railpush && sudo mv railpush /usr/local/bin/

# Linux (arm64)
curl -fsSL https://railpush.com/dl/railpush-linux-arm64 -o railpush && chmod +x railpush && sudo mv railpush /usr/local/bin/`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">CLI Quick Start</h3>
              <CodeBlock language="bash" filename="terminal" code={`# Login
railpush login --host railpush.com

# List your services
railpush services list

# Trigger a deploy
railpush deploy <service-id>

# View logs
railpush logs <service-id> --tail 100

# Manage environment variables
railpush env list <service-id>
railpush env set <service-id> DATABASE_URL=postgres://... NODE_ENV=production

# Blueprint operations
railpush blueprints list
railpush blueprints sync <blueprint-id>

# Database management
railpush databases list
railpush databases create --name mydb --engine postgresql --plan starter`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">CLI Commands</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Command</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    {[
                      ['railpush login', 'Authenticate and store credentials'],
                      ['railpush logout', 'Remove stored credentials'],
                      ['railpush whoami', 'Show current user info'],
                      ['railpush services list', 'List all services'],
                      ['railpush services get <id>', 'Show service details (JSON)'],
                      ['railpush services create', 'Create a service (--name, --type, --repo)'],
                      ['railpush services delete <id>', 'Delete a service'],
                      ['railpush services restart <id>', 'Restart a running service'],
                      ['railpush deploy <service-id>', 'Trigger a new deploy'],
                      ['railpush deploys list <service-id>', 'List deploy history'],
                      ['railpush logs <service-id>', 'View service logs (--tail N)'],
                      ['railpush env list <service-id>', 'List environment variables'],
                      ['railpush env set <service-id> K=V', 'Set environment variables'],
                      ['railpush blueprints list', 'List all blueprints'],
                      ['railpush blueprints sync <id>', 'Trigger blueprint sync'],
                      ['railpush databases list', 'List all databases'],
                      ['railpush databases create', 'Create a database (--name, --plan)'],
                      ['railpush domains list <service-id>', 'List custom domains'],
                      ['railpush domains add <sid> <domain>', 'Add a custom domain'],
                    ].map(([cmd, desc]) => (
                      <tr key={cmd} className="border-b border-border-subtle">
                        <td className="py-2 pr-4 font-mono text-xs text-brand">{cmd}</td>
                        <td className="py-2 font-sans text-sm">{desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* ── REST API ── */}
              <h3 className="text-lg font-semibold mt-8 mb-3">REST API</h3>
              <p className="text-content-secondary text-base leading-relaxed mb-6">
                The RailPush API is a RESTful JSON API. Authenticate with a Bearer token from <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">/api/v1/auth/login</code> (JWT) or an API key from <code className="px-1.5 py-0.5 rounded bg-surface-tertiary text-content-primary text-xs font-mono">/api/v1/auth/api-keys</code>.
              </p>

              <h3 className="text-lg font-semibold mb-3">Authentication</h3>
              <CodeBlock language="bash" filename="terminal" code={`# Login and get a token
curl -X POST https://railpush.com/api/v1/auth/login \\
  -H "Content-Type: application/json" \\
  -d '{"email": "you@example.com", "password": "your-password"}'

# Use the token in subsequent requests
curl https://railpush.com/api/v1/services \\
  -H "Authorization: Bearer YOUR_TOKEN"`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Endpoints</h3>
              <div className="space-y-2 mb-6">
                {[
                  { method: 'GET', path: '/api/v1/services', desc: 'List all services' },
                  { method: 'POST', path: '/api/v1/services', desc: 'Create a service' },
                  { method: 'GET', path: '/api/v1/services/:id', desc: 'Get service details' },
                  { method: 'PATCH', path: '/api/v1/services/:id', desc: 'Update a service' },
                  { method: 'DELETE', path: '/api/v1/services/:id', desc: 'Delete a service' },
                  { method: 'POST', path: '/api/v1/services/:id/deploys', desc: 'Trigger a deploy' },
                  { method: 'GET', path: '/api/v1/services/:id/logs', desc: 'Query service logs' },
                  { method: 'GET', path: '/api/v1/services/:id/env-vars', desc: 'List env vars' },
                  { method: 'PUT', path: '/api/v1/services/:id/env-vars', desc: 'Bulk update env vars' },
                  { method: 'POST', path: '/api/v1/services/:id/custom-domains', desc: 'Add custom domain' },
                  { method: 'GET', path: '/api/v1/blueprints', desc: 'List blueprints' },
                  { method: 'POST', path: '/api/v1/blueprints', desc: 'Create a blueprint (auto-syncs)' },
                  { method: 'POST', path: '/api/v1/blueprints/:id/sync', desc: 'Trigger blueprint sync' },
                  { method: 'GET', path: '/api/v1/databases', desc: 'List databases' },
                  { method: 'POST', path: '/api/v1/databases', desc: 'Create a database' },
                ].map(e => (
                  <div key={e.method + e.path} className="flex items-center gap-3 px-4 py-2.5 rounded-lg border border-border-default bg-surface-secondary/30">
                    <code className={`px-2 py-0.5 rounded text-xs font-mono font-bold ${
                      e.method === 'GET' ? 'bg-status-success/10 text-status-success' :
                      e.method === 'POST' ? 'bg-brand/10 text-brand' :
                      e.method === 'PUT' ? 'bg-status-warning/10 text-status-warning' :
                      e.method === 'PATCH' ? 'bg-status-warning/10 text-status-warning' :
                      'bg-status-error/10 text-status-error'
                    }`}>{e.method}</code>
                    <code className="text-xs font-mono text-content-primary flex-1">{e.path}</code>
                    <span className="text-xs text-content-secondary hidden sm:block">{e.desc}</span>
                  </div>
                ))}
              </div>
            </section>

            {/* ── MCP Server ─────────────────────────────── */}
            <section id="mcp" className="scroll-mt-20 mb-20">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
                  <Plug className="w-5 h-5 text-brand" />
                </div>
                <h2 className="text-2xl font-bold tracking-tight">MCP Server</h2>
              </div>
              <p className="text-content-secondary text-base leading-relaxed mt-4 mb-6">
                The RailPush MCP (Model Context Protocol) server lets AI agents&mdash;Claude, ChatGPT, Cursor, and any MCP-compatible client&mdash;fully manage your infrastructure through natural language. Create services, trigger deploys, manage databases, configure env vars, and more without leaving your AI conversation.
              </p>

              <div className="rounded-xl border border-brand/20 bg-brand/5 p-5 mb-8">
                <div className="flex items-start gap-3">
                  <Zap className="w-5 h-5 text-brand shrink-0 mt-0.5" />
                  <div>
                    <div className="text-sm font-semibold mb-1">AI-native infrastructure</div>
                    <div className="text-sm text-content-secondary">
                      With 50 tools covering every platform capability, agents can deploy apps, debug failures, scale services, and manage databases&mdash;all autonomously.
                    </div>
                  </div>
                </div>
              </div>

              <h3 className="text-lg font-semibold mb-3">1. Create an API Key</h3>
              <p className="text-content-secondary text-sm leading-relaxed mb-4">
                Go to <strong>Settings &rarr; API Keys</strong> in the dashboard (or use the API) to create an API key. The raw key is shown only once&mdash;copy it immediately.
              </p>
              <CodeBlock language="bash" filename="terminal" code={`# Or via the API:
curl -X POST https://apps.railpush.com/api/v1/auth/api-keys \\
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"name": "mcp-server"}'

# Response: {"id": "...", "key": "abc123...", "name": "mcp-server"}`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">2. Install the MCP Server</h3>
              <CodeBlock language="bash" filename="terminal" code={`# Clone the repo and build
git clone https://github.com/muniravila-dotcom/RAILPUSH.git
cd RAILPUSH/mcp
npm install
npm run build`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">3. Configure Your AI Client</h3>
              <p className="text-content-secondary text-sm leading-relaxed mb-4">
                Add the MCP server to your client's configuration. Below are examples for popular clients.
              </p>

              <h3 className="text-base font-semibold mt-6 mb-3">Claude Desktop</h3>
              <CodeBlock language="json" filename="claude_desktop_config.json" code={`{
  "mcpServers": {
    "railpush": {
      "command": "node",
      "args": ["/path/to/RAILPUSH/mcp/build/index.js"],
      "env": {
        "RAILPUSH_API_KEY": "your-api-key",
        "RAILPUSH_API_URL": "https://apps.railpush.com"
      }
    }
  }
}`} />

              <h3 className="text-base font-semibold mt-6 mb-3">Claude Code (CLI)</h3>
              <CodeBlock language="bash" filename="terminal" code={`claude mcp add railpush \\
  -e RAILPUSH_API_KEY=your-api-key \\
  -e RAILPUSH_API_URL=https://apps.railpush.com \\
  -- node /path/to/RAILPUSH/mcp/build/index.js`} />

              <h3 className="text-base font-semibold mt-6 mb-3">Cursor</h3>
              <CodeBlock language="json" filename=".cursor/mcp.json" code={`{
  "mcpServers": {
    "railpush": {
      "command": "node",
      "args": ["/path/to/RAILPUSH/mcp/build/index.js"],
      "env": {
        "RAILPUSH_API_KEY": "your-api-key",
        "RAILPUSH_API_URL": "https://apps.railpush.com"
      }
    }
  }
}`} />

              <h3 className="text-lg font-semibold mt-8 mb-3">Environment Variables</h3>
              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Variable</th>
                      <th className="text-left py-2 pr-4 font-semibold">Required</th>
                      <th className="text-left py-2 font-semibold">Description</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    <tr className="border-b border-border-subtle">
                      <td className="py-2 pr-4 font-mono text-xs text-brand">RAILPUSH_API_KEY</td>
                      <td className="py-2 pr-4 text-sm">Yes</td>
                      <td className="py-2 text-sm">API key for authentication</td>
                    </tr>
                    <tr className="border-b border-border-subtle">
                      <td className="py-2 pr-4 font-mono text-xs text-brand">RAILPUSH_API_URL</td>
                      <td className="py-2 pr-4 text-sm">No</td>
                      <td className="py-2 text-sm">API base URL (default: https://apps.railpush.com)</td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mt-8 mb-3">Available Tools</h3>
              <p className="text-content-secondary text-sm leading-relaxed mb-4">
                The MCP server exposes 50 tools organized by category. Agents discover these automatically.
              </p>

              <div className="overflow-x-auto mb-8">
                <table className="w-full text-sm border-collapse">
                  <thead>
                    <tr className="border-b border-border-default">
                      <th className="text-left py-2 pr-4 font-semibold">Category</th>
                      <th className="text-left py-2 font-semibold">Tools</th>
                    </tr>
                  </thead>
                  <tbody className="text-content-secondary">
                    {[
                      ['Services', 'list, get, create, update, delete, restart, suspend, resume'],
                      ['Deploys', 'trigger, list, get, rollback'],
                      ['Env Vars', 'list, set (bulk replace)'],
                      ['Custom Domains', 'list, add, delete'],
                      ['Databases', 'list, create, get, update, delete, backup, list backups, replicas, create replica, promote replica, enable HA'],
                      ['Key-Value (Redis)', 'list, create, get, delete'],
                      ['Logs', 'get runtime logs, get deploy logs'],
                      ['AI Fix', 'start fix session, get fix status'],
                      ['One-Off Jobs', 'run, list, get'],
                      ['Autoscaling', 'get policy, set policy'],
                      ['Blueprints', 'list, create, get, sync, delete'],
                      ['Env Groups', 'list, create, get, delete, list vars, set vars, link service, unlink service, list linked services'],
                      ['Metrics', 'get resource usage'],
                      ['Projects', 'list projects'],
                      ['GitHub', 'list repos, list branches'],
                    ].map(([cat, tools]) => (
                      <tr key={cat} className="border-b border-border-subtle">
                        <td className="py-2 pr-4 font-semibold text-content-primary text-xs">{cat}</td>
                        <td className="py-2 text-xs">{tools}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <h3 className="text-lg font-semibold mt-8 mb-3">Example Conversations</h3>
              <p className="text-content-secondary text-sm leading-relaxed mb-4">
                Once configured, you can interact with your infrastructure naturally:
              </p>

              <CodeBlock language="text" filename="examples" code={`You: "Deploy my-api service from the staging branch"
Agent: [calls trigger_deploy with branch="staging"] Deploy triggered.

You: "What's failing on the flightatom service?"
Agent: [calls get_service, get_logs] The service is in deploy_failed state.
       The logs show a missing RESEND_API_KEY environment variable...

You: "Create a new Postgres database called analytics-db on the pro plan"
Agent: [calls create_database] Created database analytics-db.
       Connection URL: postgresql://analytics-db:pass@...

You: "Set the DATABASE_URL env var on my-api to that connection string and redeploy"
Agent: [calls set_env_vars, trigger_deploy] Done. Deploy is building now.

You: "Scale the web service to 3 instances with autoscaling up to 5"
Agent: [calls update_service, set_autoscaling_policy] Updated to 3 instances
       with autoscaling enabled (3-5 instances, 70% CPU target).`} />

              <div className="rounded-xl border border-brand/20 bg-brand/5 p-5 mt-8">
                <div className="flex items-start gap-3">
                  <Shield className="w-5 h-5 text-brand shrink-0 mt-0.5" />
                  <div>
                    <div className="text-sm font-semibold mb-1">Security</div>
                    <div className="text-sm text-content-secondary">
                      The MCP server authenticates using your API key and inherits your RBAC permissions. It can only access workspaces and resources your account has access to. API keys can be revoked at any time from the dashboard.
                    </div>
                  </div>
                </div>
              </div>
            </section>

          </div>
        </main>
      </div>
    </div>
  );
}
