import { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ArrowRight,
  GitBranch,
  Zap,
  Database,
  Globe2,
  Lock,
  Clock,
  Check,
  X,
  Rocket,
  Code,
  Radio,
  Github,
  Search,
  Loader2,
} from 'lucide-react';
import { SEO } from '../components/SEO';
import { Logo } from '../components/Logo';
import { PLAN_SPECS } from '../lib/plans';
import { auth } from '../lib/api';
import { RailpushHeroBackground } from '../components/RailpushHeroBackground';

// ─── Scroll Reveal Hook ────────────────────────────────────────────
function useScrollReveal() {
  const ref = useRef<HTMLDivElement>(null);
  const [isVisible, setIsVisible] = useState(false);

  const handleIntersect = useCallback(
    (entries: IntersectionObserverEntry[], observer: IntersectionObserver) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          setIsVisible(true);
          observer.disconnect();
        }
      });
    },
    []
  );

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const observer = new IntersectionObserver(handleIntersect, {
      threshold: 0.1,
      rootMargin: '0px 0px -50px 0px',
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [handleIntersect]);

  return { ref, isVisible };
}

// ─── Data ──────────────────────────────────────────────────────────
const features = [
  {
    icon: GitBranch,
    title: 'Zero-config Git deploys',
    description:
      'Connect GitHub and ship straight from main. We handle build images, rollbacks, and cache layers for you.',
  },
  {
    icon: Zap,
    title: 'Instant elasticity',
    description:
      'Autoscale HTTP, workers, and cron jobs in seconds without yaml forests or per-app knobs.',
  },
  {
    icon: Database,
    title: 'Managed Postgres',
    description:
      'Provision production-ready clusters with PITR backups, safe rotations, and crisp connection info.',
  },
  {
    icon: Globe2,
    title: 'Domains & routing',
    description:
      'Buy domains, wire DNS, and route to services with preview URLs and branch-aware rules.',
  },
  {
    icon: Lock,
    title: 'Shared secrets',
    description:
      'Organize secrets into reusable groups with audit-friendly history and scoped access.',
  },
  {
    icon: Clock,
    title: 'Reliable schedulers',
    description:
      'Cron jobs with retries, logs, and per-job alerting—visible alongside your apps.',
  },
];

const steps = [
  {
    number: '01',
    title: 'Connect your repo',
    description: 'OAuth GitHub, pick a branch, and keep your pipeline inside one platform.',
  },
  {
    number: '02',
    title: 'Ship with one push',
    description: 'We build on every push, stream logs, and create preview URLs for reviews.',
  },
  {
    number: '03',
    title: 'Go live with confidence',
    description: 'Promote to production with health checks, traffic shifting, and instant rollback.',
  },
];

const terminalLines = [
  { time: '12:00:01', text: '$ railpush deploy --branch main', color: 'text-content-primary' },
  { time: '12:00:02', text: 'Cloning repository...', color: 'text-content-secondary' },
  { time: '12:00:04', text: 'Detected runtime: Node.js 20', color: 'text-status-info' },
  { time: '12:00:05', text: 'Installing dependencies...', color: 'text-content-secondary' },
  { time: '12:00:12', text: 'Building application...', color: 'text-content-secondary' },
  { time: '12:00:18', text: 'Build completed successfully', color: 'text-status-success' },
  { time: '12:00:19', text: 'Starting health check...', color: 'text-content-secondary' },
  { time: '12:00:20', text: 'Health check passed (200 OK)', color: 'text-status-success' },
  { time: '12:00:21', text: 'Configuring routing...', color: 'text-content-secondary' },
  { time: '12:00:22', text: 'Live at https://my-app.railpush.com', color: 'text-status-success' },
];

const pricingPlans = PLAN_SPECS.map((plan) => ({
  name: plan.name,
  price: plan.priceLabel.replace('/mo', ''),
  period: '/month',
  description: plan.description,
  highlighted: !!plan.highlighted,
  badge: plan.badge,
  features: plan.features,
}));

