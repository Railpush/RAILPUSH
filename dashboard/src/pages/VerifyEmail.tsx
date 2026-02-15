import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { CheckCircle, XCircle } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Logo } from '../components/Logo';
import { ApiError, auth } from '../lib/api';
import { SEO } from '../components/SEO';

type VerifyState = 'loading' | 'ok' | 'error';

export function VerifyEmail() {
  const navigate = useNavigate();
  const token = useMemo(() => new URLSearchParams(window.location.search).get('token') || '', []);
  const [state, setState] = useState<VerifyState>('loading');
  const [message, setMessage] = useState('');

  useEffect(() => {
    if (!token) {
      setState('error');
      setMessage('Missing verification token.');
      return;
    }
    auth
      .verifyEmail(token)
      .then(() => {
        setState('ok');
        setMessage('Email verified. You can sign in now.');
      })
      .catch((e: unknown) => {
        setState('error');
        if (e instanceof ApiError) setMessage(e.message);
        else setMessage(e instanceof Error ? e.message : 'Verification failed');
      });
  }, [token]);

  return (
    <div className="min-h-screen bg-surface-primary text-content-primary flex items-center justify-center px-4 py-10">
      <SEO
        title="Verify Email — RailPush"
        description="Verify your RailPush email address."
        canonical="https://railpush.com/verify"
      />

      <div className="w-full max-w-[460px] bg-surface-secondary border border-border-default rounded-lg p-6">
        <div className="flex items-center justify-between gap-3">
          <button onClick={() => navigate('/')} className="hover:opacity-80 transition-opacity">
            <Logo size={34} />
          </button>
          <div className="text-xs text-content-tertiary">railpush.com</div>
        </div>

        <div className="mt-6">
          <h1 className="text-lg font-semibold">Email verification</h1>
          <p className="text-sm text-content-secondary mt-1">Confirm your email to activate your account.</p>
        </div>

        <div className="mt-5 p-4 rounded-md border border-border-default bg-surface-tertiary/30 flex gap-3">
          {state === 'ok' ? (
            <CheckCircle className="w-5 h-5 text-status-success shrink-0 mt-0.5" />
          ) : state === 'error' ? (
            <XCircle className="w-5 h-5 text-status-error shrink-0 mt-0.5" />
          ) : (
            <div className="w-5 h-5 rounded-full border-2 border-content-tertiary border-t-transparent animate-spin shrink-0 mt-0.5" />
          )}
          <div className="flex-1">
            <div className="text-sm font-medium text-content-primary">
              {state === 'loading' ? 'Verifying…' : state === 'ok' ? 'Verified' : 'Could not verify'}
            </div>
            <div className="text-sm text-content-secondary mt-1">{message || ' '}</div>
          </div>
        </div>

        <div className="mt-5 flex gap-3">
          <Button className="flex-1 h-10" onClick={() => navigate('/login')}>
            Go to sign in
          </Button>
          <Button variant="secondary" className="flex-1 h-10" onClick={() => navigate('/')}>
            Home
          </Button>
        </div>
      </div>
    </div>
  );
}

