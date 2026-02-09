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

export type Role = 'admin' | 'user';

export interface User {
  id: string;
  login: string;
  email: string;
  name: string;
  avatar_url?: string;
  provider: 'github' | 'oidc';
  role: Role;
  groups?: string[];
  created_at: string;
  last_login_at: string;
}

export interface AuthProvider {
  id: string;
  name: string;
  login_url: string;
}

export interface AuthState {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  authEnabled: boolean;
  error: string | null;
}

export interface AuthContextType extends AuthState {
  login: (provider: string) => void;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}
