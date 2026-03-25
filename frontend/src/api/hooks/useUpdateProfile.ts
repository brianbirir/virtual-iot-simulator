import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../client';
import type { ProfileUpdateRequest } from '../types';

export function useUpdateProfile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: ProfileUpdateRequest }) =>
      api.profiles.update(id, body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['profiles'] }),
  });
}
