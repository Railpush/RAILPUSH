-- AI fix session tracking
CREATE TABLE IF NOT EXISTS ai_fix_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  service_id UUID NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  max_attempts INT NOT NULL DEFAULT 3,
  current_attempt INT NOT NULL DEFAULT 0,
  last_deploy_id TEXT,
  last_ai_summary TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Store AI-generated Dockerfile override per deploy
ALTER TABLE deploys ADD COLUMN IF NOT EXISTS dockerfile_override TEXT NOT NULL DEFAULT '';
