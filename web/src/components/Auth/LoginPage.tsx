import { useEffect, useState, FormEvent } from 'react';
import { api } from '../../services/api';
import { useAuth } from '../../hooks';
import type { AuthProvider } from '../../types';

export function LoginPage() {
  const { refresh } = useAuth();
  const [providers, setProviders] = useState<AuthProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showBasicForm, setShowBasicForm] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    async function fetchProviders() {
      try {
        const response = await api.get<{ providers: AuthProvider[] }>('/auth/providers');
        setProviders(response.providers || []);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load providers');
      } finally {
        setLoading(false);
      }
    }
    fetchProviders();
  }, []);

  const handleBasicLogin = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    try {
      await api.post('/auth/basic/login', { username, password });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  const hasBasicAuth = providers.some(p => p.id === 'basic');
  const oauthProviders = providers.filter(p => p.id !== 'basic');

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="max-w-md w-full space-y-8 p-8">
        <div className="text-center">
          <img src="/logo.png" alt="Lemuria" className="h-80 mx-auto mb-4" />
          <p className="text-gray-600">Argo CD PR Automation</p>
        </div>

        {error && (
          <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
            {error}
          </div>
        )}

        <div className="space-y-4">
          {providers.length === 0 ? (
            <p className="text-center text-gray-500">
              No authentication providers configured.
            </p>
          ) : (
            <>
              {/* OAuth providers */}
              {oauthProviders.map((provider) => (
                <a
                  key={provider.id}
                  href={provider.login_url}
                  className="w-full flex items-center justify-center px-4 py-3 border border-gray-300 rounded-lg shadow-sm bg-white hover:bg-gray-50 transition-colors"
                >
                  {provider.id === 'github' && (
                    <svg className="w-5 h-5 mr-3" fill="currentColor" viewBox="0 0 24 24">
                      <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
                    </svg>
                  )}
                  <span className="font-medium">Sign in with {provider.name}</span>
                </a>
              ))}

              {/* Basic auth form */}
              {hasBasicAuth && (
                <>
                  {oauthProviders.length > 0 && (
                    <div className="relative">
                      <div className="absolute inset-0 flex items-center">
                        <div className="w-full border-t border-gray-300" />
                      </div>
                      <div className="relative flex justify-center text-sm">
                        <span className="px-2 bg-gray-50 text-gray-500">Or</span>
                      </div>
                    </div>
                  )}

                  {!showBasicForm ? (
                    <button
                      onClick={() => setShowBasicForm(true)}
                      className="w-full flex items-center justify-center px-4 py-3 border border-gray-300 rounded-lg shadow-sm bg-white hover:bg-gray-50 transition-colors"
                    >
                      <svg className="w-5 h-5 mr-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                      </svg>
                      <span className="font-medium">Sign in with Username</span>
                    </button>
                  ) : (
                    <form onSubmit={handleBasicLogin} className="space-y-4">
                      <div>
                        <label htmlFor="username" className="block text-sm font-medium text-gray-700">
                          Username
                        </label>
                        <input
                          id="username"
                          type="text"
                          value={username}
                          onChange={(e) => setUsername(e.target.value)}
                          className="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-lg shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                          required
                          autoFocus
                        />
                      </div>
                      <div>
                        <label htmlFor="password" className="block text-sm font-medium text-gray-700">
                          Password
                        </label>
                        <input
                          id="password"
                          type="password"
                          value={password}
                          onChange={(e) => setPassword(e.target.value)}
                          className="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-lg shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                          required
                        />
                      </div>
                      <button
                        type="submit"
                        disabled={submitting}
                        className="w-full flex items-center justify-center px-4 py-3 border border-transparent rounded-lg shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        {submitting ? 'Signing in...' : 'Sign in'}
                      </button>
                      <button
                        type="button"
                        onClick={() => setShowBasicForm(false)}
                        className="w-full text-sm text-gray-500 hover:text-gray-700"
                      >
                        Back to other options
                      </button>
                    </form>
                  )}
                </>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
