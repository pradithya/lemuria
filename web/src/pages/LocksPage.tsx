import { LockList } from '../components/Locks';

export function LocksPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Application Locks</h1>
        <p className="text-gray-500">
          Locks prevent concurrent changes to Argo CD applications
        </p>
      </div>
      <LockList />
    </div>
  );
}
