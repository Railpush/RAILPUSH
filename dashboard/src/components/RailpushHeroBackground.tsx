import { useEffect, useRef } from 'react';

type Props = {
  className?: string;
};

// Three.js hero background adapted from /railpush.html.
export function RailpushHeroBackground({ className = '' }: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const rafRef = useRef<number | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const prefersReducedMotion =
      typeof window !== 'undefined' &&
      !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches;

    let disposed = false;
    let cleanup: (() => void) | undefined;

    (async () => {
      const THREE = await import('three');
      if (disposed) return;

      const scene = new THREE.Scene();
      scene.fog = new THREE.FogExp2(0x020617, 0.035);

      const camera = new THREE.PerspectiveCamera(60, 1, 0.1, 100);
      camera.position.set(0, -14, 8);
      camera.lookAt(0, 0, 0);

      const renderer = new THREE.WebGLRenderer({
        alpha: true,
        antialias: true,
        powerPreference: 'high-performance',
      });
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
      renderer.setClearColor(0x000000, 0);
      container.appendChild(renderer.domElement);

      const gridSize = 100;
      const divisions = 80;
      const step = gridSize / divisions;

      const geometry = new THREE.BufferGeometry();
      const vertices: number[] = [];

      // Lines along X
      for (let i = 0; i <= divisions; i++) {
        const y = -gridSize / 2 + i * step;
        for (let j = 0; j < divisions; j++) {
          const x1 = -gridSize / 2 + j * step;
          const x2 = -gridSize / 2 + (j + 1) * step;
          vertices.push(x1, y, 0);
          vertices.push(x2, y, 0);
        }
      }

      // Lines along Y
      for (let i = 0; i <= divisions; i++) {
        const x = -gridSize / 2 + i * step;
        for (let j = 0; j < divisions; j++) {
          const y1 = -gridSize / 2 + j * step;
          const y2 = -gridSize / 2 + (j + 1) * step;
          vertices.push(x, y1, 0);
          vertices.push(x, y2, 0);
        }
      }

      geometry.setAttribute('position', new THREE.Float32BufferAttribute(vertices, 3));

      const material = new THREE.LineBasicMaterial({
        color: 0x818cf8, // indigo-400
        transparent: true,
        opacity: 0.12,
        blending: THREE.AdditiveBlending,
      });

      const grid = new THREE.LineSegments(geometry, material);
      grid.rotation.x = -Math.PI / 3;
      scene.add(grid);

      const clock = new THREE.Clock();
      const raycaster = new THREE.Raycaster();
      const mouse = new THREE.Vector2(9999, 9999);
      const targetPoint = new THREE.Vector3();
      const plane = new THREE.Plane(new THREE.Vector3(0, 0.5, 1).normalize(), 0);

      const onMouseMove = (event: MouseEvent) => {
        const w = window.innerWidth || 1;
        const h = window.innerHeight || 1;
        mouse.x = (event.clientX / w) * 2 - 1;
        mouse.y = -(event.clientY / h) * 2 + 1;
      };
      window.addEventListener('mousemove', onMouseMove, { passive: true });

      const onResize = () => {
        const w = container.clientWidth || window.innerWidth || 1;
        const h = container.clientHeight || window.innerHeight || 1;
        renderer.setSize(w, h);
        camera.aspect = w / h;
        camera.updateProjectionMatrix();
      };
      window.addEventListener('resize', onResize, { passive: true });
      onResize();

      let running = false;
      const renderFrame = () => {
        if (!running) return;

        const time = clock.getElapsedTime();
        const positionAttribute = geometry.getAttribute('position') as unknown as {
          array: Float32Array;
          needsUpdate: boolean;
        };
        const positions = positionAttribute.array;

        raycaster.setFromCamera(mouse, camera);
        raycaster.ray.intersectPlane(plane, targetPoint);

        for (let i = 0; i < positions.length; i += 3) {
          const x = positions[i];
          const y = positions[i + 1];
          const z = positions[i + 2];

          const waveZ = Math.sin(x * 0.1 + time * 0.4) * Math.cos(y * 0.1 + time * 0.2) * 0.5;

          const dx = x - targetPoint.x;
          const dy = (y - targetPoint.y) * 0.6;
          const dist = Math.sqrt(dx * dx + dy * dy);

          let mouseInfluence = 0;
          if (dist < 12) {
            mouseInfluence = (1 - dist / 12) * -2.5;
          }

          const targetZ = waveZ + mouseInfluence;
          positions[i + 2] += (targetZ - z) * 0.05;
        }

        positionAttribute.needsUpdate = true;
        renderer.render(scene, camera);

        rafRef.current = window.requestAnimationFrame(renderFrame);
      };

      const stop = () => {
        running = false;
        if (rafRef.current != null) {
          window.cancelAnimationFrame(rafRef.current);
          rafRef.current = null;
        }
      };

      const start = () => {
        if (running) return;
        running = true;

        // Render at least once (even if reduced motion).
        renderer.render(scene, camera);

        if (!prefersReducedMotion) {
          rafRef.current = window.requestAnimationFrame(renderFrame);
        }
      };

      const observer = new IntersectionObserver(
        (entries) => {
          const anyVisible = entries.some((e) => e.isIntersecting);
          if (anyVisible) start();
          else stop();
        },
        { threshold: 0.01 }
      );
      observer.observe(container);

      start();

      cleanup = () => {
        stop();
        observer.disconnect();
        window.removeEventListener('mousemove', onMouseMove);
        window.removeEventListener('resize', onResize);
        scene.remove(grid);
        geometry.dispose();
        material.dispose();
        renderer.dispose();
        renderer.domElement.remove();
      };
    })().catch(() => {
      // best-effort background; ignore runtime failures
    });

    return () => {
      disposed = true;
      cleanup?.();
    };
  }, []);

  return <div ref={containerRef} className={className} aria-hidden="true" />;
}
