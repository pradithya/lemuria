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

import { useState, useMemo } from 'react';
import { useLocks } from '../../hooks/useLocks';
import { LockCard } from './LockCard';

const PAGE_SIZE = 10;

export function LockList() {
  const { data, isLoading, error } = useLocks();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');

  const filteredLocks = useMemo(() => {
    const locks = data?.locks || [];
    if (!search.trim()) return locks;
    const q = search.toLowerCase();
    return locks.filter(
      (lock) =>
        lock.application.toLowerCase().includes(q) ||
        lock.user.toLowerCase().includes(q) ||
        lock.repo.toLowerCase().includes(q) ||
        String(lock.pr_number).includes(q)
    );
  }, [data?.locks, search]);

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

  const allLocks = data?.locks || [];

  if (allLocks.length === 0) {
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

  const totalPages = Math.ceil(filteredLocks.length / PAGE_SIZE);
  const safePage = Math.min(page, totalPages || 1);
  const start = (safePage - 1) * PAGE_SIZE;
  const paginatedLocks = filteredLocks.slice(start, start + PAGE_SIZE);

  const maxPageButtons = 5;
  let pageStart = Math.max(1, safePage - Math.floor(maxPageButtons / 2));
  const pageEnd = Math.min(totalPages, pageStart + maxPageButtons - 1);
  if (pageEnd - pageStart + 1 < maxPageButtons) {
    pageStart = Math.max(1, pageEnd - maxPageButtons + 1);
  }
  const pageNumbers = Array.from(
    { length: pageEnd - pageStart + 1 },
    (_, i) => pageStart + i
  );

  return (
    <div className="space-y-4">
      <div className="relative">
        <svg
          className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
          />
        </svg>
        <input
          type="text"
          placeholder="Search by application, user, repo, or PR number..."
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
          className="w-full pl-10 pr-4 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-primary-500"
        />
      </div>

      {filteredLocks.length === 0 ? (
        <div className="text-center py-8 text-gray-500 text-sm">
          No locks matching &ldquo;{search}&rdquo;
        </div>
      ) : (
        <>
          {paginatedLocks.map((lock) => (
            <LockCard key={lock.application} lock={lock} />
          ))}

          {totalPages > 1 && (
            <div className="flex items-center justify-between border-t border-gray-200 pt-4">
              <p className="text-sm text-gray-500">
                Showing {start + 1}–{Math.min(start + PAGE_SIZE, filteredLocks.length)} of{' '}
                {filteredLocks.length} locks
                {search && ` (filtered from ${allLocks.length})`}
              </p>
              <div className="flex space-x-2">
                <button
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={safePage === 1}
                  className="px-3 py-1 text-sm border border-gray-300 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50"
                >
                  Previous
                </button>
                {pageStart > 1 && (
                  <>
                    <button
                      onClick={() => setPage(1)}
                      className="px-3 py-1 text-sm border border-gray-300 rounded-md hover:bg-gray-50"
                    >
                      1
                    </button>
                    {pageStart > 2 && (
                      <span className="px-2 py-1 text-sm text-gray-400">...</span>
                    )}
                  </>
                )}
                {pageNumbers.map((p) => (
                  <button
                    key={p}
                    onClick={() => setPage(p)}
                    className={`px-3 py-1 text-sm border rounded-md ${
                      p === safePage
                        ? 'bg-primary-600 text-white border-primary-600'
                        : 'border-gray-300 hover:bg-gray-50'
                    }`}
                  >
                    {p}
                  </button>
                ))}
                {pageEnd < totalPages && (
                  <>
                    {pageEnd < totalPages - 1 && (
                      <span className="px-2 py-1 text-sm text-gray-400">...</span>
                    )}
                    <button
                      onClick={() => setPage(totalPages)}
                      className="px-3 py-1 text-sm border border-gray-300 rounded-md hover:bg-gray-50"
                    >
                      {totalPages}
                    </button>
                  </>
                )}
                <button
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  disabled={safePage === totalPages}
                  className="px-3 py-1 text-sm border border-gray-300 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-gray-50"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
