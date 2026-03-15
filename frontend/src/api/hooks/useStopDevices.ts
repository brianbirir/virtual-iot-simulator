import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../client';
import type { StopRequest } from '../types';

export function useStopDevices() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: StopRequest) => api.stop(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['status'] });
    },
  });
}
