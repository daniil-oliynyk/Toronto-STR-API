package httpapi

import (
	"fmt"
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

func parseMapQuery(values url.Values) (mapQuery, error) {
	parsedBBox, err := parseBBox(values.Get("bbox"))
	if err != nil {
		return mapQuery{}, err
	}

	zoom, err := parseZoom(values.Get("zoom"))
	if err != nil {
		return mapQuery{}, err
	}

	search := strings.TrimSpace(values.Get("q"))
	if len(search) > maxMapSearchLength {
		return mapQuery{}, fmt.Errorf("q must be %d characters or fewer", maxMapSearchLength)
	}

	propertyType := strings.TrimSpace(values.Get("property_type"))
	if len(propertyType) > maxMapPropertyTypeLength {
		return mapQuery{}, fmt.Errorf("property_type must be %d characters or fewer", maxMapPropertyTypeLength)
	}

	return mapQuery{
		BBox:         parsedBBox,
		Zoom:         zoom,
		Search:       search,
		PropertyType: propertyType,
	}, nil
}

func parseBBox(value string) (bbox, error) {
	if strings.TrimSpace(value) == "" {
		return bbox{}, fmt.Errorf("bbox is required")
	}

	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return bbox{}, fmt.Errorf("bbox must contain west,south,east,north")
	}

	coordinates := make([]float64, 4)
	for i, part := range parts {
		coordinate, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
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
		return bbox{}, fmt.Errorf("bbox longitude values must be between -180 and 180")
	}
	if parsed.South < -90 || parsed.South > 90 || parsed.North < -90 || parsed.North > 90 {
		return bbox{}, fmt.Errorf("bbox latitude values must be between -90 and 90")
	}
	if parsed.West >= parsed.East {
		return bbox{}, fmt.Errorf("bbox west must be less than east")
	}
	if parsed.South >= parsed.North {
		return bbox{}, fmt.Errorf("bbox south must be less than north")
	}

	return parsed, nil
}

func parseZoom(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("zoom is required")
	}

	zoom, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("zoom must be an integer")
	}
	if zoom < 0 || zoom > 22 {
		return 0, fmt.Errorf("zoom must be between 0 and 22")
	}

	return zoom, nil
}
