import type { EnvVar } from '../types';

export type DeployAutomationMode = 'push' | 'workflow_success' | 'off';

export interface DeployAutomationState {
  mode: DeployAutomationMode;
  auto_deploy: boolean;
  gate_enabled: boolean;
  workflows: string[];
}

export function parseTruthyEnv(value?: string): boolean {
  const normalized = (value || '').trim().toLowerCase();
  return ['1', 'true', 'yes', 'on', 'y'].includes(normalized);
}

export function parseWorkflowNames(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of (raw || '').split(',')) {
    const trimmed = item.trim();
    if (!trimmed) continue;
    const key = trimmed.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
  }
  return out;
}

export function deriveDeployAutomationState(autoDeploy: boolean, envVars?: Array<Pick<EnvVar, 'key' | 'value'>> | null): DeployAutomationState {
  const byKey = new Map<string, string>();
  for (const envVar of envVars ?? []) {
    const key = (envVar.key || '').trim().toUpperCase();
    if (!key) continue;
    byKey.set(key, (envVar.value || '').trim());
  }

  const gateEnabled =
    parseTruthyEnv(byKey.get('RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY')) ||
    parseTruthyEnv(byKey.get('RAILPUSH_GITHUB_ACTIONS_ENABLED')) ||
    parseTruthyEnv(byKey.get('RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS'));

  const workflowRaw = (byKey.get('RAILPUSH_GITHUB_ACTIONS_WORKFLOWS') || byKey.get('RAILPUSH_GITHUB_ACTIONS_WORKFLOW') || '').trim();
  const workflows = parseWorkflowNames(workflowRaw);
  const mode: DeployAutomationMode = !autoDeploy ? 'off' : (gateEnabled ? 'workflow_success' : 'push');

  return {
    mode,
    auto_deploy: autoDeploy,
    gate_enabled: gateEnabled,
    workflows,
  };
}
