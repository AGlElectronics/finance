import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { Ban, Download, FileMinus, Pencil, Send, Trash2 } from 'lucide-react';

import { APPROVAL_ACTION_LABELS, isPending } from '@/api/approvals';
import { ApiError, newIdempotencyKey } from '@/api/client';
import {
  addPayment,
  createCreditNote,
  createRefund,
  deleteInvoice,
  downloadCreditNotePdf,
  downloadInvoicePdf,
  getInvoice,
  issueInvoice,
  voidInvoice,
  PAYMENT_METHODS,
  type CreditNoteSummary,
  type Invoice,
  type PaymentMethod,
} from '@/api/invoices';
import { useCan } from '@/api/me';
import { useSettings } from '@/api/settings';
import { InvoiceStatusBadge } from '@/components/InvoiceStatusBadge';
import { centsToInput, formatCents, formatTaxBp, parseEuros } from '@/lib/money';
import { queryClient } from '@/lib/queryClient';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';
const today = () => new Date().toISOString().slice(0, 10);

function invalidate(id: string) {
  for (const key of ['invoice', 'invoices', 'dashboard', 'customers', 'ledger', 'approvals']) {
    void queryClient.invalidateQueries({ queryKey: key === 'invoice' ? [key, id] : [key] });
  }
}

const PENDING_NOTE = 'Sent for approval: a second person with the approver role must approve it on the Approvals page.';

function fieldErrors(err: unknown): Record<string, string> {
  return err instanceof ApiError ? ((err.details as Record<string, string> | undefined) ?? {}) : {};
}

function FormError({ error }: { error: unknown }) {
  if (!error) return null;
  const errs = fieldErrors(error);
  const text = Object.keys(errs).length ? Object.entries(errs).map(([k, v]) => `${k}: ${v}`).join(', ') : '';
  return <p className="text-sm text-destructive">{(error as Error).message}{text && ` (${text})`}</p>;
}

/** Amount, date, method, reference and note: shared by payments and refunds. */
function MoneyForm({
  title,
  hint,
  max,
  dateLabel,
  submitLabel,
  currency,
  submit,
  onDone,
}: {
  title: string;
  hint: string;
  max: number;
  dateLabel: string;
  submitLabel: string;
  currency: string;
  submit: (v: { on: string; amount: number; method: PaymentMethod; reference: string; note: string }, key: string) => Promise<unknown>;
  onDone: (pending: boolean) => void;
}) {
  const [amount, setAmount] = useState(centsToInput(max));
  const [on, setOn] = useState(today());
  const [method, setMethod] = useState<PaymentMethod>('bank_transfer');
  const [reference, setReference] = useState('');
  const [note, setNote] = useState('');
  // One key per attempt: a retry after a network error cannot record the money twice.
  const [key] = useState(newIdempotencyKey);
  const cents = parseEuros(amount);
  const save = useMutation({
    mutationFn: () => submit({ on, amount: cents ?? 0, method, reference, note }, key),
    onSuccess: (res) => onDone(isPending(res)),
  });

  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4 print:hidden"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">{title}</h2>
      <p className="text-xs text-muted-foreground">{hint}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Amount ({currency})</span>
          <input className={inputCls} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">{dateLabel}</span>
          <input type="date" className={inputCls} value={on} max={today()} onChange={(e) => setOn(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Method</span>
          <select className={inputCls} value={method} onChange={(e) => setMethod(e.target.value as PaymentMethod)}>
            {PAYMENT_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Reference</span>
          <input className={inputCls} value={reference} onChange={(e) => setReference(e.target.value)}
            placeholder="e.g. bank statement reference" />
        </label>
        <label className="block text-sm sm:col-span-2">
          <span className="mb-1 block font-medium">Note</span>
          <input className={inputCls} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
      </div>
      <FormError error={save.error} />
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !cents || cents <= 0 || cents > max}
          className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : submitLabel}
        </button>
        <button type="button" onClick={() => onDone(false)} className="rounded-pill border border-border px-4 py-2 text-sm">Cancel</button>
      </div>
    </form>
  );
}

