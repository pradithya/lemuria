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
