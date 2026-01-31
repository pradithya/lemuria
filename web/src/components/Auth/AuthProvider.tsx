import { useState, useEffect, useCallback, ReactNode } from 'react';
import { AuthContext } from '../../hooks/useAuth';
import { api } from '../../services/api';
import type { User, AuthState } from '../../types';

interface AuthProviderProps {
  children: ReactNode;
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [state, setState] = useState<AuthState>({
    user: null,
    isLoading: true,
    isAuthenticated: false,
    authEnabled: true,
    error: null,
  });

  const checkAuth = useCallback(async () => {
    try {
      // First check if auth is enabled
      const providersResponse = await api.get<{ auth_enabled: boolean }>('/auth/providers');

      if (!providersResponse.auth_enabled) {
        // Auth is disabled - allow access without authentication
        setState({
          user: null,
          isLoading: false,
          isAuthenticated: true, // Treat as authenticated when auth is disabled
          authEnabled: false,
          error: null,
        });
        return;
      }

      // Auth is enabled - check current session
      const response = await api.get<{ user: User }>('/auth/me');
      setState({
        user: response.user,
        isLoading: false,
        isAuthenticated: true,
        authEnabled: true,
        error: null,
      });
    } catch {
      setState((prev) => ({
        user: null,
        isLoading: false,
        isAuthenticated: false,
        authEnabled: prev.authEnabled,
        error: null,
      }));
    }
  }, []);

  useEffect(() => {
    checkAuth();
  }, [checkAuth]);

  const login = useCallback((provider: string) => {
    window.location.href = `/auth/${provider}/login`;
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.post('/auth/logout');
    } finally {
      setState((prev) => ({
        user: null,
        isLoading: false,
        isAuthenticated: false,
        authEnabled: prev.authEnabled,
        error: null,
      }));
      window.location.href = '/';
    }
  }, []);

  const refresh = useCallback(async () => {
    await checkAuth();
  }, [checkAuth]);

  return (
    <AuthContext.Provider value={{ ...state, login, logout, refresh }}>
      {children}
    </AuthContext.Provider>
  );
}
