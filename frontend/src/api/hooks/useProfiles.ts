import { useQuery } from '@tanstack/react-query';
import { api } from '../client';

export function useProfiles() {
  return useQuery({
    queryKey: ['profiles'],
    queryFn: () => api.profiles.list(),
  });
}
