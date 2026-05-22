package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type ListingsRepository struct {
	db Querier
}

type Listing struct {
	ID              string
	Address         string
	PostalCode      string
	PropertyType    *string
	WardNumber      *string
	WardName        *string
	Latitude        float64
	Longitude       float64
	SourceUpdatedAt *time.Time
	IngestedAt      time.Time
	IngestionRunID  string
}

type Metadata struct {
	TotalListings             int
	PropertyTypes             []string
	LastSuccessfulIngestionAt *time.Time
}

func NewListingsRepository(db Querier) *ListingsRepository {
	return &ListingsRepository{db: db}
}

func (r *ListingsRepository) GetListing(ctx context.Context, id string) (Listing, error) {
	var listing Listing
	var propertyType sql.NullString
	var wardNumber sql.NullString
	var wardName sql.NullString
	var sourceUpdatedAt sql.NullTime

	err := r.db.QueryRow(ctx, `
		SELECT
			id,
			address,
			postal_code,
			property_type,
			ward_number,
			ward_name,
			latitude,
			longitude,
			source_updated_at,
			ingested_at,
			ingestion_run_id::text
		FROM "Listings"
		WHERE id = $1
	`, id).Scan(
		&listing.ID,
		&listing.Address,
		&listing.PostalCode,
		&propertyType,
		&wardNumber,
		&wardName,
		&listing.Latitude,
		&listing.Longitude,
		&sourceUpdatedAt,
		&listing.IngestedAt,
		&listing.IngestionRunID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Listing{}, ErrNotFound
		}
		return Listing{}, fmt.Errorf("get listing: %w", err)
	}

	listing.PropertyType = nullableStringPtr(propertyType)
	listing.WardNumber = nullableStringPtr(wardNumber)
	listing.WardName = nullableStringPtr(wardName)
	listing.SourceUpdatedAt = nullableTimePtr(sourceUpdatedAt)

	return listing, nil
}

func (r *ListingsRepository) ListPropertyTypes(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT property_type
		FROM "Listings"
		WHERE property_type IS NOT NULL AND btrim(property_type) <> ''
		ORDER BY property_type
	`)
	if err != nil {
		return nil, fmt.Errorf("list property types: %w", err)
	}
	defer rows.Close()

	propertyTypes := []string{}
	for rows.Next() {
		var propertyType string
		if err := rows.Scan(&propertyType); err != nil {
			return nil, fmt.Errorf("scan property type: %w", err)
		}
		propertyTypes = append(propertyTypes, propertyType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate property types: %w", err)
	}

	return propertyTypes, nil
}

func (r *ListingsRepository) CountListings(ctx context.Context) (int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM "Listings"`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count listings: %w", err)
	}

	return total, nil
}

func (r *ListingsRepository) LatestSuccessfulIngestionRun(ctx context.Context) (*time.Time, error) {
	var finishedAt sql.NullTime
	err := r.db.QueryRow(ctx, `
		SELECT finished_at
		FROM ingestion_runs
		WHERE status = 'success' AND finished_at IS NOT NULL
		ORDER BY finished_at DESC
		LIMIT 1
	`).Scan(&finishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("latest successful ingestion run: %w", err)
	}

	return nullableTimePtr(finishedAt), nil
}

func (r *ListingsRepository) GetMetadata(ctx context.Context) (Metadata, error) {
	total, err := r.CountListings(ctx)
	if err != nil {
		return Metadata{}, err
	}

	propertyTypes, err := r.ListPropertyTypes(ctx)
	if err != nil {
		return Metadata{}, err
	}

	lastSuccessfulIngestionAt, err := r.LatestSuccessfulIngestionRun(ctx)
	if err != nil {
		return Metadata{}, err
	}

	return Metadata{
		TotalListings:             total,
		PropertyTypes:             propertyTypes,
		LastSuccessfulIngestionAt: lastSuccessfulIngestionAt,
	}, nil
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return &value.String
}

func nullableTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}

	return &value.Time
}
