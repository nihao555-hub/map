package web

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ErrPlacesNotFound is returned by GetPlaces when the job's CSV output does not
// exist. Callers use it to distinguish a missing job (404) from other errors.
var ErrPlacesNotFound = errors.New("places not found")

// Place is a single result row extracted from a job's CSV output for the UI table/map.
type Place struct {
	Title                 string  `json:"title"`
	Category              string  `json:"category"`
	Address               string  `json:"address"`
	CompleteAddress       string  `json:"complete_address"`
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	Link                  string  `json:"link"`
	Phone                 string  `json:"phone"`
	Website               string  `json:"website"`
	ReviewRating          float64 `json:"review_rating"`
	ReviewCount           int     `json:"review_count"`
	ReviewsPerRating      string  `json:"reviews_per_rating"`
	Emails                string  `json:"emails"`
	WhatsApp              string  `json:"whatsapp"`
	Facebook              string  `json:"facebook"`
	Instagram             string  `json:"instagram"`
	LinkedIn              string  `json:"linkedin"`
	Twitter               string  `json:"twitter"`
	TikTok                string  `json:"tiktok"`
	YouTube               string  `json:"youtube"`
	Telegram              string  `json:"telegram"`
	Pinterest             string  `json:"pinterest"`
	Status                string  `json:"status"`
	OpenHours             string  `json:"open_hours"`
	PopularTimes          string  `json:"popular_times"`
	PriceRange            string  `json:"price_range"`
	Descriptions          string  `json:"descriptions"`
	About                 string  `json:"about"`
	Menu                  string  `json:"menu"`
	Owner                 string  `json:"owner"`
	Images                string  `json:"images"`
	Thumbnail             string  `json:"thumbnail"`
	ReviewsLink           string  `json:"reviews_link"`
	UserReviews           string  `json:"user_reviews"`
	UserReviewsExtended   string  `json:"user_reviews_extended"`
	PlusCode              string  `json:"plus_code"`
	Timezone              string  `json:"timezone"`
	CreditCardsAccepted   string  `json:"credit_cards_accepted"`
	Reservations          string  `json:"reservations"`
	OrderOnline           string  `json:"order_online"`
	StreetViewURL         string  `json:"street_view_url"`
	PlaceID               string  `json:"place_id"`
	Cid                   string  `json:"cid"`
	DataID                string  `json:"data_id"`
}

// GetPlaces locates the job's CSV output and parses it into mappable places.
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

// parsePlaces reads scraped results from a CSV stream and returns the places
// that have valid coordinates. Columns are resolved by header name so the
// parser tolerates reordering; the names mirror gmaps.Entry.CsvHeaders().
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

		lat, errLat := strconv.ParseFloat(get(row, "latitude"), 64)
		lon, errLon := strconv.ParseFloat(get(row, "longitude"), 64)

		if errLat != nil || errLon != nil {
			continue
		}

		if !finite(lat) || !finite(lon) {
			continue
		}

		if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			continue
		}

		if lat == 0 && lon == 0 {
			continue
		}

		rating, _ := strconv.ParseFloat(get(row, "review_rating"), 64)
		if !finite(rating) {
			rating = 0
		}

		reviewCount, _ := strconv.Atoi(get(row, "review_count"))

		places = append(places, Place{
			Title:               get(row, "title"),
			Category:            get(row, "category"),
			Address:             get(row, "address"),
			CompleteAddress:     get(row, "complete_address"),
			Latitude:            lat,
			Longitude:           lon,
			Link:                get(row, "link"),
			Phone:               get(row, "phone"),
			Website:             get(row, "website"),
			ReviewRating:        rating,
			ReviewCount:         reviewCount,
			ReviewsPerRating:    get(row, "reviews_per_rating"),
			Emails:              get(row, "emails"),
			WhatsApp:            get(row, "whatsapp"),
			Facebook:            get(row, "facebook"),
			Instagram:           get(row, "instagram"),
			LinkedIn:            get(row, "linkedin"),
			Twitter:             get(row, "twitter"),
			TikTok:              get(row, "tiktok"),
			YouTube:             get(row, "youtube"),
			Telegram:            get(row, "telegram"),
			Pinterest:           get(row, "pinterest"),
			Status:              get(row, "status"),
			OpenHours:           get(row, "open_hours"),
			PopularTimes:        get(row, "popular_times"),
			PriceRange:          get(row, "price_range"),
			Descriptions:        get(row, "descriptions"),
			About:               get(row, "about"),
			Menu:                get(row, "menu"),
			Owner:               get(row, "owner"),
			Images:              get(row, "images"),
			Thumbnail:           get(row, "thumbnail"),
			ReviewsLink:         get(row, "reviews_link"),
			UserReviews:         get(row, "user_reviews"),
			UserReviewsExtended: get(row, "user_reviews_extended"),
			PlusCode:            get(row, "plus_code"),
			Timezone:            get(row, "timezone"),
			CreditCardsAccepted: get(row, "credit_cards_accepted"),
			Reservations:        get(row, "reservations"),
			OrderOnline:         get(row, "order_online"),
			StreetViewURL:       get(row, "street_view_url"),
			PlaceID:             get(row, "place_id"),
			Cid:                 get(row, "cid"),
			DataID:              get(row, "data_id"),
		})
	}

	// 获客优先：WhatsApp > 邮箱 > 电话；同档再按评分/评论数
	sort.SliceStable(places, func(i, j int) bool {
		si, sj := contactScore(places[i]), contactScore(places[j])
		if si != sj {
			return si > sj
		}
		if places[i].ReviewRating != places[j].ReviewRating {
			return places[i].ReviewRating > places[j].ReviewRating
		}

		return places[i].ReviewCount > places[j].ReviewCount
	})

	return places, nil
}

// contactScore WhatsApp > 邮箱 > 电话（印尼等市场更看即时通讯）
func contactScore(p Place) int {
	score := 0
	if strings.TrimSpace(p.WhatsApp) != "" {
		score += 100
	}
	if strings.TrimSpace(p.Emails) != "" {
		score += 10
	}
	if strings.TrimSpace(p.Phone) != "" {
		score += 1
	}

	return score
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
