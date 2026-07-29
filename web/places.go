package web

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// ErrPlacesNotFound is returned by GetPlaces when the job's CSV output does not
// exist. Callers use it to distinguish a missing job (404) from other errors.
var ErrPlacesNotFound = errors.New("places not found")

// Place is a single result extracted from a job's CSV output.
type Place struct {
	Title            string  `json:"title"`
	Link             string  `json:"link"`
	Address          string  `json:"address"`
	Category         string  `json:"category"`
	Phone            string  `json:"phone"`
	Email            string  `json:"email"`
	Website          string  `json:"website"`
	OpenHours        string  `json:"open_hours"`
	CompleteAddress  string  `json:"complete_address"`
	PriceRange       string  `json:"price_range"`
	Descriptions     string  `json:"descriptions"`
	Thumbnail        string  `json:"thumbnail"`
	Timezone         string  `json:"timezone"`
	PlusCode         string  `json:"plus_code"`
	ReviewsPerRating string  `json:"reviews_per_rating"`
	Latitude         string  `json:"latitude"`
	Longitude        string  `json:"longitude"`
	Images           string  `json:"images"`
	Reservations     string  `json:"reservations"`
	OrderOnline      string  `json:"order_online"`
	Menu             string  `json:"menu"`
	ReviewRating     float64 `json:"review_rating"`
	ReviewCount      int     `json:"review_count"`
}

// GetPlaces locates the job's CSV output and parses it into places.
// In web mode each job writes exactly one {id}.csv, so that file is the single
// source of truth for the map.
func (s *Service) GetPlaces(_ context.Context, id string) ([]Place, error) {
	path, err := s.csvPath(id)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("csv file not found for job %s: %w", id, ErrPlacesNotFound)
		}

		return nil, err
	}

	defer func() {
		_ = f.Close()
	}()

	return parsePlaces(f)
}

// parsePlaces reads scraped results from a CSV stream. Columns are resolved by
// header name so the parser tolerates reordering.
func parsePlaces(r io.Reader) ([]Place, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return []Place{}, nil
		}

		return nil, err
	}

	col := make(map[string]int, len(header))
	for i, name := range header {
		col[name] = i
	}

	get := func(row []string, name string) string {
		idx, ok := col[name]
		if !ok || idx >= len(row) {
			return ""
		}

		return row[idx]
	}

	places := []Place{}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		rating, _ := strconv.ParseFloat(get(row, "review_rating"), 64)
		if !finite(rating) {
			rating = 0
		}

		places = append(places, Place{
			Title:            get(row, "title"),
			Link:             get(row, "link"),
			Address:          get(row, "address"),
			Category:         parseMultiValue(get(row, "category")),
			Phone:            get(row, "phone"),
			Email:            parseMultiValue(get(row, "emails")),
			Website:          get(row, "website"),
			OpenHours:        formatOpenHours(get(row, "open_hours")),
			CompleteAddress:  formatCompleteAddress(get(row, "complete_address")),
			PriceRange:       get(row, "price_range"),
			Descriptions:     formatDescription(get(row, "descriptions"), get(row, "about")),
			Thumbnail:        get(row, "thumbnail"),
			Timezone:         get(row, "timezone"),
			PlusCode:         get(row, "plus_code"),
			ReviewsPerRating: formatReviewDistribution(get(row, "reviews_per_rating")),
			Latitude:         get(row, "latitude"),
			Longitude:        get(row, "longitude"),
			Images:           parseImages(get(row, "images")),
			Reservations:     formatLinkValue(get(row, "reservations")),
			OrderOnline:      formatLinkValue(get(row, "order_online")),
			Menu:             formatLinkValue(get(row, "menu")),
			ReviewRating:     rating,
			ReviewCount:      parseInt(get(row, "review_count")),
		})
	}

	return places, nil
}

func parseMultiValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var values []string
	if json.Unmarshal([]byte(value), &values) == nil {
		return strings.Join(values, ", ")
	}

	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	}), ", ")
}

func parseDisplayValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return ""
	}

	if json.Valid([]byte(value)) {
		return ""
	}

	return value
}

func formatDescription(descriptions, about string) string {
	if parsed := parseDisplayValue(descriptions); parsed != "" {
		return parsed
	}

	return formatAbout(about)
}

func parseImages(value string) string {
	var images []struct {
		Image string `json:"image"`
	}

	if json.Unmarshal([]byte(value), &images) == nil {
		for _, image := range images {
			if image.Image != "" {
				return image.Image
			}
		}
	}

	return parseMultiValue(value)
}

func formatOpenHours(value string) string {
	decoder := json.NewDecoder(strings.NewReader(value))

	token, err := decoder.Token()
	if err != nil {
		return ""
	}

	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return ""
	}

	var entries []string

	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return ""
		}

		var periods []string
		if err := decoder.Decode(&periods); err != nil {
			return ""
		}

		keyString, ok := key.(string)
		if !ok {
			return ""
		}

		if len(periods) > 0 {
			entries = append(entries, keyString+" "+strings.Join(periods, " / "))
		}
	}

	return strings.Join(entries, " · ")
}

func formatCompleteAddress(value string) string {
	var parts map[string]string
	if json.Unmarshal([]byte(value), &parts) != nil {
		return ""
	}

	street := strings.TrimSpace(parts["street"])
	if street != "" {
		return street
	}

	ordered := []string{"borough", "city", "state", "postal_code", "country"}
	result := make([]string, 0, len(ordered))

	for _, key := range ordered {
		part := strings.TrimSpace(parts[key])
		if part == "" || part == "CN" || containsPart(result, part) {
			continue
		}

		result = append(result, part)
	}

	return strings.Join(result, " ")
}

func containsPart(parts []string, value string) bool {
	for _, part := range parts {
		if strings.Contains(part, value) || strings.Contains(value, part) {
			return true
		}
	}

	return false
}

func formatReviewDistribution(value string) string {
	var ratings map[string]int
	if json.Unmarshal([]byte(value), &ratings) != nil {
		return ""
	}

	result := make([]string, 0, 5)

	for rating := 5; rating >= 1; rating-- {
		if count := ratings[strconv.Itoa(rating)]; count > 0 {
			result = append(result, fmt.Sprintf("%d★ %d", rating, count))
		}
	}

	return strings.Join(result, " · ")
}

func formatAbout(value string) string {
	var groups []struct {
		Name    string `json:"name"`
		Options []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"options"`
	}

	if json.Unmarshal([]byte(value), &groups) != nil {
		return ""
	}

	var result []string

	for _, group := range groups {
		var options []string

		for _, option := range group.Options {
			if option.Enabled && option.Name != "" {
				options = append(options, option.Name)
			}
		}

		if group.Name != "" && len(options) > 0 {
			result = append(result, group.Name+": "+strings.Join(options, "/"))
		}
	}

	return strings.Join(result, " · ")
}

func formatLinkValue(value string) string {
	var link struct {
		Link string `json:"link"`
	}

	if json.Unmarshal([]byte(value), &link) != nil {
		return ""
	}

	return strings.TrimSpace(link.Link)
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(value)

	return parsed
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
