import { createContext, useContext } from 'react';
import type { ReactNode } from 'react';
import type { User } from '../types';

export type WorkspaceInfo = { id: string; name: string };

export type Session = {
  user: User;
  workspace?: WorkspaceInfo;
};

type SessionContextValue = {
  session: Session | null;
  user: User | null;
  workspace: WorkspaceInfo | null;
  isOps: boolean;
  refresh: () => Promise<void>;
};

const SessionContext = createContext<SessionContextValue | undefined>(undefined);

export function SessionProvider({ value, children }: { value: SessionContextValue; children: ReactNode }) {
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) {
    throw new Error('useSession must be used within a SessionProvider');
  }
  return ctx;
}

