package permission_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

func TestValidateDeclarationAcceptsOrdinaryAndWellFormedFieldCodes(t *testing.T) {
	codes := []string{
		"invoice.read",
		"organization.member.manage",
		"invoice.field.internal_cost.read",
		"invoice.field.internal_cost.write",
		"billing.invoice.field.margin.read",
		"billing.invoice.field.margin.write",
		// `field` bukan di posisi ketiga dari kanan: bukan kode field, tidak dibatasi
		"invoice.field.export",
		"field.x.y.export",
		"report.field.margin.read.export",
	}
	for _, c := range codes {
		require.NoError(t, permission.ValidateDeclaration(c), c)
	}
}

func TestValidateDeclarationRejectsFieldCodesWithAnyOtherAction(t *testing.T) {
	codes := []string{
		"invoice.field.internal_cost.export",
		"invoice.field.internal_cost.delete",
		"invoice.field.internal_cost.manage",
		"invoice.field.internal_cost.reader",
		"billing.invoice.field.margin.approve",
		"report.field.x.export",
		"field.field.x.export",
	}
	for _, c := range codes {
		err := permission.ValidateDeclaration(c)
		require.ErrorIs(t, err, permission.ErrInvalidFieldDeclaration, c)
		require.NotErrorIs(t, err, permission.ErrInvalidCode, c)
	}
}

func TestValidateDeclarationRejectsMalformedCodesWithTheCodeError(t *testing.T) {
	codes := []string{"", "invoice", "Invoice.read", "invoice..read", "invoice.field.Cost.read", "invoice.field.cost.READ", "invoice.field.cost.read "}
	for _, c := range codes {
		err := permission.ValidateDeclaration(c)
		require.ErrorIs(t, err, permission.ErrInvalidCode, "%q", c)
		require.NotErrorIs(t, err, permission.ErrInvalidFieldDeclaration, "%q", c)
	}
}

func TestEveryCoreCatalogCodeIsAValidDeclaration(t *testing.T) {
	require.NotEmpty(t, permission.Catalog())
	for _, d := range permission.Catalog() {
		require.NoError(t, permission.ValidateDeclaration(d.Code), d.Code)
	}
}

func TestSyncRejectsAReservedFieldDeclarationBeforeTouchingTheDatabase(t *testing.T) {
	// tx nil: bila validasi terlewat, Sync menyentuh database dan tes ini panik, bukan lulus hampa.
	defs := []permission.Def{
		{Code: "invoice.read", Description: "baca"},
		{Code: "invoice.field.internal_cost.export", Description: "rusak"},
	}

	res, err := permission.Sync(context.Background(), nil, defs)

	require.ErrorIs(t, err, permission.ErrInvalidFieldDeclaration)
	require.Empty(t, res.IDs, "tidak ada kode yang diproses")
}
