export const PAGE_SIZE = 50;

/** "1–50 of 120" plus previous/next buttons for limit/offset lists. */
export function Pagination({
  total,
  offset,
  limit = PAGE_SIZE,
  onChange,
}: {
  total: number;
  offset: number;
  limit?: number;
  onChange: (offset: number) => void;
}) {
  if (total <= limit && offset === 0) return null;
  const first = total === 0 ? 0 : offset + 1;
  const last = Math.min(offset + limit, total);
  const btn = 'rounded-pill border border-border px-3 py-1.5 text-sm disabled:opacity-40';
  return (
    <nav aria-label="Pages" className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
      <span>
        {first}–{last} of {total}
      </span>
      <span className="flex gap-2">
        <button type="button" className={btn} disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>
          Previous
        </button>
        <button type="button" className={btn} disabled={last >= total} onClick={() => onChange(offset + limit)}>
          Next
        </button>
      </span>
    </nav>
  );
}
