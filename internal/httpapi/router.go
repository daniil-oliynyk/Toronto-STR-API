package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/daniil-oliynyk/go-api/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

type MetadataProvider interface {
	GetMetadata(ctx context.Context) (store.Metadata, error)
}

type ListingProvider interface {
	GetListing(ctx context.Context, id string) (store.Listing, error)
}

type MapProvider interface {
	ListMapListings(ctx context.Context, query store.MapListingsQuery) ([]store.MapListing, error)
	ListMapClusters(ctx context.Context, query store.MapListingsQuery) ([]store.MapCluster, error)
}

type StatsProvider interface {
	ListWardStats(ctx context.Context, query store.MapListingsQuery) ([]store.WardCount, error)
}

type Dependencies struct {
	ReadinessChecker ReadinessChecker
	MetadataProvider MetadataProvider
	ListingProvider  ListingProvider
	MapProvider      MapProvider
	StatsProvider    StatsProvider
	CORSOrigins      []string
	InternalAPIKey   string
}

func NewRouter(deps Dependencies) http.Handler {
	slog.Debug(
		"function entry",
		"function", "httpapi.NewRouter",
		"cors_origin_count", len(deps.CORSOrigins),
		"internal_api_key_configured", deps.InternalAPIKey != "",
	)
	defer slog.Debug("function exit", "function", "httpapi.NewRouter")

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware(deps.CORSOrigins))

	router.Get("/healthz", healthzHandler)
	router.Get("/readyz", readyzHandler(deps.ReadinessChecker))
	router.Route("/api", func(api chi.Router) {
		api.Use(internalAPIKeyMiddleware(deps.InternalAPIKey))
		api.Get("/meta", metaHandler(deps.MetadataProvider))
		api.Get("/listings/map", mapListingsHandler(deps.MapProvider))
		api.Get("/listings/{id}", listingDetailHandler(deps.ListingProvider))
		api.Get("/stats/wards", wardStatsHandler(deps.StatsProvider))
	})

	return router
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	slog.Debug("function entry", "function", "httpapi.healthzHandler")
	defer slog.Debug("function exit", "function", "httpapi.healthzHandler", "status", http.StatusOK)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	slog.Debug("function entry", "function", "httpapi.corsMiddleware", "origin_count", len(allowedOrigins))
	defer slog.Debug("function exit", "function", "httpapi.corsMiddleware")

	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		slog.Debug("function entry", "function", "httpapi.corsMiddleware.wrap")
		defer slog.Debug("function exit", "function", "httpapi.corsMiddleware.wrap")

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slog.DebugContext(r.Context(), "function entry", "function", "httpapi.corsMiddleware.handler", "method", r.Method, "path", r.URL.Path)
			defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.corsMiddleware.handler", "method", r.Method, "path", r.URL.Path)

			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				slog.InfoContext(r.Context(), "cors preflight handled", "origin", origin, "path", r.URL.Path, "status", http.StatusNoContent)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func internalAPIKeyMiddleware(expectedKey string) func(http.Handler) http.Handler {
	slog.Debug("function entry", "function", "httpapi.internalAPIKeyMiddleware", "configured", expectedKey != "")
	defer slog.Debug("function exit", "function", "httpapi.internalAPIKeyMiddleware")

	if expectedKey == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			providedKey := r.Header.Get("X-Internal-API-Key")
			if subtle.ConstantTimeCompare([]byte(providedKey), []byte(expectedKey)) != 1 {
				slog.WarnContext(r.Context(), "internal api key rejected", "method", r.Method, "path", r.URL.Path, "status", http.StatusUnauthorized)
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "unauthorized",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func readyzHandler(checker ReadinessChecker) http.HandlerFunc {
	slog.Debug("function entry", "function", "httpapi.readyzHandler")
	defer slog.Debug("function exit", "function", "httpapi.readyzHandler")

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "function entry", "function", "httpapi.readyzHandler.handler", "method", r.Method, "path", r.URL.Path)
		defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.readyzHandler.handler", "method", r.Method, "path", r.URL.Path)

		if checker == nil {
			slog.ErrorContext(r.Context(), "readiness check failed", "reason", "checker_missing", "status", http.StatusServiceUnavailable)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "readiness checker is not configured",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := checker.Ping(ctx); err != nil {
			slog.ErrorContext(r.Context(), "readiness check failed", "error", err, "status", http.StatusServiceUnavailable)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "database is not reachable",
			})
			return
		}

		slog.InfoContext(r.Context(), "readiness check passed", "status", http.StatusOK)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type metadataResponse struct {
	TotalListings             int        `json:"totalListings"`
	PropertyTypes             []string   `json:"propertyTypes"`
	LastSuccessfulIngestionAt *time.Time `json:"lastSuccessfulIngestionAt"`
}

func metaHandler(provider MetadataProvider) http.HandlerFunc {
	slog.Debug("function entry", "function", "httpapi.metaHandler")
	defer slog.Debug("function exit", "function", "httpapi.metaHandler")

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "function entry", "function", "httpapi.metaHandler.handler", "method", r.Method, "path", r.URL.Path)
		defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.metaHandler.handler", "method", r.Method, "path", r.URL.Path)

		if provider == nil {
			slog.ErrorContext(r.Context(), "metadata request failed", "reason", "provider_missing", "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "metadata provider is not configured",
			})
			return
		}

		metadata, err := provider.GetMetadata(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "metadata request failed", "error", err, "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load metadata",
			})
			return
		}

		slog.InfoContext(r.Context(), "metadata request completed", "total_listings", metadata.TotalListings, "property_type_count", len(metadata.PropertyTypes), "status", http.StatusOK)
		writeJSON(w, http.StatusOK, metadataResponse{
			TotalListings:             metadata.TotalListings,
			PropertyTypes:             metadata.PropertyTypes,
			LastSuccessfulIngestionAt: metadata.LastSuccessfulIngestionAt,
		})
	}
}

