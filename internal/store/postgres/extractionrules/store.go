package extractionrules

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/extractionrules"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool} }
func (s *Store) Get(ctx context.Context) (app.RuleSet, error) {
	var set app.RuleSet
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT rules_version,definitions_version,rules FROM extraction_rules WHERE singleton=TRUE`).Scan(&set.RulesVersion, &set.DefinitionsVersion, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return set, app.ErrNotFound
	}
	if err != nil {
		return set, err
	}
	err = domain.DecodeJSON(data, &set.Rules)
	return set, err
}
func (s *Store) Save(ctx context.Context, set app.RuleSet, expected uint64) (app.RuleSet, error) {
	data, err := json.Marshal(set.Rules)
	if err != nil {
		return set, err
	}
	// Lock the metadata head as well as the rule CAS so registry edits cannot
	// commit between application validation and this write.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return set, err
	}
	defer tx.Rollback(ctx)
	var version string
	if err = tx.QueryRow(ctx, `SELECT version FROM metadata_definition_head WHERE singleton=TRUE FOR SHARE`).Scan(&version); err != nil {
		return set, err
	}
	if version != set.DefinitionsVersion {
		return set, app.ErrConflict
	}
	query := `UPDATE extraction_rules SET rules_version=rules_version+1,definitions_version=$1,rules=$2,updated_at=now() WHERE singleton=TRUE AND rules_version=$3 RETURNING rules_version`
	if expected == 0 {
		query = `INSERT INTO extraction_rules(singleton,rules_version,definitions_version,rules) SELECT TRUE,1,$1,$2 WHERE $3::bigint=0 ON CONFLICT(singleton) DO NOTHING RETURNING rules_version`
	}
	err = tx.QueryRow(ctx, query, set.DefinitionsVersion, data, expected).Scan(&set.RulesVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return set, app.ErrConflict
	}
	if err != nil {
		return set, err
	}
	return set, tx.Commit(ctx)
}
