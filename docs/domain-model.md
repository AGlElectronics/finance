# Finance domain model

Proof of concept: outgoing invoices and a simple money ledger. No bank
connection yet — payments are typed in by hand, and the `method` +
`reference` fields on a payment are the hook a later bank feed would match
transactions against.

## Money

- All amounts are **integer cents** in a single currency, **EUR**.
- Line quantities are stored in **hundredths** (1.5 hours → `150`).
- Tax rates are **basis points** (21% VAT → `2100`).
- Line subtotal = quantity × unit price, rounded half away from zero to a
  whole cent; tax is applied per line. Invoice totals are the line sums and
  are stored on the invoice whenever a draft is saved and when it is issued.

## Entities

| Table | What it is |
| --- | --- |
| `customers` | Who an invoice is addressed to (name, email, billing address, tax id). Archived instead of deleted once invoices exist. |
| `invoices` | Stored status is only `draft` / `issued` / `void`. `number` is NULL until issued. |
| `invoice_lines` | Description, quantity (hundredths), unit price (cents), tax rate (bp). Rewritten wholesale while the invoice is a draft; locked after issue. |
| `payments` | Date, amount (cents), method (`bank_transfer` / `cash` / `card` / `other`), reference, note. Only against issued invoices; an amount can never exceed what is still owed. |
| `invoice_counter` | One row. Bumped inside the issue transaction so numbers (`FIN-0001`) are sequential with no gaps. |
| `audit_events` | Append-only log of every state-changing write (same shape as HR's). |
| `den_link` | One row when paired with Den (see `internal/denlink`). |

## Invoice lifecycle

```
draft --issue--> issued --payment(s)--> partial --> paid
                   |                       |
                   +--------- void --------+
```

- **Draft**: editable (customer, dates, notes, lines). Can be deleted.
- **Issue**: takes the next sequential number, stamps `issue_date`
  (default today) and `due_date` (default +30 days), locks the lines.
- **Issued / partial / paid**: the API derives `partial` and `paid` from the
  recorded payments at read time; they are never stored.
- **Void**: allowed from issued (including partially paid). The number stays
  taken. Drafts are deleted, not voided.
- **Overdue** is a flag, not a status: an issued/partial invoice past its due
  date.

## API

All routes require the `finance-admin` Keycloak group except `/livez`,
`/readyz`, `GET /api/role-catalog` and `GET /api/brand`.

- `GET|POST /api/customers`, `GET|PATCH|DELETE /api/customers/{id}`
- `GET|POST /api/invoices`, `GET|PATCH|DELETE /api/invoices/{id}`
- `POST /api/invoices/{id}/issue`, `POST /api/invoices/{id}/void`,
  `POST /api/invoices/{id}/payments`
- `GET /api/dashboard` — outstanding, overdue, cash received (month / all
  time), draft and open counts, recent invoices
- `GET|POST|DELETE /api/den` — Den pairing, same contract as HR