function ReasonForm({ title, hint, submitLabel, submit, onDone }: {
  title: string;
  hint: string;
  submitLabel: string;
  submit: (reason: string) => Promise<unknown>;
  onDone: (pending: boolean) => void;
}) {
  const [reason, setReason] = useState('');
  const save = useMutation({ mutationFn: () => submit(reason), onSuccess: (res) => onDone(isPending(res)) });
  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4 print:hidden"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">{title}</h2>
      <p className="text-xs text-muted-foreground">{hint}</p>
      <label className="block text-sm">
        <span className="mb-1 block font-medium">Reason</span>
        <input className={inputCls} value={reason} onChange={(e) => setReason(e.target.value)} required />
      </label>
      <FormError error={save.error} />
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !reason.trim()}
          className="rounded-pill bg-destructive px-4 py-2 text-sm font-semibold text-destructive-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : submitLabel}
        </button>
        <button type="button" onClick={() => onDone(false)} className="rounded-pill border border-border px-4 py-2 text-sm">Cancel</button>
      </div>
    </form>
  );
}

function CreditNoteForm({ invoice, defaultVatBp, onDone }: { invoice: Invoice; defaultVatBp: number; onDone: (pending: boolean) => void }) {
  const full = invoice.credited_cents === 0;
  const [mode, setMode] = useState<'full' | 'partial'>(full ? 'full' : 'partial');
  const [reason, setReason] = useState('');
  const [description, setDescription] = useState('');
  const [net, setNet] = useState('');
  const [taxPercent, setTaxPercent] = useState(String(defaultVatBp / 100));
  const [key] = useState(newIdempotencyKey);
  const netCents = parseEuros(net);
  const taxBp = parseEuros(taxPercent);
  const save = useMutation({
    mutationFn: () =>
      createCreditNote(invoice.id, mode === 'full'
        ? { reason, full: true }
        : { reason, lines: [{ description, quantity: 1, unit_price_cents: netCents ?? 0, tax_rate_bp: taxBp ?? 0 }] }, key),
    onSuccess: (res) => onDone(isPending(res)),
  });
  const ready = reason.trim() && (mode === 'full' || (description.trim() && netCents && netCents > 0 && taxBp !== null));
  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4 print:hidden"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">Issue a credit note</h2>
      <p className="text-xs text-muted-foreground">
        A credit note corrects this invoice in its own numbered series. It lowers what the customer owes; money already paid
        beyond the new total is paid back with a refund. Left to credit: {formatCents(invoice.total_cents - invoice.credited_cents, invoice.currency)}.
      </p>
      <div className="flex gap-4 text-sm">
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'full'} disabled={!full} onChange={() => setMode('full')} /> The whole invoice
        </label>
        <label className="flex items-center gap-2">
          <input type="radio" checked={mode === 'partial'} onChange={() => setMode('partial')} /> Part of it
        </label>
      </div>
      {mode === 'partial' && (
        <div className="grid gap-3 sm:grid-cols-[1fr_8rem_6rem]">
          <input className={inputCls} placeholder="What is credited" value={description} onChange={(e) => setDescription(e.target.value)} />
          <input className={inputCls} inputMode="decimal" placeholder="Net amount" value={net} onChange={(e) => setNet(e.target.value)} />
          <input className={inputCls} inputMode="decimal" placeholder="Tax %" value={taxPercent} onChange={(e) => setTaxPercent(e.target.value)} />
        </div>
      )}
      <label className="block text-sm">
        <span className="mb-1 block font-medium">Reason (printed on the credit note)</span>
        <input className={inputCls} value={reason} onChange={(e) => setReason(e.target.value)} />
      </label>
      <FormError error={save.error} />
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !ready}
          className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : 'Issue credit note'}
        </button>
        <button type="button" onClick={() => onDone(false)} className="rounded-pill border border-border px-4 py-2 text-sm">Cancel</button>
      </div>
    </form>
  );
}

function PartyBlock({ title, name, lines }: { title: string; name: string; lines: string[] }) {
  return (
    <div className="text-sm">
      <p className="text-xs font-semibold uppercase text-muted-foreground">{title}</p>
      <p className="font-semibold">{name || '—'}</p>
      {lines.filter(Boolean).map((l) => <p key={l} className="whitespace-pre-wrap text-muted-foreground">{l}</p>)}
    </div>
  );
}

type Panel = 'pay' | 'void' | 'credit' | { refund: CreditNoteSummary } | null;

