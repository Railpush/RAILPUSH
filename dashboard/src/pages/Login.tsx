import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { Github, Eye, EyeOff, Loader2, ArrowRight, Terminal, Sparkles } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { auth } from '../lib/api';
import { SEO } from '../components/SEO';
import { Logo } from '../components/Logo';

const terminalLines = [
  { delay: 0, text: '$ git push origin main', color: 'text-content-primary' },
  { delay: 600, text: 'Enumerating objects: 42, done.', color: 'text-content-tertiary' },
  { delay: 1000, text: '> Webhook received — starting build', color: 'text-content-secondary' },
  { delay: 1600, text: '> Cloning repository...', color: 'text-content-secondary' },
  { delay: 2200, text: '> Detected runtime: Node.js 20', color: 'text-status-info' },
  { delay: 2800, text: '> Installing dependencies...', color: 'text-content-secondary' },
  { delay: 3800, text: '> npm install — 1,247 packages in 4.2s', color: 'text-content-secondary' },
  { delay: 4400, text: '> Building application...', color: 'text-content-secondary' },
  { delay: 5400, text: '> Build succeeded — image railpush/app:a3f8c21', color: 'text-status-success' },
  { delay: 6000, text: '> Starting container...', color: 'text-content-secondary' },
  { delay: 6600, text: '> Health check passed (200 OK)', color: 'text-status-success' },
  { delay: 7200, text: '> Live at https://my-app.railpush.com', color: 'text-status-success font-semibold' },
];

function AnimatedTerminal() {
  const [visibleLines, setVisibleLines] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    terminalLines.forEach((line, i) => {
      timers.push(setTimeout(() => {
        setVisibleLines(i + 1);
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, line.delay));
    });
    // Loop
    timers.push(setTimeout(() => setVisibleLines(0), 9000));
    const loop = setInterval(() => {
      setVisibleLines(0);
      terminalLines.forEach((line, i) => {
        timers.push(setTimeout(() => {
          setVisibleLines(i + 1);
          if (containerRef.current) {
            containerRef.current.scrollTop = containerRef.current.scrollHeight;
          }
        }, line.delay));
      });
    }, 9000);
    return () => { timers.forEach(clearTimeout); clearInterval(loop); };
  }, []);

  return (
    <div className="rounded-xl border border-border-default bg-surface-secondary/80 backdrop-blur-sm overflow-hidden shadow-2xl shadow-black/40">
      <div className="flex items-center gap-2 px-4 py-2.5 border-b border-border-subtle bg-surface-tertiary/50">
        <div className="w-3 h-3 rounded-full bg-[#FF5F57]" />
        <div className="w-3 h-3 rounded-full bg-[#FEBC2E]" />
        <div className="w-3 h-3 rounded-full bg-[#28C840]" />
        <span className="ml-2 text-xs text-content-tertiary font-mono flex items-center gap-1.5">
          <Terminal className="w-3 h-3" />
          deploy
        </span>
      </div>
      <div ref={containerRef} className="p-4 font-mono text-xs leading-relaxed h-[220px] overflow-hidden">
        {terminalLines.slice(0, visibleLines).map((line, i) => (
          <div
            key={i}
            className={`${line.color} transition-opacity duration-300`}
            style={{ animation: 'fadeIn 0.3s ease-out' }}
          >
            {line.text}
          </div>
        ))}
        {visibleLines >= terminalLines.length && (
          <div className="text-content-primary mt-1">
            $ <span className="inline-block w-2 h-3.5 bg-content-primary align-middle animate-blink-cursor" />
          </div>
        )}
      </div>
    </div>
  );
}

