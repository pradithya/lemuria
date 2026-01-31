import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../services/api';
import type { User, Role } from '../../types';

interface UsersResponse {
  users: User[];
  count: number;
}

export function UserManagement() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<UsersResponse>('/api/v1/users'),
  });

  const updateRoleMutation = useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) =>
      api.put(`/api/v1/users/${encodeURIComponent(userId)}/role`, { role }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });

  if (isLoading) {
    return <div className="text-center py-8 text-gray-500">Loading users...</div>;
  }

  if (error) {
    return (
      <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded">
        Failed to load users: {error.message}
      </div>
    );
  }

  const users = data?.users || [];

  return (
    <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
      <div className="px-4 py-3 border-b border-gray-200">
        <h3 className="font-medium text-gray-900">Active Users</h3>
        <p className="text-sm text-gray-500">Users with active sessions</p>
      </div>

      {users.length === 0 ? (
        <div className="text-center py-8 text-gray-500">No active users</div>
      ) : (
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                User
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Provider
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Role
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
                Last Login
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200">
            {users.map((user) => (
              <UserRow
                key={user.id}
                user={user}
                onUpdateRole={(role) =>
                  updateRoleMutation.mutate({ userId: user.id, role })
                }
                isUpdating={updateRoleMutation.isPending}
              />
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

interface UserRowProps {
  user: User;
  onUpdateRole: (role: Role) => void;
  isUpdating: boolean;
}

function UserRow({ user, onUpdateRole, isUpdating }: UserRowProps) {
  const [selectedRole, setSelectedRole] = useState<Role>(user.role);

  const handleRoleChange = (newRole: Role) => {
    setSelectedRole(newRole);
    if (newRole !== user.role) {
      onUpdateRole(newRole);
    }
  };

  return (
    <tr>
      <td className="px-4 py-3">
        <div className="flex items-center">
          {user.avatar_url && (
            <img
              src={user.avatar_url}
              alt={user.name || user.login}
              className="w-8 h-8 rounded-full mr-3"
            />
          )}
          <div>
            <div className="font-medium text-gray-900">{user.name || user.login}</div>
            <div className="text-sm text-gray-500">{user.email}</div>
          </div>
        </div>
      </td>
      <td className="px-4 py-3 text-sm text-gray-500 capitalize">{user.provider}</td>
      <td className="px-4 py-3">
        <select
          value={selectedRole}
          onChange={(e) => handleRoleChange(e.target.value as Role)}
          disabled={isUpdating}
          className="block w-24 rounded-md border-gray-300 text-sm focus:border-primary-500 focus:ring-primary-500 disabled:opacity-50"
        >
          <option value="user">User</option>
          <option value="admin">Admin</option>
        </select>
      </td>
      <td className="px-4 py-3 text-sm text-gray-500">
        {new Date(user.last_login_at).toLocaleString()}
      </td>
    </tr>
  );
}
