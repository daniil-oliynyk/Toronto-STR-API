package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

type Dependencies struct {
	ReadinessChecker ReadinessChecker
	MetadataProvider MetadataProvider
	ListingProvider  ListingProvider
	MapProvider      MapProvider
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	router.Get("/healthz", healthzHandler)
	router.Get("/readyz", readyzHandler(deps.ReadinessChecker))
	router.Get("/api/meta", metaHandler(deps.MetadataProvider))
	router.Get("/api/listings/map", mapListingsHandler(deps.MapProvider))
	router.Get("/api/listings/{id}", listingDetailHandler(deps.ListingProvider))

	return router
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func readyzHandler(checker ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "readiness checker is not configured",
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := checker.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"error":  "database is not reachable",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type metadataResponse struct {
	TotalListings             int        `json:"totalListings"`
	PropertyTypes             []string   `json:"propertyTypes"`
	LastSuccessfulIngestionAt *time.Time `json:"lastSuccessfulIngestionAt"`
}

func metaHandler(provider MetadataProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "metadata provider is not configured",
			})
			return
		}

		metadata, err := provider.GetMetadata(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load metadata",
			})
			return
		}

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
}

func listingDetailHandler(provider ListingProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "listing provider is not configured",
			})
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "listing not found",
			})
			return
		}

		listing, err := provider.GetListing(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "listing not found",
				})
				return
			}

			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load listing",
			})
			return
		}

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

func mapListingsHandler(provider MapProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, err := parseMapQuery(r.URL.Query())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}

		if provider == nil {
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

		if query.Zoom < 14 {
			clusters, err := provider.ListMapClusters(r.Context(), storeQuery)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "failed to load map clusters",
				})
				return
			}

			writeJSON(w, http.StatusOK, mapClustersFeatureCollection(clusters))
			return
		}

		listings, err := provider.ListMapListings(r.Context(), storeQuery)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to load map listings",
			})
			return
		}

		writeJSON(w, http.StatusOK, mapListingsFeatureCollection(listings))
	}
}

func emptyFeatureCollection() geoJSONFeatureCollection {
	return geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
	}
}

func mapListingsFeatureCollection(listings []store.MapListing) geoJSONFeatureCollection {
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
