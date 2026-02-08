export interface ResourceKey {
  apiVersion: string;
  kind: string;
  name: string;
  namespace?: string;
}

export interface PlanDiffEntry {
  resource: ResourceKey;
  action: string;
  diff: string;
}

export interface Lock {
  application: string;
  pr_number: number;
  repo: string;
  repo_url?: string;
  user: string;
  locked_at: string;
  plan_revision: string;
  plan_output?: string;
  plan_diffs?: PlanDiffEntry[];
}

export interface LockListResponse {
  locks: Lock[];
  count: number;
}
