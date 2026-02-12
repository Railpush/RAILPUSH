import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

type Theme = 'light' | 'dark';
type Surface = 'slate' | 'frost';

interface ThemeContextValue {
  theme: Theme;
  surface: Surface;
  toggleTheme: () => void;
  toggleSurface: () => void;
  setTheme: (t: Theme) => void;
  setSurface: (s: Surface) => void;
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined);

const THEME_KEY = 'railpush-theme';
const SURFACE_KEY = 'railpush-surface';

function getInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'light';
  const stored = window.localStorage.getItem(THEME_KEY) as Theme | null;
  if (stored === 'light' || stored === 'dark') return stored;
  const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches;
  return prefersDark ? 'dark' : 'light';
}

function getInitialSurface(): Surface {
  if (typeof window === 'undefined') return 'slate';
  const stored = window.localStorage.getItem(SURFACE_KEY) as Surface | null;
  if (stored === 'slate' || stored === 'frost') return stored;
  return 'slate';
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(getInitialTheme);
  const [surface, setSurface] = useState<Surface>(getInitialSurface);

  useEffect(() => {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    if (theme === 'dark') {
      root.classList.add('theme-dark');
    } else {
      root.classList.remove('theme-dark');
    }
    window.localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  useEffect(() => {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    if (surface === 'frost') {
      root.classList.add('theme-frost');
    } else {
      root.classList.remove('theme-frost');
    }
    window.localStorage.setItem(SURFACE_KEY, surface);
  }, [surface]);

  const value = useMemo(
    () => ({
      theme,
      surface,
      setTheme,
      setSurface,
      toggleTheme: () => setTheme((prev) => (prev === 'dark' ? 'light' : 'dark')),
      toggleSurface: () => setSurface((prev) => (prev === 'frost' ? 'slate' : 'frost')),
    }),
    [theme, surface]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider');
  return ctx;
}
