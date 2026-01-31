import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../services/api';
import type { LockListResponse } from '../types';

export function useLocks() {
  return useQuery({
    queryKey: ['locks'],
    queryFn: () => api.get<LockListResponse>('/api/v1/locks'),
    refetchInterval: 30000,
  });
}

export function useUnlockApp() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (app: string) =>
      api.delete<{ status: string }>(`/api/v1/locks/${encodeURIComponent(app)}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['locks'] });
    },
  });
}
