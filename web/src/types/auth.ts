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
