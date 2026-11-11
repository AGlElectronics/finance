import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Archive, ArchiveRestore, Pencil, Plus, Trash2 } from 'lucide-react';

import { ApiError } from '@/api/client';
import {
  createCustomer,
  deleteCustomer,
  listCustomers,
  updateCustomer,
  type Customer,
  type CustomerInput,
} from '@/api/customers';
import { SearchInput } from '@/components/SearchInput';
import { useCan } from '@/api/me';
import { formatCents } from '@/lib/money';
import { noAutofill } from '@/lib/noAutofill';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';

const EMPTY_FORM = { name: '', email: '', billing_address: '', tax_id: '', notes: '' };
type Form = typeof EMPTY_FORM;

function CustomerForm({ initial, onDone, customerId }: { initial: Form; customerId?: string; onDone: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<Form>(initial);
  const set = (k: keyof Form, v: string) => setForm((f) => ({ ...f, [k]: v }));

  const save = useMutation({
    mutationFn: () => {
      const input: CustomerInput = form;
      return customerId ? updateCustomer(customerId, input) : createCustomer(input);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['customers'] });
      onDone();
    },
  });
  const errs = save.error instanceof ApiError ? ((save.error.details as Record<string, string> | undefined) ?? {}) : {};

  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">{customerId ? 'Edit customer' : 'New customer'}</h2>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Name</span>
          <input {...noAutofill} required className={inputCls} value={form.name} onChange={(e) => set('name', e.target.value)} />
          {errs.name && <span className="mt-1 block text-xs text-destructive">{errs.name}</span>}
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Email</span>
          <input {...noAutofill} type="email" className={inputCls} value={form.email} onChange={(e) => set('email', e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Tax ID</span>
          <input {...noAutofill} className={inputCls} value={form.tax_id} onChange={(e) => set('tax_id', e.target.value)} placeholder="e.g. ATU12345678" />
        </label>
        <label className="block text-sm sm:row-span-2">
          <span className="mb-1 block font-medium">Billing address</span>
          <textarea className={`${inputCls} min-h-20`} value={form.billing_address} onChange={(e) => set('billing_address', e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Notes</span>
          <textarea className={`${inputCls} min-h-20`} value={form.notes} onChange={(e) => set('notes', e.target.value)} />
        </label>
      </div>
      {save.error && Object.keys(errs).length === 0 && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !form.name.trim()}
          className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : 'Save'}
        </button>
        <button type="button" onClick={onDone} className="rounded-pill border border-border px-4 py-2 text-sm">
          Cancel
        </button>
      </div>
    </form>
  );
}

export function CustomersPage() {
  const qc = useQueryClient();
  const canWrite = useCan('finance.write');
  const [showArchived, setShowArchived] = useState(false);
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<Customer | 'new' | null>(null);

  const customers = useQuery({
    queryKey: ['customers', showArchived],
    queryFn: () => listCustomers(showArchived),
  });

  const archive = useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) => updateCustomer(id, { archived }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['customers'] }),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteCustomer(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['customers'] }),
  });

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const all = customers.data ?? [];
    if (!q) return all;
    return all.filter((c) => `${c.name} ${c.email} ${c.tax_id}`.toLowerCase().includes(q));
  }, [customers.data, search]);

  return (
    <div className="p-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Customers</h1>
          <p className="text-sm text-muted-foreground">Who your invoices are addressed to.</p>
        </div>
        {canWrite && (
          <button type="button" onClick={() => setEditing('new')}
            className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground">
            <Plus className="h-4 w-4" /> New customer
          </button>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        Issued invoices keep the name, address and tax ID they were issued with; editing a customer changes only new invoices.
      </p>

      {editing && (
        <CustomerForm
          key={editing === 'new' ? 'new' : editing.id}
          customerId={editing === 'new' ? undefined : editing.id}
          initial={editing === 'new' ? EMPTY_FORM : {
            name: editing.name, email: editing.email, billing_address: editing.billing_address,
            tax_id: editing.tax_id, notes: editing.notes,
          }}
          onDone={() => setEditing(null)}
        />
      )}

      <div className="flex flex-wrap items-center gap-3">
        <SearchInput value={search} onChange={setSearch} placeholder="Search customers…" className="w-64" />
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
          Show archived
        </label>
      </div>

      {customers.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : customers.error ? (
        <p className="text-destructive">Could not load customers: {(customers.error as Error).message}</p>
      ) : filtered.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">
          {search ? 'No customers match your search.' : 'No customers yet — create the first one.'}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2.5 font-medium">Name</th>
                <th className="px-2 py-2.5 font-medium">Email</th>
                <th className="px-2 py-2.5 text-right font-medium">Invoices</th>
                <th className="px-2 py-2.5 text-right font-medium">Still owed</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {filtered.map((c) => (
                <tr key={c.id} className="border-b border-border last:border-b-0 hover:bg-muted/50">
                  <td className="px-4 py-2.5">
                    <span className={`font-medium ${c.archived ? 'text-muted-foreground line-through' : ''}`}>{c.name}</span>
                    {c.archived && <span className="ml-2 rounded-pill bg-muted px-2 py-0.5 text-xs text-muted-foreground">Archived</span>}
                  </td>
                  <td className="px-2 py-2.5 text-muted-foreground">{c.email || '—'}</td>
                  <td className="px-2 py-2.5 text-right tabular-nums">
                    {c.invoice_count > 0 ? (
                      <Link to="/invoices" search={{ customer: c.id }} className="text-primary hover:underline">{c.invoice_count}</Link>
                    ) : (
                      '0'
                    )}
                  </td>
                  <td className="px-2 py-2.5 text-right font-medium tabular-nums">
                    {c.outstanding_cents > 0 ? formatCents(c.outstanding_cents) : '—'}
                  </td>
                  <td className="px-4 py-2.5">
                    <div className={`flex justify-end gap-1 ${canWrite ? '' : 'hidden'}`}>
                      <button type="button" title="Edit" onClick={() => setEditing(c)}
                        className="rounded-pill p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button type="button" title={c.archived ? 'Restore' : 'Archive'}
                        onClick={() => archive.mutate({ id: c.id, archived: !c.archived })}
                        className="rounded-pill p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                        {c.archived ? <ArchiveRestore className="h-4 w-4" /> : <Archive className="h-4 w-4" />}
                      </button>
                      {c.invoice_count === 0 && (
                        <button type="button" title="Delete"
                          onClick={() => {
                            if (confirm(`Delete ${c.name}? This cannot be undone.`)) remove.mutate(c.id);
                          }}
                          className="rounded-pill p-1.5 text-muted-foreground hover:bg-muted hover:text-destructive">
                          <Trash2 className="h-4 w-4" />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {(archive.error ?? remove.error) && (
        <p className="text-sm text-destructive">{((archive.error ?? remove.error) as Error).message}</p>
      )}
    </div>
  );
}
