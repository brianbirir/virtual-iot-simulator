import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../client';
import type { SpawnRequest } from '../types';

export function useSpawnDevices() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: SpawnRequest) => api.spawn(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['status'] });
    },
  });
}
