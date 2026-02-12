export type PlanID = 'free' | 'starter' | 'standard' | 'pro';

export interface PlanSpec {
  id: PlanID;
  name: string;
  cpu: string;
  mem: string;
  priceLabel: string;
  monthlyCents: number;
  description: string;
  features: string[];
  highlighted?: boolean;
  badge?: string;
}

export const PLAN_SPECS: PlanSpec[] = [
  {
    id: 'free',
    name: 'Free',
    cpu: '0.1 CPU',
    mem: '256 MB',
    priceLabel: '$0/mo',
    monthlyCents: 0,
    description: 'For experiments and personal projects',
    features: [
      '1 free service per workspace',
      '1 free PostgreSQL database',
      '1 free key value store',
      'Free TLS and default subdomain',
    ],
  },
  {
    id: 'starter',
    name: 'Starter',
    cpu: '0.5 CPU',
    mem: '512 MB',
    priceLabel: '$7/mo',
    monthlyCents: 700,
    description: 'For small production workloads',
    features: [
      'Always-on services',
      'Custom domains',
      'Background workers and cron jobs',
      'Project and environment grouping',
    ],
  },
  {
    id: 'standard',
    name: 'Standard',
    cpu: '1 CPU',
    mem: '2 GB',
    priceLabel: '$25/mo',
    monthlyCents: 2500,
    description: 'For growing applications',
    features: [
      'More CPU and memory per service',
      'Managed PostgreSQL and key value stores',
      'Autoscaling policy support',
      'Blueprint-driven provisioning',
    ],
    highlighted: true,
    badge: 'Most Popular',
  },
  {
    id: 'pro',
    name: 'Pro',
    cpu: '2 CPU',
    mem: '4 GB',
    priceLabel: '$85/mo',
    monthlyCents: 8500,
    description: 'For higher-throughput production apps',
    features: [
      'Highest compute tier currently available',
      'Advanced autoscaling configurations',
      'Workspace-level audit logs',
      'Best fit for sustained traffic',
    ],
  },
];

export const PLAN_BY_ID: Record<PlanID, PlanSpec> = PLAN_SPECS.reduce((acc, plan) => {
  acc[plan.id] = plan;
  return acc;
}, {} as Record<PlanID, PlanSpec>);