export function InvoiceDetailPage() {
  const { id } = useParams({ from: '/app/invoices/$id' });
  const navigate = useNavigate();
  const canWrite = useCan('finance.write');
  const settings = useSettings();
  const [panel, setPanel] = useState<Panel>(null);
  const [notice, setNotice] = useState('');

  const invoice = useQuery({ queryKey: ['invoice', id], queryFn: () => getInvoice(id) });

  const issue = useMutation({ mutationFn: () => issueInvoice(id), onSuccess: () => invalidate(id) });
  const remove = useMutation({
    mutationFn: () => deleteInvoice(id),
    onSuccess: () => {
      invalidate(id);
      void navigate({ to: '/invoices', search: { status: undefined, customer: undefined } });
    },
  });
  const download = useMutation({ mutationFn: (fn: () => Promise<void>) => fn() });

  if (invoice.isPending) {
    return <div className="p-6 text-muted-foreground">Loading…</div>;
  }
  if (invoice.error) {
    return <p className="p-6 text-destructive">Could not load the invoice: {(invoice.error as Error).message}</p>;
  }
  const inv = invoice.data;
  const cur = inv.currency;
  const open = inv.status === 'issued' || inv.status === 'partial';
  const correctable = inv.status !== 'draft' && inv.status !== 'void' && inv.total_cents - inv.credited_cents > 0;
  const done = (pending: boolean) => {
    setPanel(null);
    setNotice(pending ? PENDING_NOTE : '');
    invalidate(id);
  };
  const actionError = (issue.error ?? remove.error ?? download.error) as Error | null;
  const btn = 'inline-flex items-center gap-1.5 rounded-pill border border-border px-3 py-1.5 text-sm disabled:opacity-60';

  return (
    <div className="p-6 max-w-3xl space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold">{inv.number || 'Draft invoice'}</h1>
            <InvoiceStatusBadge status={inv.status} overdue={inv.overdue} />
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {inv.customer_name}
            {inv.issue_date && <> · issued {inv.issue_date}</>}
            {inv.due_date && <> · due {inv.due_date}</>}
          </p>
        </div>

        <div className="flex flex-wrap gap-2 print:hidden">
          {canWrite && inv.status === 'draft' && (
            <>
              <Link to="/invoices/$id/edit" params={{ id }} className={btn}>
                <Pencil className="h-4 w-4" /> Edit
              </Link>
              <button type="button" disabled={issue.isPending}
                onClick={() => {
                  if (confirm('Issue this invoice? It gets the next number, the customer and seller details are frozen and the PDF is stored.')) issue.mutate();
                }}
                className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-60">
                <Send className="h-4 w-4" /> {issue.isPending ? 'Issuing…' : 'Issue'}
              </button>
              <button type="button" disabled={remove.isPending}
                onClick={() => {
                  if (confirm('Delete this draft? This cannot be undone.')) remove.mutate();
                }}
                className="inline-flex items-center gap-1.5 rounded-pill border border-destructive/40 px-3 py-1.5 text-sm text-destructive disabled:opacity-60">
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            </>
          )}
          {canWrite && open && (
            <button type="button" onClick={() => setPanel('pay')}
              className="rounded-pill bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground">
              Record payment
            </button>
          )}
          {canWrite && correctable && (
            <button type="button" onClick={() => setPanel('credit')} className={btn}>
              <FileMinus className="h-4 w-4" /> Credit note
            </button>
          )}
          {canWrite && inv.status === 'issued' && inv.credited_cents === 0 && (
            <button type="button" onClick={() => setPanel('void')}
              className="inline-flex items-center gap-1.5 rounded-pill border border-destructive/40 px-3 py-1.5 text-sm text-destructive">
              <Ban className="h-4 w-4" /> Void
            </button>
          )}
          {inv.has_pdf && (
            <button type="button" disabled={download.isPending} onClick={() => download.mutate(() => downloadInvoicePdf(inv))} className={btn}>
              <Download className="h-4 w-4" /> PDF
            </button>
          )}
        </div>
      </div>

      {actionError && <p className="text-sm text-destructive print:hidden">{actionError.message}</p>}
      {notice && <p className="rounded-md bg-info-soft px-3 py-2 text-sm text-info-foreground">{notice}</p>}

      {panel === 'pay' && (
        <MoneyForm title="Record a payment" currency={cur} max={inv.due_cents} dateLabel="Received on" submitLabel="Record payment"
          hint={`Still owed: ${formatCents(inv.due_cents, cur)}. Method and reference are what a later bank feed matches on.`}
          submit={(v, key) => addPayment(id, { paid_on: v.on, amount_cents: v.amount, method: v.method, reference: v.reference, note: v.note }, key)}
          onDone={done} />
      )}
      {panel === 'void' && (
        <ReasonForm title="Void this invoice" submitLabel="Void"
          hint="Only for invoices with no payments: the number stays taken and the invoice stops counting as owed. Paid invoices are corrected with a credit note."
          submit={(reason) => voidInvoice(id, reason)} onDone={done} />
      )}
      {panel === 'credit' && <CreditNoteForm invoice={inv} defaultVatBp={settings.data?.default_vat_rate_bp ?? 2000} onDone={done} />}
      {panel && typeof panel === 'object' && (
        <MoneyForm title={`Refund against ${panel.refund.number}`} currency={cur}
          max={Math.min(inv.refundable_cents, panel.refund.total_cents - panel.refund.refunded_cents)}
          dateLabel="Paid back on" submitLabel="Record refund"
          hint={`The customer paid ${formatCents(inv.refundable_cents, cur)} more than they owe after credit notes.`}
          submit={(v, key) => createRefund(panel.refund.id, { refunded_on: v.on, amount_cents: v.amount, method: v.method, reference: v.reference, note: v.note }, key)}
          onDone={done} />
      )}

      {inv.pending_approvals.length > 0 && (
        <section className="rounded-lg border border-warning/40 bg-warning-soft p-4 text-sm">
          <h2 className="mb-1 font-semibold">Waiting for approval</h2>
          <ul className="space-y-1">
            {inv.pending_approvals.map((a) => (
              <li key={a.id}>
                {APPROVAL_ACTION_LABELS[a.action]} · {formatCents(a.amount_cents, cur)} · requested by {a.requested_by.name}{' '}
                <Link to="/approvals" className="text-primary hover:underline">open</Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2">
        <PartyBlock title="From" name={inv.seller?.name ?? settings.data?.seller.name ?? ''}
          lines={[inv.seller?.address ?? settings.data?.seller.address ?? '',
            (inv.seller?.vat_number ?? settings.data?.seller.vat_number) ? `VAT number: ${inv.seller?.vat_number ?? settings.data?.seller.vat_number}` : '']} />
        <PartyBlock title="Bill to" name={inv.customer.name}
          lines={[inv.customer.address, inv.customer.tax_id ? `Tax ID: ${inv.customer.tax_id}` : '', inv.customer.email]} />
        {inv.status === 'draft' && (
          <p className="text-xs text-muted-foreground sm:col-span-2">
            These details are frozen when the invoice is issued; later changes to the customer or the seller do not change it.
          </p>
        )}
      </section>

      <section className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="px-4 py-2.5 font-medium">Description</th>
              <th className="px-2 py-2.5 text-right font-medium">Qty</th>
              <th className="px-2 py-2.5 text-right font-medium">Unit price</th>
              <th className="px-2 py-2.5 text-right font-medium">Tax</th>
              <th className="px-4 py-2.5 text-right font-medium">Total</th>
            </tr>
          </thead>
          <tbody>
            {inv.lines.map((l) => (
              <tr key={l.id} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">{l.description}</td>
                <td className="px-2 py-2.5 text-right tabular-nums">{l.quantity}</td>
                <td className="px-2 py-2.5 text-right tabular-nums">{formatCents(l.unit_price_cents, cur)}</td>
                <td className="px-2 py-2.5 text-right tabular-nums text-muted-foreground">{formatTaxBp(l.tax_rate_bp)}</td>
                <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(l.total_cents, cur)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="ml-auto w-72 space-y-1 px-4 py-3 text-sm">
          <div className="flex justify-between"><span className="text-muted-foreground">Subtotal</span><span className="tabular-nums">{formatCents(inv.subtotal_cents, cur)}</span></div>
          <div className="flex justify-between"><span className="text-muted-foreground">Tax</span><span className="tabular-nums">{formatCents(inv.tax_cents, cur)}</span></div>
          <div className="flex justify-between font-semibold"><span>Total</span><span className="tabular-nums">{formatCents(inv.total_cents, cur)}</span></div>
          {inv.credited_cents > 0 && (
            <div className="flex justify-between"><span className="text-muted-foreground">Credited</span><span className="tabular-nums">−{formatCents(inv.credited_cents, cur)}</span></div>
          )}
          {inv.paid_cents > 0 && (
            <div className="flex justify-between text-success"><span>Paid</span><span className="tabular-nums">−{formatCents(inv.paid_cents, cur)}</span></div>
          )}
          {inv.refunded_cents > 0 && (
            <div className="flex justify-between"><span className="text-muted-foreground">Refunded</span><span className="tabular-nums">+{formatCents(inv.refunded_cents, cur)}</span></div>
          )}
          {(inv.paid_cents > 0 || inv.credited_cents > 0) && (
            <div className="flex justify-between font-semibold"><span>Still owed</span><span className="tabular-nums">{formatCents(inv.due_cents, cur)}</span></div>
          )}
          {inv.refundable_cents > 0 && (
            <div className="flex justify-between font-semibold text-warning-foreground"><span>To refund</span><span className="tabular-nums">{formatCents(inv.refundable_cents, cur)}</span></div>
          )}
        </div>
      </section>

      {inv.notes && (
        <section className="rounded-lg border border-border bg-card p-4 text-sm">
          <h2 className="mb-1 font-semibold">Notes</h2>
          <p className="whitespace-pre-wrap text-muted-foreground">{inv.notes}</p>
        </section>
      )}

      {inv.payments.length > 0 && (
        <section className="rounded-lg border border-border bg-card">
          <h2 className="border-b border-border px-4 py-3 font-semibold">Payments</h2>
          <table className="w-full text-sm">
            <tbody>
              {inv.payments.map((p) => (
                <tr key={p.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 text-muted-foreground">{p.paid_on}</td>
                  <td className="px-2 py-2.5">{PAYMENT_METHODS.find((m) => m.value === p.method)?.label ?? p.method}</td>
                  <td className="px-2 py-2.5 font-mono text-xs text-muted-foreground">{p.reference || '—'}</td>
                  <td className="px-2 py-2.5 text-muted-foreground">{p.note}</td>
                  <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(p.amount_cents, cur)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      {inv.credit_notes.length > 0 && (
        <section className="rounded-lg border border-border bg-card">
          <h2 className="border-b border-border px-4 py-3 font-semibold">Credit notes</h2>
          <table className="w-full text-sm">
            <tbody>
              {inv.credit_notes.map((c) => (
                <tr key={c.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 font-mono text-xs">{c.number}</td>
                  <td className="px-2 py-2.5 text-muted-foreground">{c.issue_date}</td>
                  <td className="px-2 py-2.5 text-muted-foreground">{c.reason}</td>
                  <td className="px-2 py-2.5 text-right font-medium tabular-nums">−{formatCents(c.total_cents, cur)}</td>
                  <td className="px-4 py-2.5">
                    <div className="flex justify-end gap-1">
                      <button type="button" title="Download PDF" className={btn} onClick={() => download.mutate(() => downloadCreditNotePdf(c))}>
                        <Download className="h-4 w-4" />
                      </button>
                      {canWrite && inv.refundable_cents > 0 && c.total_cents > c.refunded_cents && (
                        <button type="button" className={btn} onClick={() => setPanel({ refund: c })}>Refund</button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      {inv.refunds.length > 0 && (
        <section className="rounded-lg border border-border bg-card">
          <h2 className="border-b border-border px-4 py-3 font-semibold">Refunds</h2>
          <table className="w-full text-sm">
            <tbody>
              {inv.refunds.map((r) => (
                <tr key={r.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 text-muted-foreground">{r.refunded_on}</td>
                  <td className="px-2 py-2.5">{PAYMENT_METHODS.find((m) => m.value === r.method)?.label ?? r.method}</td>
                  <td className="px-2 py-2.5 font-mono text-xs text-muted-foreground">{r.reference || '—'}</td>
                  <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(r.amount_cents, cur)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      <Link to="/invoices" search={{ status: undefined, customer: undefined }}
        className="inline-block text-sm text-primary hover:underline print:hidden">
        ← All invoices
      </Link>
    </div>
  );
}
