import { apiRequest } from './client';

export type Customer = {
  id: string;
  name: string;
  email: string;
  billing_address: string;
  tax_id: string;
  notes: string;
  archived: boolean;
  created_at: string;
  updated_at: string;
  invoice_count: number;
  outstanding_cents: number;
};

export type CustomerInput = Partial<{
  name: string;
  email: string;
  billing_address: string;
  tax_id: string;
  notes: string;
  archived: boolean;
}>;

export const listCustomers = (includeArchived = false) =>
  apiRequest<{ customers: Customer[] }>('/api/customers', {
    query: includeArchived ? { include_archived: true } : undefined,
  }).then((r) => r.customers);

export const getCustomer = (id: string) => apiRequest<Customer>(`/api/customers/${id}`);

export const createCustomer = (input: CustomerInput) =>
  apiRequest<Customer>('/api/customers', { method: 'POST', body: input });

export const updateCustomer = (id: string, input: CustomerInput) =>
  apiRequest<Customer>(`/api/customers/${id}`, { method: 'PATCH', body: input });

export const deleteCustomer = (id: string) =>
  apiRequest<void>(`/api/customers/${id}`, { method: 'DELETE' });
