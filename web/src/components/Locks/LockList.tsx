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

import { useLocks } from '../../hooks/useLocks';
import { LockCard } from './LockCard';

export function LockList() {
  const { data, isLoading, error } = useLocks();

  if (isLoading) {
    return (
      <div className="text-center py-8 text-gray-500">
        Loading locks...
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
        Failed to load locks: {error.message}
      </div>
    );
  }

  const locks = data?.locks || [];

  if (locks.length === 0) {
    return (
      <div className="text-center py-12">
        <svg
          className="mx-auto h-12 w-12 text-gray-400"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M8 11V7a4 4 0 118 0m-4 8v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2z"
          />
        </svg>
        <h3 className="mt-2 text-sm font-medium text-gray-900">No active locks</h3>
        <p className="mt-1 text-sm text-gray-500">
          Applications are locked when a PR runs <code>lemuria plan</code>.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {locks.map((lock) => (
        <LockCard key={lock.application} lock={lock} />
      ))}
    </div>
  );
}
