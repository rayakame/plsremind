package oauth

import (
	"context"
	"log/slog"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

// RevokeAll withdraws the hub's grants at the providers of the given identities.
// It is best effort: the identity is gone on our side either way, and a
// provider being down must not keep a user from leaving.
func (s *Service) RevokeAll(ctx context.Context, identities []sqlc.Identity) {
	for _, identity := range identities {
		if identity.ProviderToken == nil {
			continue
		}
		if err := s.revoke(ctx, identity); err != nil {
			slog.Warn("could not revoke provider token", "provider", identity.Provider, "err", err)
		}
	}
}

func (s *Service) revoke(ctx context.Context, identity sqlc.Identity) error {
	row, err := s.d.Q().GetLoginProvider(ctx, identity.Provider)
	if err != nil {
		return err
	}
	provider, err := s.Build(ctx, row)
	if err != nil {
		return err
	}
	token, err := s.d.Box().Open(identity.ProviderToken)
	if err != nil {
		return err
	}
	return provider.Revoke(ctx, string(token))
}
