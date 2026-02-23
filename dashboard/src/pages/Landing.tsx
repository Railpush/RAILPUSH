import { useEffect, useRef } from 'react';

export function Landing() {
  const canvasRef = useRef<HTMLCanvasElement>(null);

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
        {/* Logo */}
        {/* Title */}
        <h1 style={{
          fontSize: 'clamp(2rem, 5vw, 3.5rem)', fontWeight: 700, letterSpacing: '-0.03em', lineHeight: 1.1,
          marginBottom: '0.75rem', fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
          background: 'linear-gradient(135deg, #ffffff 0%, #94a3b8 100%)',
          WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text',
        }}>
          BermudAir AI Platform
        </h1>

        {/* Subtitle */}
        <p style={{
          fontSize: 'clamp(1rem, 2vw, 1.25rem)', fontWeight: 400, color: 'rgba(148, 163, 184, 0.8)',
          letterSpacing: '0.12em', textTransform: 'uppercase' as const, marginBottom: '3rem',
          fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
        }}>
          Flight Intelligence
        </p>

        {/* Badge */}
        <div style={{
          display: 'inline-flex', alignItems: 'center', gap: 8, padding: '8px 20px', borderRadius: 100,
          background: 'rgba(59, 130, 246, 0.1)', border: '1px solid rgba(59, 130, 246, 0.2)',
          fontSize: '0.8125rem', fontWeight: 500, color: 'rgba(147, 197, 253, 0.9)', letterSpacing: '0.02em',
          fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, sans-serif",
        }}>
          <span style={{
            width: 6, height: 6, borderRadius: '50%', background: '#3b82f6',
            animation: 'bermudair-pulse 2s ease-in-out infinite',
          }} />
          Launching Soon
        </div>
      </div>

      <style>{`
        @keyframes bermudair-pulse {
          0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
          50% { opacity: 0.6; box-shadow: 0 0 0 6px rgba(59, 130, 246, 0); }
        }
      `}</style>
    </div>
  );
}
