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

import { useMemo } from 'react';
import { useLocks } from '../hooks/useLocks';
import { LockList } from '../components/Locks/LockList';

export function Dashboard() {
  const { data } = useLocks();

  const stats = useMemo(() => {
    const locks = data?.locks || [];
    const repos = new Set(locks.map((l) => l.repo));
    const users = new Set(locks.map((l) => l.user));
    return {
      total: locks.length,
      repos: repos.size,
      users: users.size,
    };
  }, [data?.locks]);

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="bg-white rounded-lg border border-gray-200 p-5">
          <p className="text-sm font-medium text-gray-500">Total Locks</p>
          <p className="mt-1 text-3xl font-semibold text-gray-900">{stats.total}</p>
        </div>
        <div className="bg-white rounded-lg border border-gray-200 p-5">
          <p className="text-sm font-medium text-gray-500">Repositories</p>
          <p className="mt-1 text-3xl font-semibold text-gray-900">{stats.repos}</p>
        </div>
        <div className="bg-white rounded-lg border border-gray-200 p-5">
          <p className="text-sm font-medium text-gray-500">Users</p>
          <p className="mt-1 text-3xl font-semibold text-gray-900">{stats.users}</p>
        </div>
      </div>

      <div className="bg-white rounded-lg border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Active Locks</h2>
        <LockList />
      </div>
    </div>
  );
}
