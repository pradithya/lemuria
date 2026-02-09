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

import { useQuery } from '@tanstack/react-query';
import { api } from '../services/api';
import { LockList } from '../components/Locks/LockList';

interface StatusResponse {
  status: string;
  version: string;
}

export function Dashboard() {
  const { data: status } = useQuery({
    queryKey: ['status'],
    queryFn: () => api.get<StatusResponse>('/api/v1/status'),
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
        <p className="text-gray-500">Argo CD PR automation overview</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div className="bg-white rounded-lg border border-gray-200 p-6">
          <div className="text-sm font-medium text-gray-500">Server Status</div>
          <div className="mt-2 flex items-center">
            <span
              className={`w-3 h-3 rounded-full mr-2 ${
                status?.status === 'running' ? 'bg-green-500' : 'bg-red-500'
              }`}
            />
            <span className="text-lg font-semibold capitalize">
              {status?.status || 'Unknown'}
            </span>
          </div>
          <div className="mt-1 text-sm text-gray-500">
            Version: {status?.version || '-'}
          </div>
        </div>

        <div className="bg-white rounded-lg border border-gray-200 p-6">
          <div className="text-sm font-medium text-gray-500">Quick Actions</div>
          <div className="mt-3 space-y-2">
            <a
              href="/locks"
              className="block text-sm text-primary-600 hover:underline"
            >
              Manage application locks
            </a>
          </div>
        </div>
      </div>

      <div className="bg-white rounded-lg border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Active Locks</h2>
        <LockList />
      </div>

      <div className="bg-white rounded-lg border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">How It Works</h2>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 text-sm">
          <div className="text-center p-4">
            <div className="w-10 h-10 bg-primary-100 rounded-full flex items-center justify-center mx-auto mb-2">
              <span className="text-primary-600 font-bold">1</span>
            </div>
            <p className="text-gray-600">Open a PR that modifies Kubernetes manifests</p>
          </div>
          <div className="text-center p-4">
            <div className="w-10 h-10 bg-primary-100 rounded-full flex items-center justify-center mx-auto mb-2">
              <span className="text-primary-600 font-bold">2</span>
            </div>
            <p className="text-gray-600">
              Lemuria automatically detects affected Argo CD apps
            </p>
          </div>
          <div className="text-center p-4">
            <div className="w-10 h-10 bg-primary-100 rounded-full flex items-center justify-center mx-auto mb-2">
              <span className="text-primary-600 font-bold">3</span>
            </div>
            <p className="text-gray-600">Review the manifest diff posted as a PR comment</p>
          </div>
          <div className="text-center p-4">
            <div className="w-10 h-10 bg-primary-100 rounded-full flex items-center justify-center mx-auto mb-2">
              <span className="text-primary-600 font-bold">4</span>
            </div>
            <p className="text-gray-600">
              Comment <code className="bg-gray-100 px-1 rounded">lemuria sync</code> to
              deploy
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