const footerLinks = {
  Product: ['Web Services', 'Databases', 'Domains', 'Cron Jobs', 'Blueprints', 'Pricing'],
  Resources: ['Documentation', 'API Reference'],
  Company: ['Privacy'],
};

// ─── Typing Terminal ───────────────────────────────────────────────
function TypingTerminal() {
  const [visibleLines, setVisibleLines] = useState<number>(0);
  const [charIndex, setCharIndex] = useState<number>(0);
  const [started, setStarted] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const typingDone = visibleLines >= terminalLines.length;

  // Start when visible
  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setStarted(true);
          observer.disconnect();
        }
      },
      { threshold: 0.3 }
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  // Type characters one by one, then advance to next line
  useEffect(() => {
    if (!started || typingDone) return;

    const currentLine = terminalLines[visibleLines];
    if (!currentLine) return;

    if (charIndex < currentLine.text.length) {
      // Typing speed: faster for spaces/punctuation, slower for letters
      const ch = currentLine.text[charIndex];
      const delay = ch === ' ' ? 15 : ch === '.' ? 40 : 25;
      const timer = setTimeout(() => setCharIndex((c) => c + 1), delay);
      return () => clearTimeout(timer);
    } else {
      // Line done, pause then show next line
      const pause = visibleLines === 0 ? 400 : 200;
      const timer = setTimeout(() => {
        setVisibleLines((l) => l + 1);
        setCharIndex(0);
      }, pause);
      return () => clearTimeout(timer);
    }
  }, [started, visibleLines, charIndex, typingDone]);

  return (
    <div ref={ref} className="max-w-2xl mx-auto animate-slide-up-landing delay-500">
      <div className="rounded-xl border border-border-default bg-surface-secondary/70 backdrop-blur-sm overflow-hidden shadow-2xl shadow-black/40">
        {/* Title bar */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border-subtle">
          <div className="w-3 h-3 rounded-full bg-[#FF5F57]" />
          <div className="w-3 h-3 rounded-full bg-[#FEBC2E]" />
          <div className="w-3 h-3 rounded-full bg-[#28C840]" />
          <span className="ml-2 text-xs text-content-tertiary font-mono">terminal</span>
        </div>
        {/* Lines */}
        <div className="p-4 font-mono text-xs sm:text-sm leading-relaxed text-left min-h-[320px]">
          {terminalLines.map((line, i) => {
            if (i > visibleLines) return null;
            const isCurrentLine = i === visibleLines && !typingDone;
            const displayText = isCurrentLine
              ? line.text.slice(0, charIndex)
              : i < visibleLines
                ? line.text
                : '';

            return (
              <div key={i} className="flex gap-3" style={{ opacity: i <= visibleLines ? 1 : 0 }}>
                <span className="text-content-tertiary select-none shrink-0">{line.time}</span>
                <span className={line.color}>
                  {displayText}
                  {isCurrentLine && (
                    <span className="inline-block w-2 h-4 bg-content-primary align-middle animate-blink-cursor ml-px" />
                  )}
                </span>
              </div>
            );
          })}
          {/* Final prompt line */}
          {typingDone && (
            <div className="flex gap-3 mt-1">
              <span className="text-content-tertiary select-none shrink-0">12:00:23</span>
              <span className="text-content-primary">
                ${' '}
                <span className="inline-block w-2 h-4 bg-content-primary align-middle animate-blink-cursor" />
              </span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Component ─────────────────────────────────────────────────────
export function Landing() {
  const navigate = useNavigate();
  const [scrolled, setScrolled] = useState(false);

  const featuresReveal = useScrollReveal();
  const domainReveal = useScrollReveal();
  const stepsReveal = useScrollReveal();
  const terminalReveal = useScrollReveal();
  const pricingReveal = useScrollReveal();
  const ctaReveal = useScrollReveal();

  // Domain search state
  const [domainQuery, setDomainQuery] = useState('');
  const [domainResults, setDomainResults] = useState<Array<{ domain: string; available: boolean; price_cents: number }>>([]);
  const [domainSearching, setDomainSearching] = useState(false);
  const [domainSearched, setDomainSearched] = useState(false);

  const searchDomains = async () => {
    if (!domainQuery.trim()) return;
    setDomainSearching(true);
    setDomainSearched(false);
    try {
      const res = await fetch('/api/v1/domains/search', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: domainQuery.trim() }),
      });
      if (res.ok) {
        const data = await res.json();
        setDomainResults(data);
      }
    } catch {
      // silently fail on landing page
    } finally {
      setDomainSearching(false);
      setDomainSearched(true);
    }
  };

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 20);
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  return (
    <div className="min-h-screen bg-surface-primary text-content-primary overflow-x-hidden page-shell">
      <SEO
        title="RailPush — Modern app platform without the ops tax"
        description="Build, deploy, and scale web services, workers, cron, and Postgres from one clean console. Git-native, instant rollbacks, domain + DNS baked in."
        canonical="https://railpush.com/"
      />
      {/* ── Nav ──────────────────────────────────────── */}
      <nav
        className={`fixed top-0 inset-x-0 z-50 transition-all duration-300 ${
          scrolled
            ? 'bg-surface-primary/80 backdrop-blur-xl border-b border-border-default'
            : 'bg-transparent'
        }`}
      >
        <div className="max-w-7xl mx-auto px-6 h-16 flex items-center justify-between">
          {/* Logo */}
          <Logo size={32} />

          {/* Desktop links */}
          <div className="hidden md:flex items-center gap-8">
            <a href="#features" className="text-sm text-content-secondary hover:text-content-primary transition-colors">
              Features
            </a>
            <a href="#pricing" className="text-sm text-content-secondary hover:text-content-primary transition-colors">
              Pricing
            </a>
            <span onClick={() => navigate('/docs')} className="text-sm text-content-secondary hover:text-content-primary transition-colors cursor-pointer">
              Docs
            </span>
          </div>

          {/* CTA */}
          <div className="flex items-center gap-3">
            <button
              onClick={() => navigate('/login')}
              className="hidden sm:inline-flex text-sm text-content-secondary hover:text-content-primary transition-colors px-3 py-1.5"
            >
              Log in
            </button>
            <button
              onClick={() => navigate('/login')}
              className="text-sm font-medium bg-brand hover:bg-brand-hover text-white px-4 py-2 rounded-lg transition-colors"
            >
              Get Started
            </button>
          </div>
        </div>
      </nav>

      {/* ── Hero ─────────────────────────────────────── */}
      <section className="relative min-h-screen flex items-center justify-center overflow-hidden bg-[#020617] theme-dark">
        <RailpushHeroBackground className="absolute inset-0 pointer-events-none" />
        <div
          className="absolute top-[-30%] left-1/2 -translate-x-1/2 w-[1200px] h-[1000px] blur-[100px] pointer-events-none"
          style={{
            background:
              'radial-gradient(circle, rgba(99,102,241,0.15) 0%, rgba(148,163,184,0.05) 50%, transparent 80%)',
          }}
        />
        <div
          className="absolute inset-x-0 bottom-0 h-28 pointer-events-none"
          style={{
            background:
              'linear-gradient(to bottom, rgba(2,6,23,0) 0%, var(--color-surface-primary) 100%)',
          }}
        />

        <div className="relative z-10 max-w-4xl mx-auto px-6 text-center pt-24">
          {/* Badge */}
          <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full border border-border-default bg-surface-secondary/60 backdrop-blur-sm mb-8 animate-slide-up-landing delay-100">
            <Radio className="w-3.5 h-3.5 text-brand" />
            <span className="text-xs font-medium text-content-secondary">
              Unified platform for modern apps
            </span>
          </div>

          {/* Headline */}
          <h1 className="text-5xl sm:text-6xl lg:text-7xl font-bold tracking-tight leading-[1.05] mb-6 animate-slide-up-landing delay-200">
            Run your stack{' '}
            <span className="text-gradient-brand">without the ops tax</span>
          </h1>

          {/* Subtitle */}
          <p className="text-lg sm:text-xl text-content-secondary max-w-2xl mx-auto mb-10 animate-slide-up-landing delay-300">
            RailPush keeps build, deploy, DNS, secrets, and data in the same calm console—so teams can ship quickly without another control plane.
          </p>

          {/* CTAs */}
          <div className="flex flex-col sm:flex-row items-center justify-center gap-4 mb-8 animate-slide-up-landing delay-400">
            <button
              onClick={() => navigate('/login')}
              className="inline-flex items-center gap-2 bg-brand hover:bg-brand-hover text-white font-medium px-6 py-3 rounded-lg transition-colors text-sm"
            >
              Get Started Free
              <ArrowRight className="w-4 h-4" />
            </button>
            <button
              onClick={() => auth.loginGithub()}
              className="inline-flex items-center gap-2 border border-border-default hover:border-border-hover text-content-primary font-medium px-6 py-3 rounded-lg transition-colors text-sm bg-surface-secondary/40"
            >
              <Github className="w-4 h-4" />
              Connect GitHub
            </button>
          </div>

          {/* Trust line */}
          <p className="text-xs text-content-tertiary mb-16 animate-slide-up-landing delay-500">
            Free tier &middot; No credit card &middot; Self-hosted
          </p>

          {/* Terminal mockup — typing animation */}
          <TypingTerminal />
        </div>
      </section>

      {/* ── Features ─────────────────────────────────── */}
      <section id="features" className="py-32 px-6" ref={featuresReveal.ref}>
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold tracking-tight mb-4">
              Everything you need to ship
            </h2>
            <p className="text-content-secondary text-lg max-w-xl mx-auto">
              From git push to production in seconds. No Dockerfiles, no YAML, no hassle.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {features.map((f, i) => (
              <div
                key={f.title}
                className={`group rounded-xl border border-border-default bg-surface-secondary/50 p-6 hover:-translate-y-1 transition-all duration-300 ${
                  featuresReveal.isVisible
                    ? `animate-scale-in delay-${(i + 1) * 100}`
                    : 'opacity-0'
                }`}
              >
                <div className="w-10 h-10 rounded-lg bg-brand/10 flex items-center justify-center mb-4">
                  <f.icon className="w-5 h-5 text-brand" />
                </div>
                <h3 className="text-base font-semibold mb-2">{f.title}</h3>
                <p className="text-sm text-content-secondary leading-relaxed">{f.description}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── Domain Search ─────────────────────────────── */}
      <section className="py-32 px-6 bg-surface-secondary" ref={domainReveal.ref}>
        <div className="max-w-3xl mx-auto">
          <div className="text-center mb-10">
            <h2
              className={`text-3xl sm:text-4xl font-bold tracking-tight mb-4 ${
                domainReveal.isVisible ? 'animate-slide-up-landing' : 'opacity-0'
              }`}
            >
              Find your perfect domain
            </h2>
            <p
              className={`text-content-secondary text-lg max-w-xl mx-auto ${
                domainReveal.isVisible ? 'animate-slide-up-landing delay-100' : 'opacity-0'
              }`}
            >
              Search, register, and manage domains — all from one dashboard. Full DNS editor included.
            </p>
          </div>

          <div
            className={`${
              domainReveal.isVisible ? 'animate-scale-in delay-200' : 'opacity-0'
            }`}
          >
            {/* Search bar */}
            <div className="flex items-center gap-3 mb-6">
              <div className="flex-1 relative">
                <input
                  type="text"
                  placeholder="Search for a domain name..."
                  value={domainQuery}
                  onChange={(e) => setDomainQuery(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && searchDomains()}
                  className="w-full bg-surface-primary border border-border-default rounded-xl px-5 py-4 text-base text-content-primary placeholder:text-content-tertiary focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/15 transition-all duration-150"
                />
                <Globe2 className="absolute right-4 top-1/2 -translate-y-1/2 w-5 h-5 text-content-tertiary pointer-events-none" />
              </div>
              <button
                onClick={searchDomains}
                disabled={domainSearching || !domainQuery.trim()}
                className="inline-flex items-center gap-2 bg-brand hover:bg-brand-hover disabled:opacity-50 text-white font-medium px-6 py-4 rounded-xl transition-colors text-sm shrink-0"
              >
                {domainSearching ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <Search className="w-4 h-4" />
                )}
                Search
              </button>
            </div>

            {/* Results */}
            {domainSearched && domainResults.length > 0 && (
              <div className="rounded-xl border border-border-default bg-surface-primary overflow-hidden">
                {domainResults.slice(0, 6).map((r) => (
                  <div
                    key={r.domain}
                    className="flex items-center justify-between px-5 py-3.5 border-b border-border-subtle last:border-0 hover:bg-surface-tertiary/50 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      {r.available ? (
                        <div className="w-6 h-6 rounded-full bg-status-success/15 flex items-center justify-center">
                          <Check className="w-3.5 h-3.5 text-status-success" />
                        </div>
                      ) : (
                        <div className="w-6 h-6 rounded-full bg-status-error/15 flex items-center justify-center">
                          <X className="w-3.5 h-3.5 text-status-error" />
                        </div>
                      )}
                      <span className="text-sm font-medium text-content-primary">{r.domain}</span>
                    </div>
                    <div className="flex items-center gap-4">
                      {r.available ? (
                        <>
                          <span className="text-sm font-semibold text-content-primary">
                            ${(r.price_cents / 100).toFixed(2)}<span className="text-content-tertiary font-normal">/yr</span>
                          </span>
                          <button
                            onClick={() => navigate('/login')}
                            className="text-xs font-medium bg-brand hover:bg-brand-hover text-white px-3.5 py-1.5 rounded-lg transition-colors"
                          >
                            Register
                          </button>
                        </>
                      ) : (
                        <span className="text-xs text-content-tertiary">Taken</span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* TLD pricing badges */}
            {!domainSearched && (
              <div className="flex flex-wrap items-center justify-center gap-3 mt-2">
                {[
                  { tld: '.com', price: '$11.99' },
                  { tld: '.dev', price: '$14.99' },
                  { tld: '.io', price: '$39.99' },
                  { tld: '.app', price: '$14.99' },
                  { tld: '.xyz', price: '$1.99' },
                  { tld: '.co', price: '$29.99' },
                ].map((t) => (
                  <span
                    key={t.tld}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-border-default bg-surface-primary/50 text-sm"
                  >
                    <span className="font-semibold text-content-primary">{t.tld}</span>
                    <span className="text-content-tertiary">from {t.price}/yr</span>
                  </span>
                ))}
              </div>
            )}
          </div>
        </div>
      </section>

      {/* ── How It Works ─────────────────────────────── */}
      <section className="py-32 px-6 bg-surface-secondary" ref={stepsReveal.ref}>
        <div className="max-w-5xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold tracking-tight mb-4">
              Three steps to production
            </h2>
            <p className="text-content-secondary text-lg max-w-xl mx-auto">
              No complex pipelines. No infrastructure to manage.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-8 relative">
            {/* Connector line (desktop) */}
            <div className="hidden md:block absolute top-10 left-[calc(16.67%+24px)] right-[calc(16.67%+24px)] h-px border-t-2 border-dashed border-border-default" />

            {steps.map((s, i) => (
              <div
                key={s.number}
                className={`relative text-center ${
                  stepsReveal.isVisible
                    ? `animate-slide-up-landing delay-${(i + 1) * 100}`
                    : 'opacity-0'
                }`}
              >
                <div className="w-12 h-12 rounded-full bg-brand/10 border-2 border-brand text-brand font-bold text-sm flex items-center justify-center mx-auto mb-5 relative z-10 bg-surface-secondary">
                  {s.number}
                </div>
                <h3 className="text-lg font-semibold mb-2">{s.title}</h3>
                <p className="text-sm text-content-secondary leading-relaxed max-w-xs mx-auto">
                  {s.description}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── Terminal Demo ────────────────────────────── */}
      <section className="py-32 px-6" ref={terminalReveal.ref}>
        <div className="max-w-4xl mx-auto">
          <div className="text-center mb-12">
            <h2 className="text-3xl sm:text-4xl font-bold tracking-tight mb-4">
              Watch it happen
            </h2>
            <p className="text-content-secondary text-lg max-w-xl mx-auto">
              A real deploy in real time. From push to live in under 30 seconds.
            </p>
          </div>

          <div
            className={`rounded-xl border border-border-default bg-surface-secondary/80 overflow-hidden shadow-2xl shadow-brand/5 ${
              terminalReveal.isVisible ? 'animate-scale-in' : 'opacity-0'
            }`}
          >
            <div className="flex items-center gap-2 px-4 py-3 border-b border-border-subtle bg-surface-tertiary/50">
              <div className="w-3 h-3 rounded-full bg-[#FF5F57]" />
              <div className="w-3 h-3 rounded-full bg-[#FEBC2E]" />
              <div className="w-3 h-3 rounded-full bg-[#28C840]" />
              <span className="ml-2 text-xs text-content-tertiary font-mono">
                railpush deploy
              </span>
            </div>
            <div className="p-6 font-mono text-sm leading-loose">
              <div className="flex gap-3">
                <span className="text-content-tertiary select-none">$</span>
                <span className="text-content-primary">git push origin main</span>
              </div>
              <div className="mt-3 space-y-1">
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Webhook received — starting build
                </p>
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Cloning <span className="text-brand">acme/web-app</span> @ <span className="text-content-primary">a3f8c21</span>
                </p>
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Detected runtime: <span className="text-status-info">Node.js 20</span>
                </p>
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  npm install — <span className="text-content-primary">1,247 packages</span> in 4.2s
                </p>
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  npm run build — completed in 6.1s
                </p>
                <p className="text-status-success">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Build succeeded — image <span className="text-content-primary">railpush/acme-web-app:a3f8c21</span>
                </p>
                <p className="text-content-secondary">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Starting container...
                </p>
                <p className="text-status-success">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Health check passed (200 OK)
                </p>
                <p className="text-status-success font-semibold">
                  <span className="text-content-tertiary mr-3">{'>'}</span>
                  Live at <span className="underline decoration-brand/50">https://web-app.railpush.com</span>
                </p>
              </div>
              <div className="flex gap-3 mt-4">
                <span className="text-content-tertiary select-none">$</span>
                <span className="inline-block w-2 h-4 bg-content-primary align-middle animate-blink-cursor" />
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ── Pricing ──────────────────────────────────── */}
      <section id="pricing" className="py-32 px-6 bg-surface-secondary" ref={pricingReveal.ref}>
        <div className="max-w-6xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl sm:text-4xl font-bold tracking-tight mb-4">
              Simple, transparent pricing
            </h2>
            <p className="text-content-secondary text-lg max-w-xl mx-auto">
              Start free, scale when you need to. No surprise bills.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 items-start">
            {pricingPlans.map((plan, i) => (
              <div
                key={plan.name}
                className={`relative rounded-xl border p-8 transition-all duration-300 ${
                  plan.highlighted
                    ? 'border-brand bg-surface-primary shadow-lg shadow-brand/10 scale-[1.02]'
                    : 'border-border-default bg-surface-primary/50'
                } ${
                  pricingReveal.isVisible
                    ? `animate-scale-in delay-${(i + 1) * 100}`
                    : 'opacity-0'
                }`}
              >
                {plan.badge && (
                  <div className="absolute -top-3 left-1/2 -translate-x-1/2 px-3 py-1 rounded-full bg-brand text-white text-xs font-medium">
                    {plan.badge}
                  </div>
                )}

                <h3 className="text-lg font-semibold mb-1">{plan.name}</h3>
                <p className="text-sm text-content-secondary mb-6">{plan.description}</p>

                <div className="mb-6">
                  <span className="text-4xl font-bold">{plan.price}</span>
                  <span className="text-content-secondary text-sm">{plan.period}</span>
                </div>

                <ul className="space-y-3 mb-8">
                  {plan.features.map((f) => (
                    <li key={f} className="flex items-center gap-2.5 text-sm text-content-secondary">
                      <Check className="w-4 h-4 text-status-success shrink-0" />
                      {f}
                    </li>
                  ))}
                </ul>

                <button
                  onClick={() => navigate('/login')}
                  className={`w-full py-2.5 rounded-lg text-sm font-medium transition-colors ${
                    plan.highlighted
                      ? 'bg-brand hover:bg-brand-hover text-white'
                      : 'border border-border-default hover:border-border-hover text-content-primary'
                  }`}
                >
                  Get Started
                </button>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── Final CTA ────────────────────────────────── */}
      <section className="py-32 px-6" ref={ctaReveal.ref}>
        <div
          className={`max-w-3xl mx-auto text-center ${
            ctaReveal.isVisible ? 'animate-slide-up-landing' : 'opacity-0'
          }`}
        >
          <Rocket className="w-10 h-10 text-brand mx-auto mb-6" />
          <h2 className="text-3xl sm:text-4xl font-bold tracking-tight mb-4">
            Ready to deploy?
          </h2>
          <p className="text-content-secondary text-lg mb-10 max-w-lg mx-auto">
            Join developers shipping faster with RailPush. Go from code to production in minutes.
          </p>
          <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
            <button
              onClick={() => navigate('/login')}
              className="inline-flex items-center gap-2 bg-brand hover:bg-brand-hover text-white font-medium px-6 py-3 rounded-lg transition-colors text-sm"
            >
              Get Started Free
              <ArrowRight className="w-4 h-4" />
            </button>
            <button
              onClick={() => navigate('/docs')}
              className="inline-flex items-center gap-2 border border-border-default hover:border-border-hover text-content-primary font-medium px-6 py-3 rounded-lg transition-colors text-sm bg-surface-secondary/40"
            >
              <Code className="w-4 h-4" />
              View Documentation
            </button>
          </div>
        </div>
      </section>

      {/* ── Footer ───────────────────────────────────── */}
      <footer className="border-t border-border-default bg-surface-secondary py-16 px-6">
        <div className="max-w-7xl mx-auto grid grid-cols-2 md:grid-cols-4 gap-10">
          {/* Brand */}
          <div className="col-span-2 md:col-span-1">
            <div className="mb-4">
              <Logo size={28} />
            </div>
            <p className="text-sm text-content-secondary leading-relaxed mb-4">
              The cloud platform for developers who ship fast.
            </p>
            <p className="text-xs text-content-tertiary">
              &copy; {new Date().getFullYear()} RailPush. All rights reserved.
            </p>
          </div>

          {/* Link groups */}
          {Object.entries(footerLinks).map(([group, links]) => (
            <div key={group}>
              <h4 className="text-sm font-semibold mb-4">{group}</h4>
              <ul className="space-y-2.5">
                {links.map((link) => {
                  const linkMap: Record<string, string> = {
                    'Documentation': '/docs',
                    'API Reference': '/docs#cli',
                    'Web Services': '/docs#services',
                    'Databases': '/docs#databases',
                    'Domains': '/docs#domains',
                    'Cron Jobs': '/docs#cron-jobs',
                    'Blueprints': '/docs#blueprints',
                    'Pricing': '#pricing',
                    'Privacy': '/privacy',
                  };
                  const href = linkMap[link];
                  return (
                    <li key={link}>
                      <span
                        onClick={() => href ? navigate(href) : undefined}
                        className="text-sm text-content-secondary hover:text-content-primary transition-colors cursor-pointer"
                      >
                        {link}
                      </span>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      </footer>
    </div>
  );
}
