package httpapi

import (
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

const maxMapSearchLength = 120
const maxMapPropertyTypeLength = 120

type bbox struct {
	West  float64
	South float64
	East  float64
	North float64
}

type mapQuery struct {
	BBox         bbox
	Zoom         int
	Search       string
	PropertyType string
}

type statsQuery struct {
	BBox         bbox
	Search       string
	PropertyType string
}

func parseMapQuery(values url.Values) (mapQuery, error) {
	slog.Debug("function entry", "function", "httpapi.parseMapQuery")
	defer slog.Debug("function exit", "function", "httpapi.parseMapQuery")

	parsedBBox, err := parseBBox(values.Get("bbox"))
	if err != nil {
		slog.Warn("map query validation failed", "field", "bbox", "error", err)
		return mapQuery{}, err
	}

	zoom, err := parseZoom(values.Get("zoom"))
	if err != nil {
		slog.Warn("map query validation failed", "field", "zoom", "error", err)
		return mapQuery{}, err
	}

	search, propertyType, err := parseMapFilters(values)
	if err != nil {
		slog.Warn("map query validation failed", "field", "filters", "error", err)
		return mapQuery{}, err
	}

	slog.Debug("map query parsed", "zoom", zoom, "search_present", search != "", "property_type_present", propertyType != "")
	return mapQuery{
		BBox:         parsedBBox,
		Zoom:         zoom,
		Search:       search,
		PropertyType: propertyType,
	}, nil
}

func parseStatsQuery(values url.Values) (statsQuery, error) {
	slog.Debug("function entry", "function", "httpapi.parseStatsQuery")
	defer slog.Debug("function exit", "function", "httpapi.parseStatsQuery")

	parsedBBox, err := parseBBox(values.Get("bbox"))
	if err != nil {
		slog.Warn("stats query validation failed", "field", "bbox", "error", err)
		return statsQuery{}, err
	}

	search, propertyType, err := parseMapFilters(values)
	if err != nil {
		slog.Warn("stats query validation failed", "field", "filters", "error", err)
		return statsQuery{}, err
	}

	slog.Debug("stats query parsed", "search_present", search != "", "property_type_present", propertyType != "")
	return statsQuery{
		BBox:         parsedBBox,
		Search:       search,
		PropertyType: propertyType,
	}, nil
}

func parseMapFilters(values url.Values) (string, string, error) {
	slog.Debug("function entry", "function", "httpapi.parseMapFilters")
	defer slog.Debug("function exit", "function", "httpapi.parseMapFilters")

	search := strings.TrimSpace(values.Get("q"))
	if len(search) > maxMapSearchLength {
		slog.Warn("map filter validation failed", "field", "q", "length", len(search), "max_length", maxMapSearchLength)
		return "", "", fmt.Errorf("q must be %d characters or fewer", maxMapSearchLength)
	}

	propertyType := strings.TrimSpace(values.Get("property_type"))
	if len(propertyType) > maxMapPropertyTypeLength {
		slog.Warn("map filter validation failed", "field", "property_type", "length", len(propertyType), "max_length", maxMapPropertyTypeLength)
		return "", "", fmt.Errorf("property_type must be %d characters or fewer", maxMapPropertyTypeLength)
	}

	slog.Debug("map filters parsed", "search_present", search != "", "property_type_present", propertyType != "")
	return search, propertyType, nil
}

func parseBBox(value string) (bbox, error) {
	slog.Debug("function entry", "function", "httpapi.parseBBox", "empty", strings.TrimSpace(value) == "")
	defer slog.Debug("function exit", "function", "httpapi.parseBBox")

	if strings.TrimSpace(value) == "" {
		slog.Warn("bbox validation failed", "reason", "missing")
		return bbox{}, fmt.Errorf("bbox is required")
	}

	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		slog.Warn("bbox validation failed", "reason", "invalid_part_count", "count", len(parts))
		return bbox{}, fmt.Errorf("bbox must contain west,south,east,north")
	}

	coordinates := make([]float64, 4)
	for i, part := range parts {
		coordinate, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			slog.Warn("bbox validation failed", "reason", "invalid_coordinate", "index", i)
			return bbox{}, fmt.Errorf("bbox contains an invalid coordinate")
		}
		coordinates[i] = coordinate
	}

	parsed := bbox{
		West:  coordinates[0],
		South: coordinates[1],
		East:  coordinates[2],
		North: coordinates[3],
	}

	if parsed.West < -180 || parsed.West > 180 || parsed.East < -180 || parsed.East > 180 {
		slog.Warn("bbox validation failed", "reason", "longitude_out_of_range", "west", parsed.West, "east", parsed.East)
		return bbox{}, fmt.Errorf("bbox longitude values must be between -180 and 180")
	}
	if parsed.South < -90 || parsed.South > 90 || parsed.North < -90 || parsed.North > 90 {
		slog.Warn("bbox validation failed", "reason", "latitude_out_of_range", "south", parsed.South, "north", parsed.North)
		return bbox{}, fmt.Errorf("bbox latitude values must be between -90 and 90")
	}
	if parsed.West >= parsed.East {
		slog.Warn("bbox validation failed", "reason", "west_not_less_than_east", "west", parsed.West, "east", parsed.East)
		return bbox{}, fmt.Errorf("bbox west must be less than east")
	}
	if parsed.South >= parsed.North {
		slog.Warn("bbox validation failed", "reason", "south_not_less_than_north", "south", parsed.South, "north", parsed.North)
		return bbox{}, fmt.Errorf("bbox south must be less than north")
	}

	slog.Debug("bbox parsed", "west", parsed.West, "south", parsed.South, "east", parsed.East, "north", parsed.North)
	return parsed, nil
}

func parseZoom(value string) (int, error) {
	slog.Debug("function entry", "function", "httpapi.parseZoom", "empty", strings.TrimSpace(value) == "")
	defer slog.Debug("function exit", "function", "httpapi.parseZoom")

	if strings.TrimSpace(value) == "" {
		slog.Warn("zoom validation failed", "reason", "missing")
		return 0, fmt.Errorf("zoom is required")
	}

	zoom, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		slog.Warn("zoom validation failed", "reason", "not_integer")
		return 0, fmt.Errorf("zoom must be an integer")
	}
	if zoom < 0 || zoom > 22 {
		slog.Warn("zoom validation failed", "reason", "out_of_range", "zoom", zoom)
		return 0, fmt.Errorf("zoom must be between 0 and 22")
	}

	slog.Debug("zoom parsed", "zoom", zoom)
	return zoom, nil
}
