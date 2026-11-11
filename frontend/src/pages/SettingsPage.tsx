import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useCan } from '@/api/me';
import {
  closePeriod,
  listPeriods,
  reopenPeriod,
  updateSettings,
  useSettings,
  type Settings,
  type Source,
} from '@/api/settings';
import { DenConnection } from '@/components/DenConnection';
import { formatCents, formatTaxBp } from '@/lib/money';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';

const SOURCE_LABEL: Record<Source, string> = {
  finance: 'set in Finance',
  den: 'from Den',
  unset: 'not set',
};

function SellerForm({ settings }: { settings: Settings }) {
  const qc = useQueryClient();
  const [name, setName] = useState(settings.overrides.name);
  const [address, setAddress] = useState(settings.overrides.address);
  const [vat, setVat] = useState(settings.overrides.vat_number);
  const save = useMutation({
    mutationFn: () => updateSettings({ seller_name: name, seller_address: address, seller_vat_number: vat }),
    onSuccess: (s) => qc.setQueryData(['settings'], s),
  });
  const denHint = (v: string) => (v ? `Den: ${v}` : settings.den_paired ? 'Den sets nothing here' : 'Not connected to Den');
  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">Override the seller details</h2>
      <p className="text-xs text-muted-foreground">Leave a field empty to use the value from Den.</p>
      <label className="block text-sm">
        <span className="mb-1 block font-medium">Seller name</span>
        <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder={denHint(settings.den.name)} />
      </label>
      <label className="block text-sm">
        <span className="mb-1 block font-medium">Address</span>
        <textarea className={`${inputCls} min-h-20`} value={address} onChange={(e) => setAddress(e.target.value)}
          placeholder={denHint(settings.den.address)} />
      </label>
      <label className="block text-sm">
        <span className="mb-1 block font-medium">VAT number</span>
        <input className={inputCls} value={vat} onChange={(e) => setVat(e.target.value)} placeholder={denHint(settings.den.vat_number)} />
      </label>
      {save.error && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
      {save.isSuccess && <p className="text-sm text-success">Saved. The next issued invoice uses these details.</p>}
      <button type="submit" disabled={save.isPending}
        className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
        {save.isPending ? 'Saving…' : 'Save'}
      </button>
    </form>
  );
}

function lastMonth(): string {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - 1);
  return d.toISOString().slice(0, 7);
}

function Periods() {
  const qc = useQueryClient();
  const canClose = useCan('finance.approve');
  const canReopen = useCan('finance.admin');
  const [month, setMonth] = useState(lastMonth());
  const periods = useQuery({ queryKey: ['periods'], queryFn: listPeriods });
  const refresh = () => void qc.invalidateQueries({ queryKey: ['periods'] });
  const close = useMutation({ mutationFn: () => closePeriod(month), onSuccess: refresh });
  const reopen = useMutation({ mutationFn: ({ m, reason }: { m: string; reason: string }) => reopenPeriod(m, reason), onSuccess: refresh });
  const error = (close.error ?? reopen.error) as Error | null;

  return (
    <section className="space-y-3 rounded-lg border border-border bg-card p-4">
      <h2 className="font-semibold">Closed months</h2>
      <p className="text-xs text-muted-foreground">
        Once a month is closed, nothing dated in it can be added, changed, voided or deleted. Approvers close months; only an
        admin can reopen one, and the reason goes into the audit log.
      </p>
      {canClose && (
        <form className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (confirm(`Close ${month}? Nothing dated in ${month} can change afterwards.`)) close.mutate();
          }}>
          <label className="text-sm">
            <span className="mb-1 block font-medium">Month</span>
            <input type="month" className={inputCls} value={month} onChange={(e) => setMonth(e.target.value)} />
          </label>
          <button type="submit" disabled={close.isPending || !month}
            className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
            Close month
          </button>
        </form>
      )}
      {error && <p className="text-sm text-destructive">{error.message}</p>}
      {periods.data && periods.data.length === 0 && <p className="text-sm text-muted-foreground">No month is closed yet.</p>}
      <ul className="divide-y divide-border text-sm">
        {(periods.data ?? []).map((p) => (
          <li key={p.month} className="flex items-center justify-between py-2">
            <span>
              <span className="font-mono">{p.month}</span>
              <span className="text-muted-foreground"> · closed by {p.closed_by.name} on {new Date(p.closed_at).toLocaleDateString()}</span>
            </span>
            {canReopen && (
              <button type="button" className="rounded-pill border border-border px-3 py-1 text-xs"
                onClick={() => {
                  const reason = prompt(`Why reopen ${p.month}?`);
                  if (reason?.trim()) reopen.mutate({ m: p.month, reason });
                }}>
                Reopen
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

export function SettingsPage() {
  const settings = useSettings();
  const isAdmin = useCan('finance.admin');
  const s = settings.data;

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Finance settings</h1>
        <p className="text-sm text-muted-foreground">
          Who issues the invoices, which months are closed, and how this installation is set up.
        </p>
      </div>

      {settings.error && <p className="text-destructive">Could not load the settings: {(settings.error as Error).message}</p>}
      {s && (
        <>
          <section className="space-y-2 rounded-lg border border-border bg-card p-4 text-sm">
            <h2 className="font-semibold">Seller on new invoices</h2>
            {(['name', 'address', 'vat_number'] as const).map((k) => (
              <div key={k} className="flex gap-3">
                <span className="w-28 shrink-0 text-muted-foreground">{k === 'vat_number' ? 'VAT number' : k === 'name' ? 'Name' : 'Address'}</span>
                <span className="whitespace-pre-wrap">{s.seller[k] || '—'}</span>
                <span className="ml-auto shrink-0 text-xs text-muted-foreground">{SOURCE_LABEL[s.sources[k]]}</span>
              </div>
            ))}
            {(!s.seller.name || !s.seller.address) && (
              <p className="text-destructive">Invoices cannot be issued until the seller name and address are set (here or in Den).</p>
            )}
            {!s.storage_configured && (
              <p className="text-destructive">PDF storage (S3) is not configured, so invoices cannot be issued. Ask your operator to set S3_*.</p>
            )}
          </section>
          {isAdmin && <SellerForm key={JSON.stringify(s.overrides)} settings={s} />}

          <section className="space-y-1 rounded-lg border border-border bg-card p-4 text-sm">
            <h2 className="mb-1 font-semibold">This installation</h2>
            <p>Currency: {s.currency} · default VAT {formatTaxBp(s.default_vat_rate_bp)}</p>
            <p>Numbers: {s.invoice_prefix}0001 for invoices, {s.credit_note_prefix}0001 for credit notes</p>
            <p>
              Approval needed above {formatCents(s.approval_threshold_cents, s.currency)}
              {s.approval_threshold_cents === 0 && ' (every void, payment, expense, credit note and refund)'}
            </p>
            <p>Dates may be up to {s.max_backdate_days} days in the past.</p>
            <p className="text-xs text-muted-foreground">Set by the operator (FINANCE_* settings). Pending review by an accountant.</p>
          </section>
        </>
      )}

      <Periods />

      {isAdmin && <DenConnection base="/api/den" />}
    </div>
  );
}