type listingResponse struct {
	ID              string     `json:"id"`
	Address         string     `json:"address"`
	PostalCode      string     `json:"postalCode"`
	PropertyType    *string    `json:"propertyType"`
	WardNumber      *string    `json:"wardNumber"`
	WardName        *string    `json:"wardName"`
	Latitude        float64    `json:"latitude"`
	Longitude       float64    `json:"longitude"`
	SourceUpdatedAt *time.Time `json:"sourceUpdatedAt"`
	IngestedAt      time.Time  `json:"ingestedAt"`
	IngestionRunID  string     `json:"ingestionRunId"`
	RegistrationIDs []string   `json:"registrationIds"`
}

func listingDetailHandler(provider ListingProvider) http.HandlerFunc {
	slog.Debug("function entry", "function", "httpapi.listingDetailHandler")
	defer slog.Debug("function exit", "function", "httpapi.listingDetailHandler")

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "function entry", "function", "httpapi.listingDetailHandler.handler", "method", r.Method, "path", r.URL.Path)
		defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.listingDetailHandler.handler", "method", r.Method, "path", r.URL.Path)

		if provider == nil {
			slog.ErrorContext(r.Context(), "listing detail request failed", "reason", "provider_missing", "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "listing provider is not configured",
			})
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			slog.WarnContext(r.Context(), "listing detail request failed", "reason", "missing_id", "status", http.StatusNotFound)
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "listing not found",
			})
			return
		}

		listing, err := provider.GetListing(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				slog.WarnContext(r.Context(), "listing detail request failed", "listing_id", id, "error", err, "status", http.StatusNotFound)
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "listing not found",
				})
				return
			}

			slog.ErrorContext(r.Context(), "listing detail request failed", "listing_id", id, "error", err, "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load listing",
			})
			return
		}

		slog.InfoContext(r.Context(), "listing detail request completed", "listing_id", listing.ID, "status", http.StatusOK)
		writeJSON(w, http.StatusOK, listingResponse{
			ID:              listing.ID,
			Address:         listing.Address,
			PostalCode:      listing.PostalCode,
			PropertyType:    listing.PropertyType,
			WardNumber:      listing.WardNumber,
			WardName:        listing.WardName,
			Latitude:        listing.Latitude,
			Longitude:       listing.Longitude,
			SourceUpdatedAt: listing.SourceUpdatedAt,
			IngestedAt:      listing.IngestedAt,
			IngestionRunID:  listing.IngestionRunID,
			RegistrationIDs: listing.RegistrationIDs,
		})
	}
}

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	Type       string               `json:"type"`
	ID         string               `json:"id,omitempty"`
	Geometry   geoJSONPoint         `json:"geometry"`
	Properties mapListingProperties `json:"properties"`
}

