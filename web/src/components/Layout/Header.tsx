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

import { useAuth } from '../../hooks';

export function Header() {
  const { user, logout } = useAuth();

  return (
    <header className="bg-white border-b border-gray-200">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between items-center h-16">
          <div className="flex items-center">
            <a href="/" className="flex items-center space-x-2">
              <img src="/lemuria.png" alt="Lemuria" className="h-10" />
              <span className="text-xl font-bold text-gray-900">Lemuria</span>
            </a>
            <nav className="ml-10 flex space-x-4">
              <a
                href="/"
                className="text-gray-600 hover:text-gray-900 px-3 py-2 text-sm font-medium"
              >
                Dashboard
              </a>
              {user?.role === 'admin' && (
                <a
                  href="/admin"
                  className="text-gray-600 hover:text-gray-900 px-3 py-2 text-sm font-medium"
                >
                  Admin
                </a>
              )}
              <a
                href="https://pradithya.github.io/lemuria/"
                target="_blank"
                rel="noopener noreferrer"
                className="text-gray-600 hover:text-gray-900 px-3 py-2 text-sm font-medium"
              >
                Docs
              </a>
            </nav>
          </div>

          {user && (
            <div className="flex items-center space-x-4">
              <div className="flex items-center space-x-2">
                {user.avatar_url && (
                  <img
                    src={user.avatar_url}
                    alt={user.name || user.login}
                    className="w-8 h-8 rounded-full"
                  />
                )}
                <div className="text-sm">
                  <p className="font-medium text-gray-900">{user.name || user.login}</p>
                  <p className="text-gray-500 text-xs capitalize">{user.role}</p>
                </div>
              </div>
              <button
                onClick={logout}
                className="text-gray-600 hover:text-gray-900 text-sm font-medium"
              >
                Sign out
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
