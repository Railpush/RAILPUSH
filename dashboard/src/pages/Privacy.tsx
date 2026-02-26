import { useNavigate } from 'react-router-dom';
import { ArrowLeft, ChevronRight, Shield } from 'lucide-react';
import { SEO } from '../components/SEO';
import { Logo } from '../components/Logo';

export function Privacy() {
  const navigate = useNavigate();

  return (
    <div className="min-h-screen bg-surface-primary text-content-primary">
      <SEO
        title="Privacy Policy — RailPush"
        description="RailPush privacy policy. Learn how we collect, use, and protect your data with AES-256-GCM encryption, bcrypt passwords, and TLS security."
        canonical="https://railpush.com/privacy"
      />
      {/* Nav */}
      <nav className="fixed top-0 inset-x-0 z-50 bg-surface-primary/80 backdrop-blur-xl border-b border-border-default">
        <div className="max-w-4xl mx-auto px-6 h-14 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button onClick={() => navigate('/')} className="flex items-center gap-2.5 hover:opacity-80 transition-opacity">
              <Logo size={28} />
            </button>
            <ChevronRight className="w-4 h-4 text-content-tertiary" />
            <span className="text-sm font-medium text-content-secondary flex items-center gap-1.5">
              <Shield className="w-3.5 h-3.5" />
              Privacy Policy
            </span>
          </div>
          <button
            onClick={() => navigate('/')}
            className="text-sm font-medium text-content-secondary hover:text-content-primary transition-colors flex items-center gap-1.5"
          >
            <ArrowLeft className="w-3.5 h-3.5" />
            Back
          </button>
        </div>
      </nav>

      <main className="max-w-3xl mx-auto px-6 pt-28 pb-24">
        <div className="flex items-center gap-3 mb-2">
          <div className="w-10 h-10 rounded-xl bg-brand/10 flex items-center justify-center">
            <Shield className="w-5 h-5 text-brand" />
          </div>
          <h1 className="text-3xl font-bold tracking-tight">Privacy Policy</h1>
        </div>
        <p className="text-sm text-content-tertiary mt-2 mb-10">Last updated: February 2026</p>

        <div className="prose-custom space-y-10">
          <Section title="1. Introduction">
            RailPush ("we", "our", "us") is a self-hosted cloud platform for deploying applications. This Privacy Policy explains how we collect, use, and protect your information when you use our platform at <code>railpush.com</code>.
          </Section>

          <Section title="2. Information We Collect">
            <Subsection title="Account Information">
              When you create an account, we collect your email address, name, and a hashed password. If you sign in with GitHub, we receive your GitHub user ID, username, and avatar URL.
            </Subsection>
            <Subsection title="GitHub Access">
              If you connect your GitHub account, we store an encrypted OAuth access token to clone your repositories and list branches. This token is encrypted at rest using AES-256-GCM and is only used to interact with the GitHub API on your behalf.
            </Subsection>
            <Subsection title="Service Data">
              We store metadata about your services, databases, deploys, environment variables, and domains. Environment variable values are encrypted at rest.
            </Subsection>
            <Subsection title="Build Logs">
              Deploy build logs are stored as plain text and may contain output from your build and start commands. Do not log secrets in your build process.
            </Subsection>
          </Section>

          <Section title="3. How We Use Your Information">
            <ul className="list-disc list-inside space-y-2 text-sm text-content-secondary mt-3">
              <li>To authenticate you and provide access to your dashboard</li>
              <li>To clone your repositories and build your applications</li>
              <li>To provision and manage databases, Redis instances, and containers</li>
              <li>To route traffic to your services via custom domains</li>
              <li>To send transactional emails (deploy notifications, billing)</li>
              <li>To improve the platform and fix bugs</li>
            </ul>
          </Section>

          <Section title="4. Data Storage & Security">
            All data is stored on our self-hosted infrastructure. We use the following security measures:
            <ul className="list-disc list-inside space-y-2 text-sm text-content-secondary mt-3">
              <li>Passwords are hashed with bcrypt</li>
              <li>Environment variables and tokens are encrypted with AES-256-GCM</li>
              <li>All traffic is encrypted via TLS (HTTPS)</li>
              <li>Database access is restricted to internal services</li>
              <li>Docker containers run in isolated networks</li>
            </ul>
          </Section>

          <Section title="5. Third-Party Services">
            <Subsection title="GitHub">
              We use GitHub's OAuth API for authentication and repository access. Your use of GitHub is subject to <a href="https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement" target="_blank" rel="noopener noreferrer" className="text-brand hover:text-brand-hover transition-colors underline decoration-brand/30">GitHub's Privacy Statement</a>.
            </Subsection>
            <Subsection title="Stripe">
              If you use paid features, payment processing is handled by Stripe. We do not store your full credit card number. See <a href="https://stripe.com/privacy" target="_blank" rel="noopener noreferrer" className="text-brand hover:text-brand-hover transition-colors underline decoration-brand/30">Stripe's Privacy Policy</a>.
            </Subsection>
            <Subsection title="Docker Hub">
              Base images for builds are pulled from Docker Hub. We do not share your data with Docker.
            </Subsection>
          </Section>

          <Section title="6. Data Retention">
            Your data is retained for as long as your account is active. If you delete your account, we will remove your personal data and service metadata. Build logs and container images may be retained for up to 30 days after deletion.
          </Section>

          <Section title="7. Your Rights">
            You have the right to:
            <ul className="list-disc list-inside space-y-2 text-sm text-content-secondary mt-3">
              <li>Access the personal data we hold about you</li>
              <li>Update or correct your account information</li>
              <li>Delete your account and associated data</li>
              <li>Revoke GitHub access at any time via GitHub settings</li>
              <li>Export your service configurations</li>
            </ul>
          </Section>

          <Section title="8. Cookies">
            We use a single secure, HttpOnly session cookie for authentication. We do not use tracking cookies, analytics scripts, or third-party advertising pixels.
          </Section>

          <Section title="9. Changes to This Policy">
            We may update this Privacy Policy from time to time. Changes will be posted on this page with an updated date. Continued use of the platform after changes constitutes acceptance.
          </Section>

          <Section title="10. Contact">
            If you have questions about this Privacy Policy, you can reach us at{' '}
            <a href="mailto:privacy@railpush.com" className="text-brand hover:text-brand-hover transition-colors underline decoration-brand/30">
              privacy@railpush.com
            </a>.
          </Section>
        </div>
      </main>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className="text-lg font-semibold text-content-primary mb-3">{title}</h2>
      <div className="text-sm text-content-secondary leading-relaxed">{children}</div>
    </section>
  );
}

function Subsection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mt-4">
      <h3 className="text-sm font-semibold text-content-primary mb-1.5">{title}</h3>
      <div className="text-sm text-content-secondary leading-relaxed">{children}</div>
    </div>
  );
}
