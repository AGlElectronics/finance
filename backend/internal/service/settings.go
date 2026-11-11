package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Seller is who issues invoices.
type Seller struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	VATNumber string `json:"vat_number"`
}

// SellerSources says where each effective seller field comes from:
// "finance" (Finance settings), "den" or "unset".
type SellerSources struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	VATNumber string `json:"vat_number"`
}

// Settings is GET /api/settings.
type Settings struct {
	// Seller is what the next issued invoice prints.
	Seller  Seller        `json:"seller"`
	Sources SellerSources `json:"sources"`
	// Overrides are Finance's own values; empty means "use Den's".
	Overrides Seller `json:"overrides"`
	// Den is what Den sends (Den → Module settings → Finance; empty when
	// standalone or not set in Den).
	Den       Seller `json:"den"`
	DenPaired bool   `json:"den_paired"`

	Currency               string `json:"currency"`
	DefaultVATRateBp       int32  `json:"default_vat_rate_bp"`
	InvoicePrefix          string `json:"invoice_prefix"`
	CreditNotePrefix       string `json:"credit_note_prefix"`
	ApprovalThresholdCents int64  `json:"approval_threshold_cents"`
	MaxBackdateDays        int    `json:"max_backdate_days"`
	StorageConfigured      bool   `json:"storage_configured"`
}

// SettingsInput sets Finance's seller overrides. Nil keeps a value; an
// empty string clears the override so Den's value applies again.
type SettingsInput struct {
	SellerName      *string `json:"seller_name"`
	SellerAddress   *string `json:"seller_address"`
	SellerVATNumber *string `json:"seller_vat_number"`
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// denSeller reads Den → Module settings → Finance (seller name, address,
// VAT number) from the integration bundle that denlink stores on every
// sync; the shared denlink does not parse module_settings itself. paired is
// false when Finance has no synced bundle.
//
// Two shapes are read: the 1.0 contract
// (module_settings.data.finance.seller.{legal_name,address,vat_id}) and
// what Den sends today (module_settings.finance.{seller_name,
// seller_address,vat_number}). The contract wins when both are present.
func denSeller(ctx context.Context, q querier) (Seller, bool, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT COALESCE(bundle->'module_settings', 'null'::jsonb) FROM den_link WHERE id = 1 AND bundle IS NOT NULL`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Seller{}, false, nil
	}
	if err != nil {
		return Seller{}, false, err
	}
	var ms struct {
		Data struct {
			Finance struct {
				Seller struct {
					LegalName string `json:"legal_name"`
					Address   string `json:"address"`
					VATID     string `json:"vat_id"`
				} `json:"seller"`
			} `json:"finance"`
		} `json:"data"`
		Finance struct {
			SellerName    string `json:"seller_name"`
			SellerAddress string `json:"seller_address"`
			VATNumber     string `json:"vat_number"`
		} `json:"finance"`
	}
	if err := json.Unmarshal(raw, &ms); err != nil {
		return Seller{}, true, nil
	}
	first := func(a, b string) string {
		if s := strings.TrimSpace(a); s != "" {
			return s
		}
		return strings.TrimSpace(b)
	}
	c, l := ms.Data.Finance.Seller, ms.Finance
	return Seller{
		Name:      first(c.LegalName, l.SellerName),
		Address:   first(c.Address, l.SellerAddress),
		VATNumber: first(c.VATID, l.VATNumber),
	}, true, nil
}

func loadOverrides(ctx context.Context, q querier) (Seller, error) {
	var o Seller
	err := q.QueryRow(ctx, `SELECT seller_name, seller_address, seller_vat_number FROM settings WHERE id = 1`).
		Scan(&o.Name, &o.Address, &o.VATNumber)
	return o, err
}

func pick(override, den string) (string, string) {
	switch {
	case override != "":
		return override, "finance"
	case den != "":
		return den, "den"
	default:
		return "", "unset"
	}
}

func resolve(over, den Seller) (Seller, SellerSources) {
	var eff Seller
	var src SellerSources
	eff.Name, src.Name = pick(over.Name, den.Name)
	eff.Address, src.Address = pick(over.Address, den.Address)
	eff.VATNumber, src.VATNumber = pick(over.VATNumber, den.VATNumber)
	return eff, src
}

// GetSettings returns the effective settings.
func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	over, err := loadOverrides(ctx, s.pool)
	if err != nil {
		return Settings{}, httperror.Internal(err)
	}
	den, paired, err := denSeller(ctx, s.pool)
	if err != nil {
		return Settings{}, httperror.Internal(err)
	}
	eff, src := resolve(over, den)
	_, unconfigured := s.opts.Store.(docstore.Unconfigured)
	return Settings{
		Seller: eff, Sources: src, Overrides: over, Den: den, DenPaired: paired,
		Currency:               s.opts.Currency,
		DefaultVATRateBp:       s.opts.DefaultVATRateBp,
		InvoicePrefix:          s.opts.InvoicePrefix,
		CreditNotePrefix:       s.opts.CreditNotePrefix,
		ApprovalThresholdCents: s.opts.ApprovalThresholdCents,
		MaxBackdateDays:        s.opts.MaxBackdateDays,
		StorageConfigured:      !unconfigured,
	}, nil
}

// UpdateSettings changes Finance's seller overrides.
func (s *Service) UpdateSettings(ctx context.Context, who audit.Actor, in SettingsInput) (Settings, error) {
	fields := map[string]string{}
	for key, v := range map[string]*string{"seller_name": in.SellerName, "seller_address": in.SellerAddress, "seller_vat_number": in.SellerVATNumber} {
		if v != nil && len(*v) > 500 {
			fields[key] = "at most 500 characters"
		}
	}
	if len(fields) > 0 {
		return Settings{}, httperror.Validation("settings are not valid", fields)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		before, err := loadOverrides(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE settings SET
				seller_name       = COALESCE(btrim($1), seller_name),
				seller_address    = COALESCE(btrim($2), seller_address),
				seller_vat_number = COALESCE(btrim($3), seller_vat_number),
				updated_at = now(), updated_by = $4
			 WHERE id = 1`, in.SellerName, in.SellerAddress, in.SellerVATNumber, who.Name); err != nil {
			return err
		}
		after, err := loadOverrides(ctx, tx)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "settings.updated", EntityType: "settings", EntityID: uuid.Nil, Before: before, After: after,
		})
	})
	if err != nil {
		return Settings{}, err
	}
	return s.GetSettings(ctx)
}

// sellerForIssue returns the seller details to freeze on a document, or a
// validation error when the name or address is missing.
func (s *Service) sellerForIssue(ctx context.Context, tx pgx.Tx) (Seller, error) {
	over, err := loadOverrides(ctx, tx)
	if err != nil {
		return Seller{}, err
	}
	den, _, err := denSeller(ctx, tx)
	if err != nil {
		return Seller{}, err
	}
	eff, _ := resolve(over, den)
	fields := map[string]string{}
	if eff.Name == "" {
		fields["seller_name"] = "set it in Finance settings or in Den"
	}
	if eff.Address == "" {
		fields["seller_address"] = "set it in Finance settings or in Den"
	}
	if len(fields) > 0 {
		return Seller{}, httperror.Validation("the seller name and address are needed before issuing", fields)
	}
	return eff, nil
}
