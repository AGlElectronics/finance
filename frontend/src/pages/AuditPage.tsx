import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { listAudit, type AuditEvent } from '@/api/settings';
import { PAGE_SIZE, Pagination } from '@/components/Pagination';
import { SearchInput } from '@/components/SearchInput';

const ENTITY_TYPES = ['', 'invoice', 'credit_note', 'expense', 'customer', 'approval', 'period', 'settings', 'den_link'];

function Json({ label, value }: { label: string; value: unknown }) {
  if (value === null || value === undefined) return null;
  return (
    <div>
      <p className="text-xs font-semibold text-muted-foreground">{label}</p>
      <pre className="max-h-48 overflow-auto rounded-md bg-muted p-2 text-xs">{JSON.stringify(value, null, 2)}</pre>
    </div>
  );
}

function EventRow({ e }: { e: AuditEvent }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <tr className="cursor-pointer border-b border-border hover:bg-muted/50" onClick={() => setOpen((o) => !o)}>
        <td className="px-4 py-2.5 font-mono text-xs">{new Date(e.occurred_at).toLocaleString()}</td>
        <td className="px-2 py-2.5 font-medium">{e.action}</td>
        <td className="px-2 py-2.5">{e.actor.name}</td>
        <td className="px-2 py-2.5 font-mono text-xs text-muted-foreground">{e.entity_type} {e.entity_id.slice(0, 8)}</td>
      </tr>
      {open && (
        <tr className="border-b border-border">
          <td colSpan={4} className="space-y-2 px-4 py-3">
            <p className="text-xs text-muted-foreground">
              Actor: {e.actor.name} · issuer {e.actor.issuer || '—'} · subject {e.actor.subject || '—'} · entity {e.entity_id}
            </p>
            <div className="grid gap-2 md:grid-cols-3">
              <Json label="Before" value={e.before} />
              <Json label="After" value={e.after} />
              <Json label="Details" value={e.metadata} />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

export function AuditPage() {
  const [entityType, setEntityType] = useState('');
  const [action, setAction] = useState('');
  const [actor, setActor] = useState('');
  const [offset, setOffset] = useState(0);

  const audit = useQuery({
    queryKey: ['audit', entityType, action, actor, offset],
    queryFn: () => listAudit({ entity_type: entityType, action, actor, limit: PAGE_SIZE, offset }),
  });
  const reset = <T,>(set: (v: T) => void) => (v: T) => {
    set(v);
    setOffset(0);
  };

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Audit log</h1>
        <p className="text-sm text-muted-foreground">
          Every change in Finance, who made it and the values before and after. Entries are never changed or removed.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <select className="rounded-md border border-input bg-background px-3 py-2 text-sm" value={entityType}
          onChange={(e) => reset(setEntityType)(e.target.value)}>
          {ENTITY_TYPES.map((t) => <option key={t} value={t}>{t || 'Everything'}</option>)}
        </select>
        <SearchInput value={action} onChange={reset(setAction)} placeholder="Action, e.g. invoice or payment.recorded" className="w-72" />
        <SearchInput value={actor} onChange={reset(setActor)} placeholder="Who" className="w-48" />
      </div>
      {audit.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : audit.error ? (
        <p className="text-destructive">Could not load the audit log: {(audit.error as Error).message}</p>
      ) : audit.data.events.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">No events match.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2.5 font-medium">When</th>
                <th className="px-2 py-2.5 font-medium">Action</th>
                <th className="px-2 py-2.5 font-medium">Who</th>
                <th className="px-2 py-2.5 font-medium">What</th>
              </tr>
            </thead>
            <tbody>
              {audit.data.events.map((e) => <EventRow key={e.id} e={e} />)}
            </tbody>
          </table>
        </div>
      )}
      {audit.data && <Pagination total={audit.data.total} offset={offset} onChange={setOffset} />}
    </div>
  );
}
