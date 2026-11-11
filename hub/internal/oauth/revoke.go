package oauth

import (
	"context"
	"log/slog"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
)

// Revoker withdraws the hub's grants at providers when accounts are unlinked or deleted.
type Revoker struct {
	d *deps.Deps
}

func NewRevoker(d *deps.Deps) *Revoker {
	return &Revoker{d: d}
}

// RevokeAll is best effort: the identity is gone on our side either way, and a
// provider being down must not keep a user from leaving.
func (r *Revoker) RevokeAll(ctx context.Context, identities []sqlc.Identity) {
	for _, identity := range identities {
		if identity.ProviderToken == nil {
			continue
		}
		if err := r.revoke(ctx, identity); err != nil {
			slog.Warn("could not revoke provider token", "provider", identity.Provider, "err", err)
		}
	}
}

func (r *Revoker) revoke(ctx context.Context, identity sqlc.Identity) error {
	row, err := r.d.Q().GetLoginProvider(ctx, identity.Provider)
	if err != nil {
		return err
	}
	provider, err := Build(ctx, r.d, row)
	if err != nil {
		return err
	}
	token, err := r.d.Box().Open(identity.ProviderToken)
	if err != nil {
		return err
	}
	return provider.Revoke(ctx, string(token))
}
