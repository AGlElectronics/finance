// Money helpers. The API speaks integer cents in the installation's one
// currency (FINANCE_CURRENCY, EUR by default).

const formatters = new Map<string, Intl.NumberFormat>();

/** 123456 -> "1.234,56 €" (in the given ISO currency, EUR by default). */
export function formatCents(cents: number, currency = 'EUR'): string {
  let f = formatters.get(currency);
  if (!f) {
    f = new Intl.NumberFormat('de-DE', { style: 'currency', currency });
    formatters.set(currency, f);
  }
  return f.format(cents / 100);
}

/** "12,50" / "12.50" -> 1250; null when not a number. */
export function parseEuros(input: string): number | null {
  const s = input.trim().replace(/\s/g, '').replace(',', '.');
  if (s === '' || !/^-?\d*(\.\d{0,2})?$/.test(s)) return null;
  const n = Number(s);
  if (!Number.isFinite(n)) return null;
  return Math.round(n * 100);
}

/** 1250 -> "12.50" for editing in an input. */
export function centsToInput(cents: number): string {
  return (cents / 100).toFixed(2);
}

/** 2100 -> "21%" */
export function formatTaxBp(bp: number): string {
  return `${(bp / 100).toLocaleString('de-DE', { maximumFractionDigits: 2 })}%`;
}
