import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../client';
import type { ProfileCreateRequest } from '../types';

export function useCreateProfile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: ProfileCreateRequest) => api.profiles.create(body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['profiles'] }),
  });
}
