import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';

export function Landing() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let w = 0;
    let h = 0;
    let animId = 0;
    const mouse = { x: -1000, y: -1000 };

    const resize = () => {
      w = canvas.width = window.innerWidth;
      h = canvas.height = window.innerHeight;
    };

    const onMouse = (e: MouseEvent) => {
      mouse.x = e.clientX;
      mouse.y = e.clientY;
    };

    class Particle {
      x = 0; y = 0; size = 0; speedX = 0; speedY = 0; opacity = 0; hue = 0;
      constructor() { this.reset(); }
      reset() {
        this.x = Math.random() * w;
        this.y = Math.random() * h;
        this.size = Math.random() * 1.5 + 0.5;
        this.speedX = (Math.random() - 0.5) * 0.3;
        this.speedY = (Math.random() - 0.5) * 0.3;
        this.opacity = Math.random() * 0.5 + 0.1;
        this.hue = 210 + Math.random() * 30;
      }
      update() {
        this.x += this.speedX;
        this.y += this.speedY;
        const dx = mouse.x - this.x;
        const dy = mouse.y - this.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < 200) {
          this.x += dx * 0.0005;
          this.y += dy * 0.0005;
          this.opacity = Math.min(this.opacity + 0.005, 0.8);
        } else {
          this.opacity = Math.max(this.opacity - 0.002, 0.1);
        }
        if (this.x < -10 || this.x > w + 10 || this.y < -10 || this.y > h + 10) this.reset();
      }
      draw() {
        ctx!.beginPath();
        ctx!.arc(this.x, this.y, this.size, 0, Math.PI * 2);
        ctx!.fillStyle = `hsla(${this.hue}, 80%, 70%, ${this.opacity})`;
        ctx!.fill();
      }
    }

    resize();
    const count = Math.min(Math.floor((w * h) / 8000), 200);
    const particles = Array.from({ length: count }, () => new Particle());

    const animate = () => {
      ctx.clearRect(0, 0, w, h);
      const grd = ctx.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, w * 0.6);
      grd.addColorStop(0, 'rgba(30, 64, 175, 0.08)');
      grd.addColorStop(1, 'rgba(10, 22, 40, 0)');
      ctx.fillStyle = grd;
      ctx.fillRect(0, 0, w, h);

      particles.forEach(p => { p.update(); p.draw(); });

      for (let i = 0; i < particles.length; i++) {
        for (let j = i + 1; j < particles.length; j++) {
          const dx = particles[i].x - particles[j].x;
          const dy = particles[i].y - particles[j].y;
          const dist = Math.sqrt(dx * dx + dy * dy);
          if (dist < 120) {
            const opacity = (1 - dist / 120) * 0.12;
            ctx.beginPath();
            ctx.moveTo(particles[i].x, particles[i].y);
            ctx.lineTo(particles[j].x, particles[j].y);
            ctx.strokeStyle = `rgba(59, 130, 246, ${opacity})`;
            ctx.lineWidth = 0.5;
            ctx.stroke();
          }
        }
      }
      animId = requestAnimationFrame(animate);
    };

    window.addEventListener('resize', resize);
    window.addEventListener('mousemove', onMouse);
    animate();

    return () => {
      cancelAnimationFrame(animId);
      window.removeEventListener('resize', resize);
      window.removeEventListener('mousemove', onMouse);
    };
  }, []);

  return (
    <div style={{ background: '#0a1628', width: '100vw', height: '100vh', overflow: 'hidden', position: 'relative' }}>
      <canvas ref={canvasRef} style={{ position: 'fixed', top: 0, left: 0, width: '100%', height: '100%', zIndex: 0 }} />

      <div style={{
        position: 'relative', zIndex: 1, display: 'flex', flexDirection: 'column',
        alignItems: 'center', justifyContent: 'center', height: '100vh', textAlign: 'center', padding: '2rem',
      }}>
        {/* BermudAir Logo - all white */}
        <svg viewBox="0 0 900 120" xmlns="http://www.w3.org/2000/svg" style={{ width: 'clamp(320px, 50vw, 600px)', marginBottom: '1rem' }}>
          {/* B */}
          <path d="M0 10 h40 q25 0 25 25 q0 15 -15 20 q20 5 20 25 q0 30 -30 30 H0 Z M18 48 h20 q12 0 12-13 q0-13-12-13 H18 Z M18 98 h22 q15 0 15-15 q0-15-15-15 H18 Z" fill="white"/>
          {/* E */}
          <path d="M90 10 h55 v15 h-37 v22 h32 v15 h-32 v26 h38 v15 H90 Z" fill="white"/>
          {/* R */}
          <path d="M165 10 h40 q28 0 28 27 q0 20-16 25 l22 48 h-20 l-20-45 h-16 v45 h-18 Z M183 52 h20 q13 0 13-14 q0-13-13-13 h-20 Z" fill="white"/>
          {/* M */}
          <path d="M250 10 h20 l22 50 l22-50 h20 v100 h-17 V35 l-22 50 h-8 l-22-50 v75 h-15 Z" fill="white"/>
          {/* U */}
          <path d="M355 10 h18 v65 q0 26 22 26 q22 0 22-26 V10 h18 v67 q0 38-40 38 q-40 0-40-38 Z" fill="white"/>
          {/* D */}
          <path d="M455 10 h35 q50 0 50 50 q0 50-50 50 h-35 Z M473 95 h15 q34 0 34-35 q0-35-34-35 h-15 Z" fill="white"/>
          {/* Stylized A (triangle with swoosh) */}
          <g transform="translate(555, 0)">
            {/* Triangle A shape */}
            <path d="M45 5 L90 110 L78 110 L65 78 L25 78 L12 110 L0 110 Z M45 22 L30 68 L60 68 Z" fill="white"/>
            {/* Swoosh inside A */}
            <path d="M22 88 Q45 72 68 88" fill="none" stroke="white" strokeWidth="4.5" strokeLinecap="round"/>
          </g>
          {/* I */}
          <path d="M665 10 h18 v100 h-18 Z" fill="white"/>
          {/* R */}
          <path d="M705 10 h40 q28 0 28 27 q0 20-16 25 l22 48 h-20 l-20-45 h-16 v45 h-18 Z M723 52 h20 q13 0 13-14 q0-13-13-13 h-20 Z" fill="white"/>
        </svg>

        {/* Subtitle */}
        <p style={{
          fontSize: 'clamp(1rem, 2vw, 1.25rem)', fontWeight: 400, color: 'rgba(148, 163, 184, 0.8)',
          letterSpacing: '0.12em', textTransform: 'uppercase' as const, marginBottom: '3rem',
          fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
        }}>
          Flight Intelligence
        </p>

        {/* Login Button */}
        <button
          onClick={() => navigate('/login')}
          style={{
            padding: '12px 48px',
            borderRadius: 8,
            background: 'rgba(255, 255, 255, 0.1)',
            border: '1px solid rgba(255, 255, 255, 0.2)',
            color: '#ffffff',
            fontSize: '1rem',
            fontWeight: 500,
            fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
            letterSpacing: '0.04em',
            cursor: 'pointer',
            transition: 'all 0.2s ease',
            backdropFilter: 'blur(12px)',
            WebkitBackdropFilter: 'blur(12px)',
          }}
          onMouseEnter={e => {
            e.currentTarget.style.background = 'rgba(255, 255, 255, 0.18)';
            e.currentTarget.style.borderColor = 'rgba(255, 255, 255, 0.4)';
          }}
          onMouseLeave={e => {
            e.currentTarget.style.background = 'rgba(255, 255, 255, 0.1)';
            e.currentTarget.style.borderColor = 'rgba(255, 255, 255, 0.2)';
          }}
        >
          Login
        </button>
      </div>
    </div>
  );
}
