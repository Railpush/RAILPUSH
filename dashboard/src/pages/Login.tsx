import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowRight, Eye, EyeOff } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { auth } from '../lib/api';
import { SEO } from '../components/SEO';
import { Logo } from '../components/Logo';

function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 98 96" className={className} fill="currentColor" xmlns="http://www.w3.org/2000/svg">
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M48.854 0C21.839 0 0 22 0 49.217c0 21.756 13.993 40.172 33.405 46.69 2.427.49 3.316-1.059 3.316-2.362 0-1.141-.08-5.052-.08-9.127-13.59 2.934-16.42-5.867-16.42-5.867-2.184-5.704-5.42-7.17-5.42-7.17-4.448-3.015.324-3.015.324-3.015 4.934.326 7.523 5.052 7.523 5.052 4.367 7.496 11.404 5.378 14.235 4.074.404-3.178 1.699-5.378 3.074-6.6-10.839-1.141-22.243-5.378-22.243-24.283 0-5.378 1.94-9.778 5.014-13.2-.485-1.222-2.184-6.275.486-13.038 0 0 4.125-1.304 13.426 5.052a46.97 46.97 0 0 1 12.214-1.63c4.125 0 8.33.571 12.213 1.63 9.302-6.356 13.427-5.052 13.427-5.052 2.67 6.763.97 11.816.485 13.038 3.155 3.422 5.015 7.822 5.015 13.2 0 18.905-11.404 23.06-22.324 24.283 1.78 1.548 3.316 4.481 3.316 9.126 0 6.6-.08 11.897-.08 13.526 0 1.304.89 2.853 3.316 2.364 19.412-6.52 33.405-24.935 33.405-46.691C97.707 22 75.788 0 48.854 0z"
      />
    </svg>
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
  const [notice, setNotice] = useState('');
  const [resending, setResending] = useState(false);

  const inputClass =
    'w-full h-10 px-3 rounded-md bg-surface-secondary border border-border-default text-content-primary placeholder:text-content-tertiary text-sm focus:outline-none focus:ring-2 focus:ring-brand/20 focus:border-brand transition-all';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setNotice('');
    setLoading(true);

    try {
      if (mode === 'register') {
        await auth.register({ email, password, name });
        setNotice('Check your email for a verification link, then sign in.');
        setMode('login');
        setPassword('');
        setShowPassword(false);
        return;
      } else {
        await auth.login({ email, password });
      }
      window.location.href = '/';
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Something went wrong';
      setError(message);
    } finally {
      setLoading(false);
    }
  };

  const resend = async () => {
    const e = email.trim();
    if (!e) return;
    setResending(true);
    setError('');
    setNotice('');
    try {
      await auth.resendVerification(e);
      setNotice('If that account needs verification, a new link has been sent.');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to resend verification email');
    } finally {
      setResending(false);
    }
  };

  return (
    <div className="min-h-screen bg-surface-primary text-content-primary flex items-center justify-center px-4 py-10">
      <SEO
        title="Sign In — RailPush"
        description="Access RailPush to manage services, databases, domains, and schedulers from one console. Create an account or sign in to keep shipping."
        canonical="https://railpush.com/login"
      />

      <div className="w-full max-w-[420px] bg-surface-secondary border border-border-default rounded-lg p-6">
        <div className="flex items-center justify-between gap-3">
          <button onClick={() => navigate('/')} className="hover:opacity-80 transition-opacity">
            <Logo size={34} />
          </button>
          <div className="text-xs text-content-tertiary">railpush.com</div>
        </div>

        <div className="mt-6">
          <h1 className="text-lg font-semibold">{mode === 'login' ? 'Sign in' : 'Create account'}</h1>
          <p className="text-sm text-content-secondary mt-1">
            {mode === 'login' ? 'Access your dashboard.' : 'Create an account to start deploying.'}
          </p>
        </div>

        <button
          onClick={() => auth.loginGithub()}
          className="mt-5 w-full flex items-center justify-center gap-2 h-10 rounded-md bg-[#24292f] hover:bg-[#32383f] text-white text-sm font-medium transition-colors"
        >
          <GitHubMark className="w-5 h-5" />
          Continue with GitHub
        </button>

        <div className="relative my-5">
          <div className="absolute inset-0 flex items-center">
            <div className="w-full border-t border-border-default" />
          </div>
          <div className="relative flex justify-center">
            <span className="bg-surface-secondary px-3 text-xs text-content-tertiary">or email + password</span>
          </div>
        </div>

        {notice && (
          <div className="mb-4 p-3 rounded-md bg-status-success-bg border border-status-success/20 text-status-success text-sm">
            {notice}
          </div>
        )}
        {error && (
          <div className="mb-4 p-3 rounded-md bg-status-error-bg border border-status-error/20 text-status-error text-sm">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-3">
          {mode === 'register' && (
            <div>
              <label className="block text-sm font-medium text-content-primary mb-1.5">Name</label>
              <input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder="Your name" className={inputClass} />
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-content-primary mb-1.5">Email</label>
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
            <label className="block text-sm font-medium text-content-primary mb-1.5">Password</label>
            <div className="relative">
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={mode === 'register' ? 'Min. 8 characters' : 'Password'}
                required
                minLength={mode === 'register' ? 8 : undefined}
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                className={`${inputClass} pr-10`}
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-content-tertiary hover:text-content-secondary transition-colors p-1"
                title={showPassword ? 'Hide password' : 'Show password'}
              >
                {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              </button>
            </div>
          </div>

          <Button type="submit" size="md" className="w-full h-10" disabled={loading} loading={loading}>
            <span className="flex items-center gap-2">
              {mode === 'login' ? 'Sign in' : 'Create account'}
              <ArrowRight className="w-4 h-4" />
            </span>
          </Button>
        </form>

        {mode === 'login' && error.toLowerCase().includes('not verified') && (
          <div className="mt-3">
            <Button variant="secondary" className="w-full h-10" onClick={resend} loading={resending} disabled={!email.trim()}>
              Resend verification email
            </Button>
          </div>
        )}

        <div className="mt-5 text-sm text-content-secondary">
          {mode === 'login' ? "Don't have an account? " : 'Already have an account? '}
          <button
            type="button"
            onClick={() => {
              setMode(mode === 'login' ? 'register' : 'login');
              setError('');
              setNotice('');
            }}
            className="text-brand hover:text-brand-hover font-medium"
          >
            {mode === 'login' ? 'Sign up' : 'Sign in'}
          </button>
        </div>

        <div className="mt-6 pt-4 border-t border-border-subtle flex items-center justify-center gap-4 text-xs text-content-tertiary">
          <button onClick={() => navigate('/docs')} className="hover:text-content-secondary transition-colors">
            Docs
          </button>
          <span className="w-1 h-1 rounded-full bg-border-default" />
          <button onClick={() => navigate('/privacy')} className="hover:text-content-secondary transition-colors">
            Privacy
          </button>
        </div>
      </div>
    </div>
  );
}
