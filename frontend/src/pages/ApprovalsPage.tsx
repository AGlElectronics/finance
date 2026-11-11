import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check, X } from 'lucide-react';

import {
  APPROVAL_ACTION_LABELS,
  approve,
  describeApproval,
  listApprovals,
  reject,
  samePerson,
  type Approval,
} from '@/api/approvals';
import { can, useMe } from '@/api/me';
import { useSettings } from '@/api/settings';
import { PAGE_SIZE, Pagination } from '@/components/Pagination';
import { formatCents } from '@/lib/money';

type Filter = 'pending' | 'approved' | 'rejected' | '';

function invoiceLink(a: Approval): string | null {
  if (a.action === 'expense.create' || a.action === 'refund.record' || !a.target_id) return null;
  return a.target_id;
}

export function ApprovalsPage() {
  const qc = useQueryClient();
  const me = useMe().data;
  const currency = useSettings().data?.currency ?? 'EUR';
  const canApprove = can(me, 'finance.approve');
  const [status, setStatus] = useState<Filter>('pending');
  const [offset, setOffset] = useState(0);

  const approvals = useQuery({
    queryKey: ['approvals', status, offset],
    queryFn: () => listApprovals({ status, limit: PAGE_SIZE, offset }),
  });
  const refresh = () => {
    for (const k of ['approvals', 'invoice', 'invoices', 'expenses', 'ledger', 'dashboard']) void qc.invalidateQueries({ queryKey: [k] });
  };
  const decide = useMutation({
    mutationFn: ({ a, ok, note }: { a: Approval; ok: boolean; note: string }) => (ok ? approve(a.id, note) : reject(a.id, note)),
    onSuccess: refresh,
  });

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Approvals</h1>
        <p className="text-sm text-muted-foreground">
          Voids, payments, expenses, credit notes and refunds above the approval threshold wait here until a second person
          with the approver role approves them. Nobody can approve their own request; the requester may withdraw it.
        </p>
      </div>

      <select className="rounded-md border border-input bg-background px-3 py-2 text-sm" value={status}
        onChange={(e) => {
          setStatus(e.target.value as Filter);
          setOffset(0);
        }}>
        <option value="pending">Waiting</option>
        <option value="approved">Approved</option>
        <option value="rejected">Rejected</option>
        <option value="">All</option>
      </select>

      {decide.error && <p className="text-sm text-destructive">{(decide.error as Error).message}</p>}

      {approvals.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : approvals.error ? (
        <p className="text-destructive">Could not load approvals: {(approvals.error as Error).message}</p>
      ) : approvals.data.approvals.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">
          {status === 'pending' ? 'Nothing is waiting for approval.' : 'No requests here.'}
        </p>
      ) : (
        <ul className="space-y-2">
          {approvals.data.approvals.map((a) => {
            const mine = me ? samePerson(a.requested_by, me) : false;
            const invoiceId = invoiceLink(a);
            return (
              <li key={a.id} className="rounded-lg border border-border bg-card p-4 text-sm">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="space-y-1">
                    <p className="font-semibold">
                      {APPROVAL_ACTION_LABELS[a.action]} · <span className="tabular-nums">{formatCents(a.amount_cents, currency)}</span>
                    </p>
                    <p className="text-muted-foreground">{describeApproval(a)}</p>
                    <p className="text-xs text-muted-foreground">
                      Requested by {a.requested_by.name} on {new Date(a.requested_at).toLocaleString()}
                      {a.decided_by && <> · {a.status} by {a.decided_by.name}{a.decision_note && `: ${a.decision_note}`}</>}
                    </p>
                    {invoiceId && (
                      <Link to="/invoices/$id" params={{ id: invoiceId }} className="text-xs text-primary hover:underline">Open the invoice</Link>
                    )}
                  </div>
                  {a.status === 'pending' && (
                    <div className="flex gap-2">
                      {canApprove && !mine && (
                        <button type="button" disabled={decide.isPending}
                          onClick={() => {
                            const note = prompt('Approve this request? Optional note:');
                            if (note !== null) decide.mutate({ a, ok: true, note });
                          }}
                          className="inline-flex items-center gap-1 rounded-pill bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-60">
                          <Check className="h-4 w-4" /> Approve
                        </button>
                      )}
                      {(canApprove || mine) && (
                        <button type="button" disabled={decide.isPending}
                          onClick={() => {
                            const note = prompt(mine ? 'Withdraw your request? Say why:' : 'Reject this request? Say why:');
                            if (note?.trim()) decide.mutate({ a, ok: false, note });
                          }}
                          className="inline-flex items-center gap-1 rounded-pill border border-destructive/40 px-3 py-1.5 text-sm text-destructive disabled:opacity-60">
                          <X className="h-4 w-4" /> {mine ? 'Withdraw' : 'Reject'}
                        </button>
                      )}
                      {mine && canApprove && <span className="self-center text-xs text-muted-foreground">Your request</span>}
                    </div>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {approvals.data && <Pagination total={approvals.data.total} offset={offset} onChange={setOffset} />}
    </div>
  );
}
