// Package metadatadoc persists immutable metadata registries. Documents remain
// owned by the caller's Task Core/history transaction; this package does not
// introduce a second task or draft store.
package metadatadoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type Store struct{ pool *pgxpool.Pool }

var _ app.Repository = (*Store)(nil)

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) initialize(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("metadata database is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin metadata initialization: %w", err)
	}
	defer tx.Rollback(ctx)
	registry := domain.StandardRegistry()
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_definition_versions (version,registry) VALUES ($1,$2) ON CONFLICT (version) DO NOTHING`, registry.DefinitionsVersion, data); err != nil {
		return fmt.Errorf("initialize metadata definitions: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_definition_head (singleton,version) VALUES (TRUE,$1) ON CONFLICT (singleton) DO NOTHING`, registry.DefinitionsVersion); err != nil {
		return fmt.Errorf("initialize metadata head: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metadata initialization: %w", err)
	}
	return nil
}

func (s *Store) Current(ctx context.Context) (domain.Registry, error) {
	if err := s.initialize(ctx); err != nil {
		return domain.Registry{}, err
	}
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT v.registry FROM metadata_definition_head h JOIN metadata_definition_versions v ON v.version=h.version WHERE h.singleton=TRUE`).Scan(&data); err != nil {
		return domain.Registry{}, fmt.Errorf("read current metadata definitions: %w", err)
	}
	return decodeRegistry(data)
}
func (s *Store) Get(ctx context.Context, version string) (domain.Registry, error) {
	if err := s.initialize(ctx); err != nil {
		return domain.Registry{}, err
	}
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT registry FROM metadata_definition_versions WHERE version=$1`, version).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Registry{}, domain.ErrUnsupportedVersion
		}
		return domain.Registry{}, fmt.Errorf("read metadata definitions: %w", err)
	}
	registry, err := decodeRegistry(data)
	if err != nil {
		return domain.Registry{}, err
	}
	if registry.DefinitionsVersion != version {
		return domain.Registry{}, errors.New("metadata registry version mismatch")
	}
	return registry, nil
}
func (s *Store) Save(ctx context.Context, expected string, next domain.Registry) error {
	if err := domain.ValidateRegistry(next); err != nil {
		return err
	}
	if err := s.initialize(ctx); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin metadata definition update: %w", err)
	}
	defer tx.Rollback(ctx)
	var version string
	if err := tx.QueryRow(ctx, `SELECT version FROM metadata_definition_head WHERE singleton=TRUE FOR UPDATE`).Scan(&version); err != nil {
		return fmt.Errorf("lock metadata definitions: %w", err)
	}
	if version != expected {
		return domain.ErrConflict
	}
	var previousData []byte
	if err := tx.QueryRow(ctx, `SELECT registry FROM metadata_definition_versions WHERE version=$1`, version).Scan(&previousData); err != nil {
		return fmt.Errorf("read locked metadata definitions: %w", err)
	}
	previous, err := decodeRegistry(previousData)
	if err != nil {
		return err
	}
	derived, err := domain.NewRegistry(previous, next.CustomDefinitions())
	if err != nil {
		return err
	}
	if derived.DefinitionsVersion != next.DefinitionsVersion {
		return fmt.Errorf("%w: immutable registry content mismatch", domain.ErrInvalidInput)
	}
	data, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode metadata definitions: %w", err)
	}
	// An existing content-addressed version can be selected again, but never changed.
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_definition_versions(version,registry) VALUES($1,$2) ON CONFLICT(version) DO NOTHING`, next.DefinitionsVersion, data); err != nil {
		return fmt.Errorf("insert metadata definition version: %w", err)
	}
	var equal bool
	if err := tx.QueryRow(ctx, `SELECT registry=$2::jsonb FROM metadata_definition_versions WHERE version=$1`, next.DefinitionsVersion, data).Scan(&equal); err != nil {
		return fmt.Errorf("verify immutable metadata definitions: %w", err)
	}
	if !equal {
		return errors.New("immutable metadata definition collision")
	}
	if _, err := tx.Exec(ctx, `UPDATE metadata_definition_head SET version=$1 WHERE singleton=TRUE`, next.DefinitionsVersion); err != nil {
		return fmt.Errorf("advance metadata definition head: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metadata definitions: %w", err)
	}
	return nil
}
func decodeRegistry(data []byte) (domain.Registry, error) {
	var registry domain.Registry
	if err := domain.DecodeJSON(data, &registry); err != nil {
		return domain.Registry{}, fmt.Errorf("decode stored metadata definitions: %w", err)
	}
	if err := domain.ValidateRegistry(registry); err != nil {
		return domain.Registry{}, fmt.Errorf("verify stored metadata definitions: %w", err)
	}
	return registry, nil
}

// DecodeStored upgrades only NULL legacy rows; a present document is always
// authoritative and must validate against its persisted registry version.
func (s *Store) DecodeStored(ctx context.Context, data []byte, legacy domain.Legacy) (domain.Document, error) {
	if data == nil {
		return domain.FromLegacy(legacy)
	}
	return app.NewService(s).Decode(ctx, data)
}
