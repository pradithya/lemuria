import { useState } from 'react';
import { useUnlockApp } from '../../hooks/useLocks';
import type { Lock } from '../../types';

interface LockCardProps {
  lock: Lock;
}

export function LockCard({ lock }: LockCardProps) {
  const unlockMutation = useUnlockApp();
  const [confirming, setConfirming] = useState(false);

  const lockedAt = new Date(lock.locked_at);
  const timeAgo = getTimeAgo(lockedAt);

  const handleUnlock = async () => {
    if (!confirming) {
      setConfirming(true);
      return;
    }

    try {
      await unlockMutation.mutateAsync(lock.application);
      setConfirming(false);
    } catch (error) {
      console.error('Failed to unlock:', error);
    }
  };

  return (
    <div className="bg-white rounded-lg border border-gray-200 p-4 hover:shadow-md transition-shadow">
      <div className="flex justify-between items-start">
        <div className="flex-1">
          <h3 className="font-semibold text-gray-900">{lock.application}</h3>
          <p className="text-sm text-gray-500 mt-1">
            Locked by <span className="font-medium">{lock.user}</span>
          </p>
        </div>
        <button
          onClick={handleUnlock}
          disabled={unlockMutation.isPending}
          className={`px-3 py-1 text-sm rounded ${
            confirming
              ? 'bg-red-600 text-white hover:bg-red-700'
              : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
          } disabled:opacity-50`}
        >
          {unlockMutation.isPending
            ? 'Unlocking...'
            : confirming
            ? 'Confirm Unlock'
            : 'Unlock'}
        </button>
      </div>

      <div className="mt-3 flex items-center space-x-4 text-sm text-gray-500">
        <a
          href={`https://github.com/${lock.repo}/pull/${lock.pr_number}`}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center hover:text-primary-600"
        >
          <svg className="w-4 h-4 mr-1" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
          </svg>
          PR #{lock.pr_number}
        </a>
        <span className="text-gray-300">|</span>
        <span>{lock.repo}</span>
        <span className="text-gray-300">|</span>
        <span title={lockedAt.toLocaleString()}>{timeAgo}</span>
      </div>

      {lock.plan_revision && (
        <div className="mt-2 text-xs text-gray-400">
          Revision: {lock.plan_revision.substring(0, 7)}
        </div>
      )}
    </div>
  );
}

function getTimeAgo(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);

  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}
