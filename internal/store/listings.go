package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	RegistrationIDs []string
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

type WardCount struct {
	WardNumber *string
	WardName   *string
	Count      int
}

func NewListingsRepository(db Querier) *ListingsRepository {
	slog.Debug("function entry", "function", "store.NewListingsRepository", "db_configured", db != nil)
	defer slog.Debug("function exit", "function", "store.NewListingsRepository")

	return &ListingsRepository{db: db}
}

func (r *ListingsRepository) GetListing(ctx context.Context, id string) (Listing, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.GetListing", "listing_id", id)
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.GetListing", "listing_id", id)

	var listing Listing
	var propertyType sql.NullString
	var wardNumber sql.NullString
	var wardName sql.NullString
	var sourceUpdatedAt sql.NullTime

	err := r.db.QueryRow(ctx, `
		SELECT
			listing.id,
			listing.address,
			listing.postal_code,
			listing.property_type,
			listing.ward_number,
			listing.ward_name,
			listing.latitude,
			listing.longitude,
			listing.source_updated_at,
			listing.ingested_at,
			listing.ingestion_run_id::text,
			ARRAY(
				SELECT registration.id
				FROM "Listings" AS registration
				WHERE registration.address = listing.address
				ORDER BY registration.id
			)
		FROM "Listings" AS listing
		WHERE listing.id = $1
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
		&listing.RegistrationIDs,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "listing lookup missed", "listing_id", id, "error", ErrNotFound)
			return Listing{}, ErrNotFound
		}
		slog.ErrorContext(ctx, "listing lookup failed", "listing_id", id, "error", err)
		return Listing{}, fmt.Errorf("get listing: %w", err)
	}

	listing.PropertyType = nullableStringPtr(propertyType)
	listing.WardNumber = nullableStringPtr(wardNumber)
	listing.WardName = nullableStringPtr(wardName)
	listing.SourceUpdatedAt = nullableTimePtr(sourceUpdatedAt)

	slog.InfoContext(ctx, "listing lookup completed", "listing_id", listing.ID)
	return listing, nil
}

func (r *ListingsRepository) ListPropertyTypes(ctx context.Context) ([]string, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.ListPropertyTypes")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.ListPropertyTypes")

	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT property_type
		FROM "Listings"
		WHERE property_type IS NOT NULL AND btrim(property_type) <> ''
		ORDER BY property_type
	`)
	if err != nil {
		slog.ErrorContext(ctx, "property type query failed", "error", err)
		return nil, fmt.Errorf("list property types: %w", err)
	}
	defer rows.Close()

	propertyTypes := []string{}
	for rows.Next() {
		var propertyType string
		if err := rows.Scan(&propertyType); err != nil {
			slog.ErrorContext(ctx, "property type scan failed", "error", err)
			return nil, fmt.Errorf("scan property type: %w", err)
		}
		propertyTypes = append(propertyTypes, propertyType)
	}
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "property type rows iteration failed", "error", err)
		return nil, fmt.Errorf("iterate property types: %w", err)
	}

	slog.InfoContext(ctx, "property types listed", "property_type_count", len(propertyTypes))
	return propertyTypes, nil
}

