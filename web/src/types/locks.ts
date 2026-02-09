/**
 * Copyright 2026 Lemuria Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

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
  provider?: string;
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
