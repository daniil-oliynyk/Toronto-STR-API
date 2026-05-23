package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

type BBox struct {
	West  float64
	South float64
	East  float64
	North float64
}

type MapListingsQuery struct {
	BBox         BBox
	Zoom         int
	Search       string
	PropertyType string
}

type MapListing struct {
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
}

type MapCluster struct {
	ID        string
	Count     int
	Latitude  float64
	Longitude float64
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

func (r *ListingsRepository) ListMapListings(ctx context.Context, query MapListingsQuery) ([]MapListing, error) {
	filterSQL, args := mapFilterSQL(query)
	querySQL := `
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
			ingested_at
		FROM "Listings"
		WHERE ` + filterSQL + `
		ORDER BY id
		LIMIT 5000
	`

	rows, err := r.db.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list map listings: %w", err)
	}
	defer rows.Close()

	listings := []MapListing{}
	for rows.Next() {
		var listing MapListing
		var propertyType sql.NullString
		var wardNumber sql.NullString
		var wardName sql.NullString
		var sourceUpdatedAt sql.NullTime

		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan map listing: %w", err)
		}

		listing.PropertyType = nullableStringPtr(propertyType)
		listing.WardNumber = nullableStringPtr(wardNumber)
		listing.WardName = nullableStringPtr(wardName)
		listing.SourceUpdatedAt = nullableTimePtr(sourceUpdatedAt)
		listings = append(listings, listing)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate map listings: %w", err)
	}

	return listings, nil
}

func (r *ListingsRepository) ListMapClusters(ctx context.Context, query MapListingsQuery) ([]MapCluster, error) {
	gridSize := clusterGridSizeMeters(query.Zoom)
	filterSQL, args := mapFilterSQL(query)
	args = append(args, gridSize)
	gridSizeParam := fmt.Sprintf("$%d", len(args))

	querySQL := `
		WITH filtered AS (
			SELECT
				geom,
				ST_Transform(geom, 3857) AS web_mercator
			FROM "Listings"
			WHERE ` + filterSQL + `
		),
		bucketed AS (
			SELECT
				floor(ST_X(web_mercator) / ` + gridSizeParam + `)::bigint AS grid_x,
				floor(ST_Y(web_mercator) / ` + gridSizeParam + `)::bigint AS grid_y,
				web_mercator
			FROM filtered
		),
		clustered AS (
			SELECT
				grid_x,
				grid_y,
				count(*)::integer AS listing_count,
				ST_Transform(ST_Centroid(ST_Collect(web_mercator)), 4326) AS centroid
			FROM bucketed
			GROUP BY grid_x, grid_y
		)
		SELECT
			grid_x,
			grid_y,
			listing_count,
			ST_Y(centroid) AS latitude,
			ST_X(centroid) AS longitude
		FROM clustered
		ORDER BY listing_count DESC, grid_x, grid_y
		LIMIT 5000
	`

	rows, err := r.db.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list map clusters: %w", err)
	}
	defer rows.Close()

	clusters := []MapCluster{}
	for rows.Next() {
		var gridX int64
		var gridY int64
		var cluster MapCluster
		if err := rows.Scan(
			&gridX,
			&gridY,
			&cluster.Count,
			&cluster.Latitude,
			&cluster.Longitude,
		); err != nil {
			return nil, fmt.Errorf("scan map cluster: %w", err)
		}

		cluster.ID = fmt.Sprintf("cluster:%d:%d:%d", query.Zoom, gridX, gridY)
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate map clusters: %w", err)
	}

	return clusters, nil
}

func mapFilterSQL(query MapListingsQuery) (string, []any) {
	args := []any{
		query.BBox.West,
		query.BBox.South,
		query.BBox.East,
		query.BBox.North,
	}
	conditions := []string{
		"geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)",
		"ST_Intersects(geom, ST_MakeEnvelope($1, $2, $3, $4, 4326))",
	}

	search := strings.TrimSpace(query.Search)
	if search != "" {
		args = append(args, "%"+search+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions, "(address ILIKE "+placeholder+" OR postal_code ILIKE "+placeholder+" OR id ILIKE "+placeholder+")")
	}

	propertyType := strings.TrimSpace(query.PropertyType)
	if propertyType != "" {
		args = append(args, propertyType)
		placeholder := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions, "property_type = "+placeholder)
	}

	return strings.Join(conditions, "\n\t\t\tAND "), args
}

func clusterGridSizeMeters(zoom int) int {
	switch {
	case zoom <= 10:
		return 1000
	case zoom == 11:
		return 600
	case zoom == 12:
		return 300
	default:
		return 150
	}
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
