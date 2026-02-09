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