type geoJSONPoint struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"`
}

type mapListingProperties struct {
	Cluster         bool       `json:"cluster"`
	Count           int        `json:"count,omitempty"`
	ID              string     `json:"id,omitempty"`
	Address         string     `json:"address,omitempty"`
	PostalCode      string     `json:"postalCode,omitempty"`
	PropertyType    *string    `json:"propertyType,omitempty"`
	WardNumber      *string    `json:"wardNumber,omitempty"`
	WardName        *string    `json:"wardName,omitempty"`
	SourceUpdatedAt *time.Time `json:"sourceUpdatedAt,omitempty"`
	IngestedAt      *time.Time `json:"ingestedAt,omitempty"`
}

const individualListingsMinZoom = 16

func mapListingsHandler(provider MapProvider) http.HandlerFunc {
	slog.Debug("function entry", "function", "httpapi.mapListingsHandler")
	defer slog.Debug("function exit", "function", "httpapi.mapListingsHandler")

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "function entry", "function", "httpapi.mapListingsHandler.handler", "method", r.Method, "path", r.URL.Path)
		defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.mapListingsHandler.handler", "method", r.Method, "path", r.URL.Path)

		query, err := parseMapQuery(r.URL.Query())
		if err != nil {
			slog.WarnContext(r.Context(), "map listings request failed", "error", err, "status", http.StatusBadRequest)
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}

		if provider == nil {
			slog.ErrorContext(r.Context(), "map listings request failed", "reason", "provider_missing", "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "map provider is not configured",
			})
			return
		}

		storeQuery := store.MapListingsQuery{
			BBox: store.BBox{
				West:  query.BBox.West,
				South: query.BBox.South,
				East:  query.BBox.East,
				North: query.BBox.North,
			},
			Zoom:         query.Zoom,
			Search:       query.Search,
			PropertyType: query.PropertyType,
		}

		if query.Zoom < individualListingsMinZoom {
			clusters, err := provider.ListMapClusters(r.Context(), storeQuery)
			if err != nil {
				slog.ErrorContext(r.Context(), "map clusters request failed", "zoom", query.Zoom, "error", err, "status", http.StatusInternalServerError)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "failed to load map clusters",
				})
				return
			}

			slog.InfoContext(r.Context(), "map clusters request completed", "zoom", query.Zoom, "cluster_count", len(clusters), "status", http.StatusOK)
			writeJSON(w, http.StatusOK, mapClustersFeatureCollection(clusters))
			return
		}

		listings, err := provider.ListMapListings(r.Context(), storeQuery)
		if err != nil {
			slog.ErrorContext(r.Context(), "map listings request failed", "zoom", query.Zoom, "error", err, "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load map listings",
			})
			return
		}

		slog.InfoContext(r.Context(), "map listings request completed", "zoom", query.Zoom, "listing_count", len(listings), "status", http.StatusOK)
		writeJSON(w, http.StatusOK, mapListingsFeatureCollection(listings))
	}
}

func emptyFeatureCollection() geoJSONFeatureCollection {
	slog.Debug("function entry", "function", "httpapi.emptyFeatureCollection")
	defer slog.Debug("function exit", "function", "httpapi.emptyFeatureCollection")

	return geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
	}
}

