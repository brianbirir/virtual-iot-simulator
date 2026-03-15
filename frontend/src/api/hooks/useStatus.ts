import { useQuery } from '@tanstack/react-query';
import { api } from '../client';

export function useStatus(refetchInterval = 5_000) {
  return useQuery({
    queryKey: ['status'],
    queryFn: api.status,
    refetchInterval,
  });
}
