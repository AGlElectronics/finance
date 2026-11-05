import { apiRequest } from './client';
import type { InvoiceSummary } from './invoices';

export type MonthFlow = {
  month: string; // YYYY-MM
  in_cents: number;
  out_cents: number;
};

export type Dashboard = {
  outstanding_cents: number;
  overdue_cents: number;
  overdue_count: number;
  received_month_cents: number;
  received_total_cents: number;
  spent_month_cents: number;
  spent_total_cents: number;
  net_total_cents: number;
  draft_count: number;
  open_count: number;
  months: MonthFlow[];
  recent_invoices: InvoiceSummary[];
};

export const getDashboard = () => apiRequest<Dashboard>('/api/dashboard');