export function Login() {
  const navigate = useNavigate();
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const result = mode === 'register'
        ? await auth.register({ email, password, name })
        : await auth.login({ email, password });
      void result;
      window.location.href = '/';
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Something went wrong';
      setError(message);
    } finally {
      setLoading(false);
    }
  };

  const inputClass = 'w-full h-11 px-4 rounded-xl bg-surface-tertiary border border-border-default text-content-primary placeholder:text-content-tertiary text-sm focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand transition-all duration-200';

  return (
    <div className="min-h-screen bg-surface-primary flex">
      <SEO
        title="Sign In — RailPush"
        description="Access RailPush to manage services, databases, domains, and schedulers from one console. Create an account or sign in to keep shipping."
        canonical="https://railpush.com/login"
      />
      {/* ── Left: Branding / Visual ───────────────────── */}
      <div className="hidden lg:flex lg:w-1/2 relative overflow-hidden">
        {/* Animated background */}
        <div className="absolute inset-0 bg-gradient-to-br from-brand/5 via-surface-primary to-brand-purple/5" />
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-brand/15 rounded-full blur-[120px] animate-float" />
        <div className="absolute bottom-1/3 right-1/4 w-80 h-80 bg-brand-purple/10 rounded-full blur-[100px] animate-float delay-700" />
        <div className="absolute top-2/3 left-1/2 w-64 h-64 bg-status-success/5 rounded-full blur-[80px] animate-float delay-300" />

        {/* Grid pattern */}
        <div
          className="absolute inset-0 opacity-[0.03]"
          style={{
            backgroundImage: 'linear-gradient(rgba(255,255,255,0.1) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.1) 1px, transparent 1px)',
            backgroundSize: '60px 60px',
          }}
        />

        <div className="relative z-10 flex flex-col justify-between p-12 w-full">
          {/* Top: Logo */}
          <div>
            <button
              onClick={() => navigate('/')}
              className="flex items-center hover:opacity-80 transition-opacity"
            >
              <Logo size={36} />
            </button>
          </div>

          {/* Center: Terminal + Copy */}
          <div className="max-w-md mx-auto w-full space-y-8">
            <div>
              <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full border border-border-default bg-surface-secondary/40 backdrop-blur-sm mb-6">
                <Sparkles className="w-3.5 h-3.5 text-brand" />
                <span className="text-xs font-medium text-content-secondary">Hands-off builds, confident releases</span>
              </div>
              <h2 className="text-3xl font-bold tracking-tight leading-tight mb-3">
                Ship with focus<br />
                <span className="text-gradient-brand">no yak shaving required</span>
              </h2>
              <p className="text-content-secondary text-sm leading-relaxed max-w-sm">
                Git in, services out. RailPush builds, provisions data, issues certs, and scales while you stay in flow.
              </p>
            </div>
            <AnimatedTerminal />
          </div>

          {/* Bottom: Stats */}
          <div className="flex items-center gap-8">
            {[
              { value: '< 30s', label: 'From push to live' },
              { value: '9', label: 'Runtime families' },
              { value: '99.9%', label: 'Target uptime' },
            ].map(s => (
              <div key={s.label}>
                <div className="text-xl font-bold text-content-primary">{s.value}</div>
                <div className="text-xs text-content-tertiary">{s.label}</div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* ── Right: Auth Form ──────────────────────────── */}
      <div className="flex-1 flex items-center justify-center px-6 py-12 relative">
        {/* Subtle background for mobile */}
        <div className="lg:hidden absolute top-0 left-0 right-0 h-64 bg-gradient-to-b from-brand/5 to-transparent" />

        <div className="w-full max-w-[400px] relative z-10">
          {/* Mobile logo */}
          <div className="lg:hidden text-center mb-10">
            <button
              onClick={() => navigate('/')}
              className="inline-flex items-center"
            >
              <Logo size={44} />
            </button>
          </div>

          {/* Header */}
          <div className="mb-8">
            <h1 className="text-2xl font-bold text-content-primary mb-1.5">
              {mode === 'login' ? 'Welcome back' : 'Create your account'}
            </h1>
            <p className="text-sm text-content-secondary">
              {mode === 'login'
                ? 'Sign in to your RailPush dashboard'
                : 'Start deploying in under a minute'}
            </p>
          </div>

          {/* GitHub OAuth */}
          <button
            onClick={() => auth.loginGithub()}
            className="w-full flex items-center justify-center gap-2.5 h-11 rounded-xl border border-border-default bg-surface-secondary hover:bg-surface-tertiary text-content-primary text-sm font-medium transition-all duration-200 mb-6"
          >
            <Github className="w-4.5 h-4.5" />
            Continue with GitHub
          </button>

          {/* Divider */}
          <div className="relative mb-6">
            <div className="absolute inset-0 flex items-center">
              <div className="w-full border-t border-border-default" />
            </div>
            <div className="relative flex justify-center">
              <span className="bg-surface-primary px-3 text-xs text-content-tertiary">or continue with email</span>
            </div>
          </div>

          {/* Error */}
          {error && (
            <div className="mb-5 p-3.5 rounded-xl bg-status-error/8 border border-status-error/15 text-status-error text-sm flex items-start gap-2.5">
              <div className="w-5 h-5 rounded-full bg-status-error/15 flex items-center justify-center shrink-0 mt-0.5">
                <span className="text-xs font-bold">!</span>
              </div>
              {error}
            </div>
          )}

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-4">
            {mode === 'register' && (
              <div>
                <label className="block text-sm font-medium text-content-primary mb-2">Name</label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Your name"
                  className={inputClass}
                />
              </div>
            )}

            <div>
              <label className="block text-sm font-medium text-content-primary mb-2">Email</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                required
                autoComplete="email"
                className={inputClass}
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-content-primary mb-2">Password</label>
              <div className="relative">
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={mode === 'register' ? 'Min. 8 characters' : 'Enter your password'}
                  required
                  minLength={mode === 'register' ? 8 : undefined}
                  autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                  className={`${inputClass} pr-11`}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-3.5 top-1/2 -translate-y-1/2 text-content-tertiary hover:text-content-secondary transition-colors p-0.5"
                >
                  {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
            </div>

            <Button
              type="submit"
              size="lg"
              className="w-full h-11 rounded-xl mt-2"
              disabled={loading}
            >
              {loading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <span className="flex items-center gap-2">
                  {mode === 'login' ? 'Sign In' : 'Create Account'}
                  <ArrowRight className="w-4 h-4" />
                </span>
              )}
            </Button>
          </form>

          {/* Toggle */}
          <div className="mt-6 text-center">
            <span className="text-sm text-content-secondary">
              {mode === 'login' ? "Don't have an account? " : 'Already have an account? '}
            </span>
            <button
              type="button"
              onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(''); }}
              className="text-sm font-medium text-brand hover:text-brand-hover transition-colors"
            >
              {mode === 'login' ? 'Sign up' : 'Sign in'}
            </button>
          </div>

          {/* Footer */}
          <div className="mt-10 pt-6 border-t border-border-subtle flex items-center justify-center gap-4 text-xs text-content-tertiary">
            <span onClick={() => navigate('/')} className="hover:text-content-secondary transition-colors cursor-pointer">Home</span>
            <span className="w-1 h-1 rounded-full bg-border-default" />
            <span onClick={() => navigate('/docs')} className="hover:text-content-secondary transition-colors cursor-pointer">Docs</span>
            <span className="w-1 h-1 rounded-full bg-border-default" />
            <span onClick={() => navigate('/privacy')} className="hover:text-content-secondary transition-colors cursor-pointer">Privacy</span>
          </div>
        </div>
      </div>
    </div>
  );
}
