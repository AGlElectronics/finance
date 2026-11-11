import { useQuery } from '@tanstack/react-query';

import { apiRequest } from './client';

export type Seller = { name: string; address: string; vat_number: string };
export type Source = 'finance' | 'den' | 'unset';

export type Settings = {
  seller: Seller;
  sources: { name: Source; address: Source; vat_number: Source };
  overrides: Seller;
  den: Seller;
  den_paired: boolean;
  currency: string;
  default_vat_rate_bp: number;
  invoice_prefix: string;
  credit_note_prefix: string;
  approval_threshold_cents: number;
  max_backdate_days: number;
  storage_configured: boolean;
};

export type SettingsInput = Partial<{ seller_name: string; seller_address: string; seller_vat_number: string }>;

export const getSettings = () => apiRequest<Settings>('/api/settings');

export const useSettings = () => useQuery({ queryKey: ['settings'], queryFn: getSettings, staleTime: 60_000 });

export const updateSettings = (input: SettingsInput) =>
  apiRequest<Settings>('/api/settings', { method: 'PUT', body: input });

export type PeriodClosure = { month: string; closed_at: string; closed_by: { name: string } };

export const listPeriods = () => apiRequest<{ closed: PeriodClosure[] }>('/api/periods').then((r) => r.closed);

export const closePeriod = (month: string) =>
  apiRequest<PeriodClosure>(`/api/periods/${month}/close`, { method: 'POST', body: {} });

export const reopenPeriod = (month: string, reason: string) =>
  apiRequest<void>(`/api/periods/${month}/reopen`, { method: 'POST', body: { reason } });

export type AuditEvent = {
  id: string;
  occurred_at: string;
  actor: { name: string; issuer: string; subject: string };
  action: string;
  entity_type: string;
  entity_id: string;
  metadata: unknown;
  before: unknown;
  after: unknown;
};

export type AuditQuery = { entity_type?: string; entity_id?: string; action?: string; actor?: string; limit?: number; offset?: number };

export const listAudit = (q: AuditQuery) =>
  apiRequest<{ events: AuditEvent[]; total: number; limit: number; offset: number }>('/api/audit', {
    query: { ...q, entity_type: q.entity_type || undefined, action: q.action || undefined, actor: q.actor || undefined },
  });
