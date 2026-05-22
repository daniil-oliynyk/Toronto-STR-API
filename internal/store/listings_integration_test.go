package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListingsRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_SUPABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_SUPABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	runID := "00000000-0000-0000-0000-000000000001"
	finishedAt := time.Date(2026, 5, 21, 12, 30, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ingestion_runs (
			id,
			source_url,
			status,
			started_at,
			finished_at,
			rows_fetched,
			rows_inserted,
			duration_ms
		)
		VALUES ($1, $2, 'success', $3, $3, 1, 1, 100)
	`, runID, "https://example.com/source.json", finishedAt); err != nil {
		t.Fatalf("insert ingestion run: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO "Listings" (
			id,
			address,
			postal_code,
			property_type,
			ward_number,
			ward_name,
			latitude,
			longitude,
			geom,
			source_updated_at,
			ingested_at,
			ingestion_run_id,
			raw_payload
		)
		VALUES (
			'STR-1',
			'100 Queen St W',
			'M5H2N2',
			'Entire home/apt',
			'10',
			'Spadina-Fort York',
			43.6532,
			-79.3832,
			ST_SetSRID(ST_MakePoint(-79.3832, 43.6532), 4326),
			$1,
			$1,
			$2,
			'{}'::jsonb
		)
	`, finishedAt, runID); err != nil {
		t.Fatalf("insert listing: %v", err)
	}

	repo := NewListingsRepository(tx)

	listing, err := repo.GetListing(ctx, "STR-1")
	if err != nil {
		t.Fatalf("get listing: %v", err)
	}
	if listing.Address != "100 Queen St W" {
		t.Fatalf("expected address %q, got %q", "100 Queen St W", listing.Address)
	}
	if listing.PropertyType == nil || *listing.PropertyType != "Entire home/apt" {
		t.Fatalf("unexpected property type: %v", listing.PropertyType)
	}

	propertyTypes, err := repo.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatalf("list property types: %v", err)
	}
	if len(propertyTypes) != 1 || propertyTypes[0] != "Entire home/apt" {
		t.Fatalf("unexpected property types: %v", propertyTypes)
	}

	total, err := repo.CountListings(ctx)
	if err != nil {
		t.Fatalf("count listings: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}

	lastSuccess, err := repo.LatestSuccessfulIngestionRun(ctx)
	if err != nil {
		t.Fatalf("latest successful ingestion run: %v", err)
	}
	if lastSuccess == nil || !lastSuccess.Equal(finishedAt) {
		t.Fatalf("unexpected latest success: %v", lastSuccess)
	}
}
