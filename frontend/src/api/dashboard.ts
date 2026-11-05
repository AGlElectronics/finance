import { apiRequest } from './client';
import type { InvoiceSummary } from './invoices';

export type Dashboard = {
  outstanding_cents: number;
  overdue_cents: number;
  overdue_count: number;
  received_month_cents: number;
  received_total_cents: number;
  draft_count: number;
  open_count: number;
  recent_invoices: InvoiceSummary[];
};

export const getDashboard = () => apiRequest<Dashboard>('/api/dashboard');
