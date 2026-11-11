import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Plus } from 'lucide-react';

import { listCustomers } from '@/api/customers';
import { INVOICE_STATUSES, listInvoices, type InvoiceStatus } from '@/api/invoices';
import { useCan } from '@/api/me';
import { InvoiceStatusBadge } from '@/components/InvoiceStatusBadge';
import { PAGE_SIZE, Pagination } from '@/components/Pagination';
import { SearchInput } from '@/components/SearchInput';
import { formatCents } from '@/lib/money';

export function InvoicesPage() {
  const { status, customer } = useSearch({ from: '/app/invoices' });
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const canWrite = useCan('finance.write');

  const invoices = useQuery({
    queryKey: ['invoices', status ?? '', customer ?? '', offset],
    queryFn: () => listInvoices({ status, customerId: customer, limit: PAGE_SIZE, offset }),
  });
  const customers = useQuery({ queryKey: ['customers', true], queryFn: () => listCustomers(true) });
  const customerName = customer ? customers.data?.find((c) => c.id === customer)?.name : undefined;

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const all = invoices.data?.invoices ?? [];
    if (!q) return all;
    return all.filter((i) => `${i.number ?? ''} ${i.customer_name}`.toLowerCase().includes(q));
  }, [invoices.data, search]);

  return (
    <div className="p-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Invoices</h1>
          <p className="text-sm text-muted-foreground">
            {customerName ? <>Showing invoices for <span className="font-medium text-foreground">{customerName}</span>.</> : 'Drafts, issued invoices and what came in.'}
          </p>
        </div>
        {canWrite && (
          <Link to="/invoices/new"
            className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground">
            <Plus className="h-4 w-4" /> New invoice
          </Link>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <SearchInput value={search} onChange={setSearch} placeholder="Search number or customer…" className="w-64" />
        <select
          value={status ?? ''}
          onChange={(e) => {
            setOffset(0);
            void navigate({
              to: '/invoices',
              search: { status: (e.target.value || undefined) as InvoiceStatus | undefined, customer },
            });
          }}
          className="rounded-md border border-input bg-background px-3 py-1.5 text-sm"
        >
          <option value="">All statuses</option>
          {INVOICE_STATUSES.map((s) => (
            <option key={s.value} value={s.value}>{s.label}</option>
          ))}
        </select>
        {customer && (
          <button type="button" className="rounded-pill border border-border px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
            onClick={() => void navigate({ to: '/invoices', search: { status, customer: undefined } })}>
            Clear customer filter
          </button>
        )}
      </div>

      {invoices.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : invoices.error ? (
        <p className="text-destructive">Could not load invoices: {(invoices.error as Error).message}</p>
      ) : filtered.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">
          {search || status || customer ? 'No invoices match.' : 'No invoices yet — write the first one.'}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2.5 font-medium">Number</th>
                <th className="px-2 py-2.5 font-medium">Customer</th>
                <th className="px-2 py-2.5 font-medium">Status</th>
                <th className="px-2 py-2.5 font-medium">Issued</th>
                <th className="px-2 py-2.5 font-medium">Due</th>
                <th className="px-2 py-2.5 text-right font-medium">Total</th>
                <th className="px-4 py-2.5 text-right font-medium">Open</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((inv) => (
                <tr key={inv.id} className="border-b border-border last:border-b-0 hover:bg-muted/50">
                  <td className="px-4 py-2.5 font-mono text-xs">
                    <Link to="/invoices/$id" params={{ id: inv.id }} className="text-primary hover:underline">
                      {inv.number || '(draft)'}
                    </Link>
                  </td>
                  <td className="px-2 py-2.5 font-medium">{inv.customer_name}</td>
                  <td className="px-2 py-2.5"><InvoiceStatusBadge status={inv.status} overdue={inv.overdue} /></td>
                  <td className="px-2 py-2.5 text-muted-foreground">{inv.issue_date || '—'}</td>
                  <td className={`px-2 py-2.5 ${inv.overdue ? 'font-medium text-destructive' : 'text-muted-foreground'}`}>
                    {inv.due_date || '—'}
                  </td>
                  <td className="px-2 py-2.5 text-right font-medium tabular-nums">{formatCents(inv.total_cents, inv.currency)}</td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {inv.status === 'issued' || inv.status === 'partial' ? formatCents(inv.due_cents, inv.currency) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {invoices.data && <Pagination total={invoices.data.total} offset={offset} onChange={setOffset} />}
    </div>
  );
}
