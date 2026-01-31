export interface Lock {
  application: string;
  pr_number: number;
  repo: string;
  user: string;
  locked_at: string;
  plan_revision: string;
  plan_output?: string;
}

export interface LockListResponse {
  locks: Lock[];
  count: number;
}