func (r *ListingsRepository) CountListings(ctx context.Context) (int, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.CountListings")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.CountListings")

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM "Listings"`).Scan(&total); err != nil {
		slog.ErrorContext(ctx, "listing count query failed", "error", err)
		return 0, fmt.Errorf("count listings: %w", err)
	}

	slog.InfoContext(ctx, "listings counted", "total_listings", total)
	return total, nil
}

func (r *ListingsRepository) LatestSuccessfulIngestionRun(ctx context.Context) (*time.Time, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.LatestSuccessfulIngestionRun")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.LatestSuccessfulIngestionRun")

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
			slog.InfoContext(ctx, "latest successful ingestion run not found")
			return nil, nil
		}
		slog.ErrorContext(ctx, "latest successful ingestion run query failed", "error", err)
		return nil, fmt.Errorf("latest successful ingestion run: %w", err)
	}

	slog.InfoContext(ctx, "latest successful ingestion run loaded", "found", finishedAt.Valid)
	return nullableTimePtr(finishedAt), nil
}

func (r *ListingsRepository) GetMetadata(ctx context.Context) (Metadata, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.GetMetadata")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.GetMetadata")

	total, err := r.CountListings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata load failed", "step", "count_listings", "error", err)
		return Metadata{}, err
	}

	propertyTypes, err := r.ListPropertyTypes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata load failed", "step", "list_property_types", "error", err)
		return Metadata{}, err
	}

	lastSuccessfulIngestionAt, err := r.LatestSuccessfulIngestionRun(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "metadata load failed", "step", "latest_successful_ingestion_run", "error", err)
		return Metadata{}, err
	}

	slog.InfoContext(ctx, "metadata loaded", "total_listings", total, "property_type_count", len(propertyTypes), "has_last_successful_ingestion_at", lastSuccessfulIngestionAt != nil)
	return Metadata{
		TotalListings:             total,
		PropertyTypes:             propertyTypes,
		LastSuccessfulIngestionAt: lastSuccessfulIngestionAt,
	}, nil
}

func (r *ListingsRepository) ListMapListings(ctx context.Context, query MapListingsQuery) ([]MapListing, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.ListMapListings", "zoom", query.Zoom, "search_present", strings.TrimSpace(query.Search) != "", "property_type_present", strings.TrimSpace(query.PropertyType) != "")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.ListMapListings", "zoom", query.Zoom)

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
		slog.ErrorContext(ctx, "map listings query failed", "zoom", query.Zoom, "error", err)
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
			slog.ErrorContext(ctx, "map listing scan failed", "error", err)
			return nil, fmt.Errorf("scan map listing: %w", err)
		}

		listing.PropertyType = nullableStringPtr(propertyType)
		listing.WardNumber = nullableStringPtr(wardNumber)
		listing.WardName = nullableStringPtr(wardName)
		listing.SourceUpdatedAt = nullableTimePtr(sourceUpdatedAt)
		listings = append(listings, listing)
	}
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "map listings rows iteration failed", "error", err)
		return nil, fmt.Errorf("iterate map listings: %w", err)
	}

	slog.InfoContext(ctx, "map listings listed", "zoom", query.Zoom, "listing_count", len(listings))
	return listings, nil
}

func (r *ListingsRepository) ListMapClusters(ctx context.Context, query MapListingsQuery) ([]MapCluster, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.ListMapClusters", "zoom", query.Zoom, "search_present", strings.TrimSpace(query.Search) != "", "property_type_present", strings.TrimSpace(query.PropertyType) != "")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.ListMapClusters", "zoom", query.Zoom)

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
		slog.ErrorContext(ctx, "map clusters query failed", "zoom", query.Zoom, "grid_size_meters", gridSize, "error", err)
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
			slog.ErrorContext(ctx, "map cluster scan failed", "error", err)
			return nil, fmt.Errorf("scan map cluster: %w", err)
		}

		cluster.ID = fmt.Sprintf("cluster:%d:%d:%d", query.Zoom, gridX, gridY)
		clusters = append(clusters, cluster)
	}
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "map clusters rows iteration failed", "error", err)
		return nil, fmt.Errorf("iterate map clusters: %w", err)
	}

	slog.InfoContext(ctx, "map clusters listed", "zoom", query.Zoom, "grid_size_meters", gridSize, "cluster_count", len(clusters))
	return clusters, nil
}

func (r *ListingsRepository) ListWardStats(ctx context.Context, query MapListingsQuery) ([]WardCount, error) {
	slog.DebugContext(ctx, "function entry", "function", "store.ListingsRepository.ListWardStats", "search_present", strings.TrimSpace(query.Search) != "", "property_type_present", strings.TrimSpace(query.PropertyType) != "")
	defer slog.DebugContext(ctx, "function exit", "function", "store.ListingsRepository.ListWardStats")

	filterSQL, args := mapFilterSQL(query)
	querySQL := `
		SELECT
			ward_number,
			ward_name,
			count(*)::integer AS listing_count
		FROM "Listings"
		WHERE ` + filterSQL + `
		GROUP BY ward_number, ward_name
		ORDER BY listing_count DESC, ward_number NULLS LAST, ward_name NULLS LAST
	`

	rows, err := r.db.Query(ctx, querySQL, args...)
	if err != nil {
		slog.ErrorContext(ctx, "ward stats query failed", "error", err)
		return nil, fmt.Errorf("list ward stats: %w", err)
	}
	defer rows.Close()

	wardCounts := []WardCount{}
	for rows.Next() {
		var wardCount WardCount
		var wardNumber sql.NullString
		var wardName sql.NullString
		if err := rows.Scan(&wardNumber, &wardName, &wardCount.Count); err != nil {
			slog.ErrorContext(ctx, "ward stat scan failed", "error", err)
			return nil, fmt.Errorf("scan ward stat: %w", err)
		}

		wardCount.WardNumber = nullableStringPtr(wardNumber)
		wardCount.WardName = nullableStringPtr(wardName)
		wardCounts = append(wardCounts, wardCount)
	}
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "ward stats rows iteration failed", "error", err)
		return nil, fmt.Errorf("iterate ward stats: %w", err)
	}

	slog.InfoContext(ctx, "ward stats listed", "ward_count", len(wardCounts))
	return wardCounts, nil
}

func mapFilterSQL(query MapListingsQuery) (string, []any) {
	slog.Debug("function entry", "function", "store.mapFilterSQL", "search_present", strings.TrimSpace(query.Search) != "", "property_type_present", strings.TrimSpace(query.PropertyType) != "")
	defer slog.Debug("function exit", "function", "store.mapFilterSQL")

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

	slog.Debug("map filter sql built", "condition_count", len(conditions), "arg_count", len(args))
	return strings.Join(conditions, "\n\t\t\tAND "), args
}

func clusterGridSizeMeters(zoom int) int {
	slog.Debug("function entry", "function", "store.clusterGridSizeMeters", "zoom", zoom)
	defer slog.Debug("function exit", "function", "store.clusterGridSizeMeters", "zoom", zoom)

	switch {
	case zoom <= 9:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 30000)
		return 30000
	case zoom == 10:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 18000)
		return 18000
	case zoom == 11:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 10000)
		return 10000
	case zoom == 12:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 5000)
		return 5000
	case zoom == 13:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 3000)
		return 3000
	default:
		slog.Debug("cluster grid size selected", "zoom", zoom, "grid_size_meters", 1500)
		return 1500
	}
}

func nullableStringPtr(value sql.NullString) *string {
	slog.Debug("function entry", "function", "store.nullableStringPtr", "valid", value.Valid)
	defer slog.Debug("function exit", "function", "store.nullableStringPtr", "valid", value.Valid)

	if !value.Valid {
		return nil
	}

	return &value.String
}

func nullableTimePtr(value sql.NullTime) *time.Time {
	slog.Debug("function entry", "function", "store.nullableTimePtr", "valid", value.Valid)
	defer slog.Debug("function exit", "function", "store.nullableTimePtr", "valid", value.Valid)

	if !value.Valid {
		return nil
	}

	return &value.Time
}
