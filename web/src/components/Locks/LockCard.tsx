import { useState } from 'react';
import { useUnlockApp } from '../../hooks/useLocks';
import type { Lock, PlanDiffEntry } from '../../types';

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
          href={getPRUrl(lock)}
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center hover:text-primary-600"
        >
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

      {lock.plan_diffs && lock.plan_diffs.length > 0 && (
        <PlanDiffSection diffs={lock.plan_diffs} />
      )}
    </div>
  );
}

function PlanDiffSection({ diffs }: { diffs: PlanDiffEntry[] }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="mt-3 border-t border-gray-100 pt-3">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className="flex items-center text-sm text-gray-600 hover:text-gray-900"
      >
        <svg
          className={`w-4 h-4 mr-1 transition-transform ${expanded ? 'rotate-90' : ''}`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        Plan Diff ({diffs.length} resource{diffs.length !== 1 ? 's' : ''} changed)
      </button>

      {expanded && (
        <div className="mt-2 space-y-3">
          {diffs.map((d) => {
            const key = `${d.resource.apiVersion || 'core'}:${d.resource.namespace || ''}/${d.resource.kind}/${d.resource.name}:${d.action}`;
            return (
              <div key={key} className="text-sm">
                <div className="flex items-center space-x-2 font-medium text-gray-700">
                  <span>{actionIcon(d.action)}</span>
                  <span>{formatResourceKey(d.resource)}</span>
                </div>
                {d.diff && (
                  <pre className="mt-1 p-2 bg-gray-50 rounded text-xs overflow-x-auto whitespace-pre-wrap border border-gray-200">
                    {d.diff}
                  </pre>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function actionIcon(action: string): string {
  switch (action) {
    case 'create': return '+';
    case 'update': return '~';
    case 'delete': return '-';
    default: return '?';
  }
}

function formatResourceKey(r: { kind: string; name: string; namespace?: string }): string {
  if (r.namespace) {
    return `${r.namespace}/${r.kind}/${r.name}`;
  }
  return `${r.kind}/${r.name}`;
}

function getPRUrl(lock: Lock): string {
  if (lock.provider === 'gitlab') {
    if (lock.repo_url) {
      return `${lock.repo_url.replace(/\/+$/, '')}/-/merge_requests/${lock.pr_number}`;
    }
    return `https://gitlab.com/${lock.repo}/-/merge_requests/${lock.pr_number}`;
  }
  if (lock.repo_url) {
    return `${lock.repo_url.replace(/\/+$/, '')}/pull/${lock.pr_number}`;
  }
  return `https://github.com/${lock.repo}/pull/${lock.pr_number}`;
}

function getTimeAgo(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);

  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}