func mapListingsFeatureCollection(listings []store.MapListing) geoJSONFeatureCollection {
	slog.Debug("function entry", "function", "httpapi.mapListingsFeatureCollection", "listing_count", len(listings))
	defer slog.Debug("function exit", "function", "httpapi.mapListingsFeatureCollection")

	features := make([]geoJSONFeature, 0, len(listings))
	for _, listing := range listings {
		features = append(features, geoJSONFeature{
			Type: "Feature",
			ID:   listing.ID,
			Geometry: geoJSONPoint{
				Type:        "Point",
				Coordinates: [2]float64{listing.Longitude, listing.Latitude},
			},
			Properties: mapListingProperties{
				Cluster:         false,
				ID:              listing.ID,
				Address:         listing.Address,
				PostalCode:      listing.PostalCode,
				PropertyType:    listing.PropertyType,
				WardNumber:      listing.WardNumber,
				WardName:        listing.WardName,
				SourceUpdatedAt: listing.SourceUpdatedAt,
				IngestedAt:      &listing.IngestedAt,
			},
		})
	}

	return geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	}
}

func mapClustersFeatureCollection(clusters []store.MapCluster) geoJSONFeatureCollection {
	slog.Debug("function entry", "function", "httpapi.mapClustersFeatureCollection", "cluster_count", len(clusters))
	defer slog.Debug("function exit", "function", "httpapi.mapClustersFeatureCollection")

	features := make([]geoJSONFeature, 0, len(clusters))
	for _, cluster := range clusters {
		features = append(features, geoJSONFeature{
			Type: "Feature",
			ID:   cluster.ID,
			Geometry: geoJSONPoint{
				Type:        "Point",
				Coordinates: [2]float64{cluster.Longitude, cluster.Latitude},
			},
			Properties: mapListingProperties{
				Cluster: true,
				Count:   cluster.Count,
			},
		})
	}

	return geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	}
}

type wardStatsResponse struct {
	Total int                 `json:"total"`
	Wards []wardCountResponse `json:"wards"`
}

type wardCountResponse struct {
	WardNumber *string `json:"wardNumber"`
	WardName   *string `json:"wardName"`
	Count      int     `json:"count"`
}

func wardStatsHandler(provider StatsProvider) http.HandlerFunc {
	slog.Debug("function entry", "function", "httpapi.wardStatsHandler")
	defer slog.Debug("function exit", "function", "httpapi.wardStatsHandler")

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "function entry", "function", "httpapi.wardStatsHandler.handler", "method", r.Method, "path", r.URL.Path)
		defer slog.DebugContext(r.Context(), "function exit", "function", "httpapi.wardStatsHandler.handler", "method", r.Method, "path", r.URL.Path)

		query, err := parseStatsQuery(r.URL.Query())
		if err != nil {
			slog.WarnContext(r.Context(), "ward stats request failed", "error", err, "status", http.StatusBadRequest)
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}

		if provider == nil {
			slog.ErrorContext(r.Context(), "ward stats request failed", "reason", "provider_missing", "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "stats provider is not configured",
			})
			return
		}

		wardCounts, err := provider.ListWardStats(r.Context(), store.MapListingsQuery{
			BBox: store.BBox{
				West:  query.BBox.West,
				South: query.BBox.South,
				East:  query.BBox.East,
				North: query.BBox.North,
			},
			Search:       query.Search,
			PropertyType: query.PropertyType,
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "ward stats request failed", "error", err, "status", http.StatusInternalServerError)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load ward stats",
			})
			return
		}

		slog.InfoContext(r.Context(), "ward stats request completed", "ward_count", len(wardCounts), "status", http.StatusOK)
		writeJSON(w, http.StatusOK, wardStatsResponseFromStore(wardCounts))
	}
}

func wardStatsResponseFromStore(wardCounts []store.WardCount) wardStatsResponse {
	slog.Debug("function entry", "function", "httpapi.wardStatsResponseFromStore", "ward_count", len(wardCounts))
	defer slog.Debug("function exit", "function", "httpapi.wardStatsResponseFromStore")

	wards := make([]wardCountResponse, 0, len(wardCounts))
	total := 0
	for _, wardCount := range wardCounts {
		total += wardCount.Count
		wards = append(wards, wardCountResponse{
			WardNumber: wardCount.WardNumber,
			WardName:   wardCount.WardName,
			Count:      wardCount.Count,
		})
	}

	return wardStatsResponse{
		Total: total,
		Wards: wards,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	slog.Debug("function entry", "function", "httpapi.writeJSON", "status", status)
	defer slog.Debug("function exit", "function", "httpapi.writeJSON", "status", status)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
