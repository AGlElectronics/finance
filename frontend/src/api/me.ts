import { useQuery } from '@tanstack/react-query';

import { apiRequest } from './client';

export type Permission = 'finance.read' | 'finance.write' | 'finance.approve' | 'finance.audit' | 'finance.admin';

export type Me = {
  username: string;
  issuer?: string;
  subject?: string;
  auth_mode: string;
  has_access: boolean;
  groups: string[];
  roles: string[];
  permissions: Permission[];
};

export const getMe = () => apiRequest<Me>('/api/me');

export const useMe = () => useQuery({ queryKey: ['me'], queryFn: getMe });

/** Whether the signed-in person may do something; the backend checks it again on every route. */
export const can = (me: Me | undefined, p: Permission) => Boolean(me?.permissions?.includes(p));

export function useCan(p: Permission): boolean {
  return can(useMe().data, p);
}
