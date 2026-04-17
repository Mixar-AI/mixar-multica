package service

import (
	"context"
	"errors"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DBTokenProvider implements TokenProvider by looking up workspace_integration
// rows. Falls back to the GITHUB_TOKEN env var when no workspace integration
// exists for the workspace.
type DBTokenProvider struct {
	Queries *db.Queries
}

// NewDBTokenProvider creates a DBTokenProvider backed by the given queries.
func NewDBTokenProvider(q *db.Queries) *DBTokenProvider {
	return &DBTokenProvider{Queries: q}
}

// GetGitHubToken returns the decrypted GitHub access token for the workspace.
// Returns GITHUB_TOKEN env var if no workspace integration is found.
func (p *DBTokenProvider) GetGitHubToken(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	pgUUID := pgtype.UUID{Bytes: workspaceID, Valid: true}

	row, err := p.Queries.GetWorkspaceIntegration(ctx, db.GetWorkspaceIntegrationParams{
		WorkspaceID: pgUUID,
		Platform:    "github",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Fall back to env var.
			if t := os.Getenv("GITHUB_TOKEN"); t != "" {
				return t, nil
			}
			return "", nil
		}
		return "", err
	}

	return crypto.DecryptToken(row.AccessToken)
}
