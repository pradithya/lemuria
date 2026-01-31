import { UserManagement } from '../components/Admin';

export function AdminPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Administration</h1>
        <p className="text-gray-500">Manage users and system settings</p>
      </div>
      <UserManagement />
    </div>
  );
}
