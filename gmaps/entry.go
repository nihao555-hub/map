package gmaps

import (
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"math"
	"net/url"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/enrich"
)

var panoidRegex = regexp.MustCompile(`panoid=([^&]+)`)

type Image struct {
	Title string `json:"title"`
	Image string `json:"image"`
}

type LinkSource struct {
	Link   string `json:"link"`
	Source string `json:"source"`
}

type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Link string `json:"link"`
}

type Address struct {
	Borough    string `json:"borough"`
	Street     string `json:"street"`
	City       string `json:"city"`
	PostalCode string `json:"postal_code"`
	State      string `json:"state"`
	Country    string `json:"country"`
}

type Option struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Values  []string `json:"values,omitempty"`
}

type About struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Options []Option `json:"options"`
}

type Review struct {
	Name           string
	ProfilePicture string
	Rating         int
	Description    string
	Images         []string
	When           string

	ReviewID            string  `json:"review_id"`
	Source              string  `json:"source"`
	RatingScale         int     `json:"rating_scale"`
	RatingFloat         float64 `json:"rating_float"`
	AuthorURL           string  `json:"author_url"`
	PostedAtUnixMicros  int64   `json:"posted_at_unix_micros"`
	UpdatedAtUnixMicros int64   `json:"updated_at_unix_micros"`
	Language            string  `json:"language"`
	TranslatedLang      string  `json:"translated_lang"`
	TextOriginal        string  `json:"text_original"`
	TextTranslated      string  `json:"text_translated"`

	ReplyText                string     `json:"reply_text,omitempty"`
	ReplyTextOriginal        string     `json:"reply_text_original,omitempty"`
	ReplyLanguage            string     `json:"reply_language,omitempty"`
	ReplyTranslatedLang      string     `json:"reply_translated_lang,omitempty"`
	ReplyPostedAtUnixMicros  int64      `json:"reply_posted_at_unix_micros,omitempty"`
	ReplyUpdatedAtUnixMicros int64      `json:"reply_updated_at_unix_micros,omitempty"`
	PublishedAt              *time.Time `json:"published_at,omitempty"`
}

const reviewPublishedAtFutureSkew = 24 * time.Hour

var earliestReviewPublishedAt = time.Date(2007, time.January, 1, 0, 0, 0, 0, time.UTC)

type Entry struct {
	ID         string              `json:"input_id"`
	Link       string              `json:"link"`
	Cid        string              `json:"cid"`
	Title      string              `json:"title"`
	Categories []string            `json:"categories"`
	Category   string              `json:"category"`
	Address    string              `json:"address"`
	OpenHours  map[string][]string `json:"open_hours"`
	// PopularTImes is a map with keys the days of the week
	// and value is a map with key the hour and value the traffic in that time
	PopularTimes     map[string]map[int]int `json:"popular_times"`
	WebSite          string                 `json:"web_site"`
	Phone            string                 `json:"phone"`
	PlusCode         string                 `json:"plus_code"`
	ReviewCount      int                    `json:"review_count"`
	ReviewRating     float64                `json:"review_rating"`
	ReviewsPerRating map[int]int            `json:"reviews_per_rating"`
	Latitude         float64                `json:"latitude"`
	// Longtitude holds the longitude. The struct field and the legacy JSON
	// key are misspelled ("longtitude"); MarshalJSON also emits the correctly
	// spelled "longitude" key, and UnmarshalJSON accepts either. The field
	// name is kept for backwards compatibility with existing imports.
	Longtitude          float64      `json:"longtitude"`
	Status              string       `json:"status"`
	Description         string       `json:"description"`
	ReviewsLink         string       `json:"reviews_link"`
	Thumbnail           string       `json:"thumbnail"`
	Timezone            string       `json:"timezone"`
	PriceRange          string       `json:"price_range"`
	DataID              string       `json:"data_id"`
	StreetViewURL       string       `json:"street_view_url"`
	PlaceID             string       `json:"place_id"`
	Images              []Image      `json:"images"`
	Reservations        []LinkSource `json:"reservations"`
	OrderOnline         []LinkSource `json:"order_online"`
	Menu                LinkSource   `json:"menu"`
	Owner               Owner        `json:"owner"`
	CompleteAddress     Address      `json:"complete_address"`
	CreditCardsAccepted []string     `json:"credit_cards_accepted"`
	About               []About      `json:"about"`
	UserReviews         []Review     `json:"user_reviews"`
	UserReviewsExtended []Review     `json:"user_reviews_extended"`
	Emails              []string     `json:"emails"`
	// CompanyProfile holds the background-research result built from the
	// business's own website. It is nil unless company research is enabled.
	CompanyProfile *enrich.CompanyProfile `json:"company_profile,omitempty"`
}

// entryAlias is used inside Marshal/UnmarshalJSON to avoid infinite recursion
// while still benefiting from the struct's json tags for every other field.
type entryAlias Entry

// MarshalJSON emits both the legacy "longtitude" key (preserved for backwards
// compatibility) and the correctly spelled "longitude" key so downstream
// consumers can migrate without a flag day.
//
//nolint:gocritic // value receiver preserves json.Marshaler behavior for Entry values.
func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Longitude float64 `json:"longitude"`
		entryAlias
	}{
		Longitude:  e.Longtitude,
		entryAlias: entryAlias(e),
	})
}

// UnmarshalJSON accepts either "longtitude" (legacy) or "longitude" (preferred)
// as the longitude key. "longtitude" wins when both are present so existing
// data files keep round-tripping byte-identical.
func (e *Entry) UnmarshalJSON(data []byte) error {
	aux := struct {
		Longitude *float64 `json:"longitude"`
		*entryAlias
	}{
		entryAlias: (*entryAlias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if e.Longtitude == 0 && aux.Longitude != nil {
		e.Longtitude = *aux.Longitude
	}

	return nil
}

func (e *Entry) haversineDistance(lat, lon float64) float64 {
	const R = 6371e3 // earth radius in meters

	clat := lat * math.Pi / 180
	clon := lon * math.Pi / 180

	elat := e.Latitude * math.Pi / 180
	elon := e.Longtitude * math.Pi / 180

	dlat := elat - clat
	dlon := elon - clon

	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(clat)*math.Cos(elat)*
			math.Sin(dlon/2)*math.Sin(dlon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

func (e *Entry) isWithinRadius(lat, lon, radius float64) bool {
	distance := e.haversineDistance(lat, lon)

	return distance <= radius
}

func (e *Entry) IsWebsiteValidForEmail() bool {
	if e.WebSite == "" {
		return false
	}

	needles := []string{
		"facebook",
		"instragram",
		"twitter",
	}

	for i := range needles {
		if strings.Contains(e.WebSite, needles[i]) {
			return false
		}
	}

	return true
}

func (e *Entry) Validate() error {
	if e.Title == "" {
		return fmt.Errorf("title is empty")
	}

	if e.Category == "" {
		return fmt.Errorf("category is empty")
	}

	return nil
}

func (e *Entry) CsvHeaders() []string {
	return []string{
		"input_id",
		"link",
		"title",
		"category",
		"address",
		"open_hours",
		"popular_times",
		"website",
		"phone",
		"plus_code",
		"review_count",
		"review_rating",
		"reviews_per_rating",
		"latitude",
		"longitude",
		"cid",
		"status",
		"descriptions",
		"reviews_link",
		"thumbnail",
		"timezone",
		"price_range",
		"data_id",
		"street_view_url",
		"place_id",
		"images",
		"reservations",
		"order_online",
		"menu",
		"owner",
		"complete_address",
		"credit_cards_accepted",
		"about",
		"user_reviews",
		"user_reviews_extended",
		"emails",
	}
}

// ResearchCsvHeaders are the background-research columns. They are appended to
// CsvHeaders only when company research is enabled, so runs without it keep
// their existing column layout.
func ResearchCsvHeaders() []string {
	return []string{
		"research_completeness",
		"best_email",
		"role_emails",
		"contact_people",
		"linkedin",
		"socials",
		"whatsapp",
		"trade_roles",
		"certifications",
		"registration_ids",
		"founded_year",
		"employee_range",
		"markets",
		"site_languages",
		"web_platform",
		"tech_stack",
		"mail_provider",
		"domain_age_days",
		"lei",
		"legal_entity",
		"ultimate_parent",
		"company_description",
		"ai_report",
		"company_profile",
	}
}

// ResearchCsvRow returns the background-research values in the same order as
// ResearchCsvHeaders. Every cell is empty when no profile was collected.
func (e *Entry) ResearchCsvRow() []string {
	profile := e.CompanyProfile
	if profile == nil {
		return make([]string, len(ResearchCsvHeaders()))
	}

	return []string{
		strconv.Itoa(profile.Completeness),
		e.BestEmail(),
		strings.Join(e.emailsOfKind(enrich.EmailKindRole, enrich.EmailKindPersonal), ", "),
		formatPeople(profile.People),
		firstSocialURL(profile.Socials, enrich.NetworkLinkedIn),
		formatSocials(profile.Socials),
		formatWhatsApp(profile.Phones),
		strings.Join(profile.TradeRoles, ", "),
		strings.Join(profile.Certifications, ", "),
		formatRegistrationIDs(profile.RegistrationIDs),
		formatYear(profile.FoundedYear),
		profile.EmployeeRange,
		strings.Join(profile.Markets, ", "),
		strings.Join(profile.Languages, ", "),
		profile.Platform,
		strings.Join(profile.TechStack, ", "),
		profile.MailProvider,
		formatYear(profile.DomainAgeDays),
		profile.LEI,
		formatLegalEntity(profile.LegalEntity),
		formatLegalEntity(profile.UltimateParent),
		profile.Description,
		profile.AIReport,
		stringify(profile),
	}
}

// BestEmail returns the address most likely to reach a decision maker, or the
// first known address when no profile was built.
func (e *Entry) BestEmail() string {
	if e.CompanyProfile != nil && len(e.CompanyProfile.Emails) > 0 {
		// Finalize sorted the list with the most actionable address first.
		return e.CompanyProfile.Emails[0].Address
	}

	if len(e.Emails) > 0 {
		return e.Emails[0]
	}

	return ""
}

func (e *Entry) emailsOfKind(kinds ...enrich.EmailKind) []string {
	if e.CompanyProfile == nil {
		return nil
	}

	wanted := make(map[enrich.EmailKind]bool, len(kinds))
	for _, kind := range kinds {
		wanted[kind] = true
	}

	out := make([]string, 0, len(e.CompanyProfile.Emails))

	for i := range e.CompanyProfile.Emails {
		if wanted[e.CompanyProfile.Emails[i].Kind] {
			out = append(out, e.CompanyProfile.Emails[i].Address)
		}
	}

	return out
}

func formatPeople(people []enrich.Person) string {
	if len(people) == 0 {
		return ""
	}

	parts := make([]string, 0, len(people))

	for i := range people {
		part := people[i].Name

		if people[i].Title != "" {
			part += " (" + people[i].Title + ")"
		}

		if people[i].Email != "" {
			part += " <" + people[i].Email + ">"
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, "; ")
}

func formatSocials(socials []enrich.Social) string {
	if len(socials) == 0 {
		return ""
	}

	parts := make([]string, 0, len(socials))
	for i := range socials {
		parts = append(parts, socials[i].Network+": "+socials[i].URL)
	}

	return strings.Join(parts, "; ")
}

func firstSocialURL(socials []enrich.Social, network string) string {
	// Company pages are more useful than individual member profiles, so a
	// company URL wins even when a person profile was seen first.
	fallback := ""

	for i := range socials {
		if socials[i].Network != network {
			continue
		}

		if !socials[i].IsPersonProfile {
			return socials[i].URL
		}

		if fallback == "" {
			fallback = socials[i].URL
		}
	}

	return fallback
}

func formatWhatsApp(phones []enrich.Phone) string {
	parts := make([]string, 0, len(phones))

	for i := range phones {
		if !phones[i].WhatsApp {
			continue
		}

		number := phones[i].E164
		if number == "" {
			number = phones[i].Raw
		}

		parts = append(parts, number)
	}

	return strings.Join(parts, ", ")
}

func formatRegistrationIDs(ids []enrich.RegistrationID) string {
	if len(ids) == 0 {
		return ""
	}

	parts := make([]string, 0, len(ids))
	for i := range ids {
		parts = append(parts, strings.ToUpper(ids[i].Kind)+": "+ids[i].Value)
	}

	return strings.Join(parts, "; ")
}

func formatYear(year int) string {
	if year == 0 {
		return ""
	}

	return strconv.Itoa(year)
}

func formatLegalEntity(entity *enrich.LegalEntity) string {
	if entity == nil {
		return ""
	}

	parts := make([]string, 0, 4)
	if entity.LegalName != "" {
		parts = append(parts, entity.LegalName)
	}

	if entity.LEI != "" {
		parts = append(parts, "LEI "+entity.LEI)
	}

	if entity.Jurisdiction != "" {
		parts = append(parts, entity.Jurisdiction)
	}

	if entity.Status != "" {
		parts = append(parts, entity.Status)
	}

	return strings.Join(parts, " | ")
}

func (e *Entry) CsvRow() []string {
	return []string{
		e.ID,
		e.Link,
		e.Title,
		e.Category,
		e.Address,
		stringify(e.OpenHours),
		stringify(e.PopularTimes),
		e.WebSite,
		e.Phone,
		e.PlusCode,
		stringify(e.ReviewCount),
		stringify(e.ReviewRating),
		stringify(e.ReviewsPerRating),
		stringify(e.Latitude),
		stringify(e.Longtitude),
		e.Cid,
		e.Status,
		e.Description,
		e.ReviewsLink,
		e.Thumbnail,
		e.Timezone,
		e.PriceRange,
		e.DataID,
		e.StreetViewURL,
		e.PlaceID,
		stringify(e.Images),
		stringify(e.Reservations),
		stringify(e.OrderOnline),
		stringify(e.Menu),
		stringify(e.Owner),
		stringify(e.CompleteAddress),
		stringSliceToString(e.CreditCardsAccepted),
		stringify(e.About),
		stringify(e.UserReviews),
		stringify(e.UserReviewsExtended),
		stringSliceToString(e.Emails),
	}
}

func (e *Entry) AddExtraReviews(pages [][]byte) {
	if len(pages) == 0 {
		return
	}

	for _, page := range pages {
		reviews := extractReviews(page)
		if len(reviews) > 0 {
			e.UserReviewsExtended = append(e.UserReviewsExtended, reviews...)
		}
	}
}

func extractReviews(data []byte) []Review {
	// Skip the security prefix
	prefix := ")]}'\n"
	if len(data) >= len(prefix) && string(data[:len(prefix)]) == prefix {
		data = data[len(prefix):]
	} else if len(data) >= 4 && string(data[0:4]) == `)]}'` {
		data = data[4:]
	}

	var jd []any
	if err := json.Unmarshal(data, &jd); err != nil {
		log.Printf("DEBUG: Error unmarshalling RPC JSON: %v (data len: %d)", err, len(data))
		return nil
	}

	if len(jd) < 3 {
		log.Printf("DEBUG: RPC response has only %d elements, expected 3+", len(jd))
		return nil
	}

	reviewsI := getNthElementAndCast[[]any](jd, 2)
	if len(reviewsI) == 0 {
		// Try alternative indices - Google may have changed the structure
		reviewsI = getNthElementAndCast[[]any](jd, 0)
	}

	return parseReviews(reviewsI)
}

//nolint:gomnd // it's ok, I need the indexes
func EntryFromJSON(raw []byte, reviewCountOnly ...bool) (entry Entry, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic: %v stack: %s", r, debug.Stack())

			return
		}
	}()

	onlyReviewCount := false

	if len(reviewCountOnly) == 1 && reviewCountOnly[0] {
		onlyReviewCount = true
	}

	var jd []any
	if err := json.Unmarshal(raw, &jd); err != nil {
		return entry, err
	}

	if len(jd) < 7 {
		return entry, fmt.Errorf("invalid json")
	}

	darray, ok := jd[6].([]any)
	if !ok {
		return entry, fmt.Errorf("invalid json")
	}

	entry.ReviewCount = int(getNthElementAndCast[float64](darray, 4, 8))

	if onlyReviewCount {
		return entry, nil
	}

	entry.Link = getNthElementAndCast[string](darray, 27)
	entry.Title = getNthElementAndCast[string](darray, 11)

	categoriesI := getNthElementAndCast[[]any](darray, 13)

	entry.Categories = make([]string, len(categoriesI))
	for i := range categoriesI {
		entry.Categories[i], _ = categoriesI[i].(string)
	}

	if len(entry.Categories) > 0 {
		entry.Category = entry.Categories[0]
	}

	entry.Address = strings.TrimSpace(
		strings.TrimPrefix(getNthElementAndCast[string](darray, 18), entry.Title+","),
	)
	entry.OpenHours = getHours(darray)
	entry.PopularTimes = getPopularTimes(darray)
	entry.WebSite = extractActualURL(getNthElementAndCast[string](darray, 7, 0))
	entry.Phone = getNthElementAndCast[string](darray, 178, 0, 0)
	entry.PlusCode = getNthElementAndCast[string](darray, 183, 2, 2, 0)
	entry.ReviewRating = getNthElementAndCast[float64](darray, 4, 7)
	entry.Latitude = getNthElementAndCast[float64](darray, 9, 2)
	entry.Longtitude = getNthElementAndCast[float64](darray, 9, 3)
	entry.Cid = getNthElementAndCast[string](jd, 25, 3, 0, 13, 0, 0, 1)
	entry.Status = getNthElementAndCast[string](darray, 34, 4, 4)

	if entry.Status == "" {
		// [34][4][4] went dead ~2026-07; closed-state enum now at [88][0] ("CLOSED" / null)
		entry.Status = getNthElementAndCast[string](darray, 88, 0)
	}

	entry.Description = getNthElementAndCast[string](darray, 32, 1, 1)
	entry.ReviewsLink = getNthElementAndCast[string](darray, 4, 3, 0)
	entry.Thumbnail = getNthElementAndCast[string](darray, 72, 0, 1, 6, 0)
	entry.Timezone = getNthElementAndCast[string](darray, 30)
	entry.PriceRange = getNthElementAndCast[string](darray, 4, 2)
	entry.DataID = getNthElementAndCast[string](darray, 10)
	entry.PlaceID = getNthElementAndCast[string](darray, 78)

	items := getLinkSource(getLinkSourceParams{
		arr:    getNthElementAndCast[[]any](darray, 171, 0),
		link:   []int{3, 0, 6, 0},
		source: []int{2},
	})

	entry.Images = make([]Image, len(items))

	for i := range items {
		entry.Images[i] = Image{
			Title: items[i].Source,
			Image: items[i].Link,
		}
	}

	// Extract Street View URL from images
	entry.StreetViewURL = extractStreetViewURL(entry.Images)

	entry.Reservations = getLinkSource(getLinkSourceParams{
		arr:    getNthElementAndCast[[]any](darray, 46),
		link:   []int{0},
		source: []int{1},
	})

	orderOnlineI := getNthElementAndCast[[]any](darray, 75, 0, 1, 2)

	if len(orderOnlineI) == 0 {
		orderOnlineI = getNthElementAndCast[[]any](darray, 75, 0, 0, 2)
	}

	entry.OrderOnline = getLinkSource(getLinkSourceParams{
		arr:    orderOnlineI,
		link:   []int{1, 2, 0},
		source: []int{0, 0},
	})

	entry.Menu = LinkSource{
		Link:   getNthElementAndCast[string](darray, 38, 0),
		Source: getNthElementAndCast[string](darray, 38, 1),
	}

	entry.Owner = Owner{
		ID:   getNthElementAndCast[string](darray, 57, 2),
		Name: getNthElementAndCast[string](darray, 57, 1),
	}

	if entry.Owner.ID != "" {
		entry.Owner.Link = fmt.Sprintf("https://www.google.com/maps/contrib/%s", entry.Owner.ID)
	}

	entry.CompleteAddress = Address{
		Borough:    getNthElementAndCast[string](darray, 183, 1, 0),
		Street:     getNthElementAndCast[string](darray, 183, 1, 1),
		City:       getNthElementAndCast[string](darray, 183, 1, 3),
		PostalCode: getNthElementAndCast[string](darray, 183, 1, 4),
		State:      getNthElementAndCast[string](darray, 183, 1, 5),
		Country:    getNthElementAndCast[string](darray, 183, 1, 6),
	}

	aboutI := getNthElementAndCast[[]any](darray, 100, 1)

	for i := range aboutI {
		el := getNthElementAndCast[[]any](aboutI, i)
		about := About{
			ID:   getNthElementAndCast[string](el, 0),
			Name: getNthElementAndCast[string](el, 1),
		}

		optsI := getNthElementAndCast[[]any](el, 2)

		for j := range optsI {
			opt := Option{
				Enabled: (getNthElementAndCast[float64](optsI, j, 2, 1, 0, 0)) == 1,
				Name:    getNthElementAndCast[string](optsI, j, 1),
				Values:  getOptionValues(getNthElementAndCast[[]any](optsI, j)),
			}

			if opt.Name != "" {
				addOrMergeOption(&about.Options, opt)
			}

			if about.ID == "payments" && opt.Name == "Credit cards" && len(opt.Values) > 0 {
				entry.CreditCardsAccepted = mergeStringSlices(entry.CreditCardsAccepted, opt.Values)
			}
		}

		entry.About = append(entry.About, about)
	}

	entry.ReviewsPerRating = map[int]int{
		1: int(getNthElementAndCast[float64](darray, 175, 3, 0)),
		2: int(getNthElementAndCast[float64](darray, 175, 3, 1)),
		3: int(getNthElementAndCast[float64](darray, 175, 3, 2)),
		4: int(getNthElementAndCast[float64](darray, 175, 3, 3)),
		5: int(getNthElementAndCast[float64](darray, 175, 3, 4)),
	}

	// Parse inline reviews from the page data
	reviewsI := getNthElementAndCast[[]any](darray, 175, 9, 0, 0)
	if len(reviewsI) > 0 {
		entry.UserReviews = parseReviews(reviewsI)
	} else {
		// Try alternative location for reviews
		reviewsI = getNthElementAndCast[[]any](darray, 175, 9, 0)
		if len(reviewsI) > 0 {
			entry.UserReviews = parseReviews(reviewsI)
		} else {
			entry.UserReviews = make([]Review, 0)
		}
	}

	return entry, nil
}

func parseReviews(reviewsI []any) []Review {
	ans := make([]Review, 0, len(reviewsI))

	for i := range reviewsI {
		el := getNthElementAndCast[[]any](reviewsI, i, 0)
		if len(el) == 0 {
			// Try alternative structure
			el = getNthElementAndCast[[]any](reviewsI, i)
			if len(el) == 0 {
				continue
			}
		}

		review := Review{
			Name:           reviewAuthorName(el),
			ProfilePicture: reviewProfilePicture(el),
			When:           reviewRelativeDate(el),
			PublishedAt:    reviewPublishedAt(el),
			Rating:         reviewRating(el),
			Description:    reviewDescription(el),
		}

		// Extended metadata
		review.ReviewID = getNthElementAndCast[string](el, 0)
		review.PostedAtUnixMicros = int64(getNthElementAndCast[float64](el, 1, 2))
		review.UpdatedAtUnixMicros = int64(getNthElementAndCast[float64](el, 1, 3))
		review.AuthorURL = getNthElementAndCast[string](el, 1, 4, 2, 0)

		src := getNthElementAndCast[string](el, 1, 13, 0)
		if src == "" {
			src = "unknown"
		}

		review.Source = src

		scale := int(getNthElementAndCast[float64](el, 1, 13, 4))
		if scale == 0 {
			scale = 5
		}

		review.RatingScale = scale

		review.Language = getNthElementAndCast[string](el, 2, 14, 0)
		review.TranslatedLang = getNthElementAndCast[string](el, 2, 14, 1)
		review.TextOriginal = getNthElementAndCast[string](el, 2, 15, 0, 0)
		review.TextTranslated = getNthElementAndCast[string](el, 2, 15, 1, 0)

		r2 := getNthElementAndCast[[]any](el, 2)

		isAggregator := len(r2) > 0 && r2[0] == nil
		if isAggregator {
			review.RatingFloat = getNthElementAndCast[float64](el, 2, 8, 1)
		} else {
			review.RatingFloat = float64(review.Rating)
		}

		r3 := getNthElementAndCast[[]any](el, 3)
		if len(r3) >= 15 && r3[1] != nil {
			review.ReplyPostedAtUnixMicros = int64(getNthElementAndCast[float64](el, 3, 1))
			review.ReplyUpdatedAtUnixMicros = int64(getNthElementAndCast[float64](el, 3, 2))
			review.ReplyLanguage = getNthElementAndCast[string](el, 3, 13, 0)
			review.ReplyTranslatedLang = getNthElementAndCast[string](el, 3, 13, 1)
			review.ReplyTextOriginal = getNthElementAndCast[string](el, 3, 14, 0, 0)
			review.ReplyText = getNthElementAndCast[string](el, 3, 14, 1, 0)
		}

		if review.Name == "" {
			continue
		}

		// Extract user-contributed photo URLs for this review.
		// Structure: el[2][2] is the image list; each image's direct lh3 URL lives at [1][6][0].
		// The previous paths (e.g. [2][2][0][1][21][7]) landed on the imagery/report
		// "report this photo" URL, not the actual hosted image. See issue #240.
		imgs := getNthElementAndCast[[]any](el, 2, 2)
		for j := range imgs {
			url := getNthElementAndCast[string](imgs, j, 1, 6, 0)
			if url != "" {
				review.Images = append(review.Images, url)
			}
		}

		ans = append(ans, review)
	}

	return ans
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func reviewRelativeDate(el []any) string {
	return firstNonEmpty(
		getNthElementAndCast[string](el, 1, 6),
		getNthElementAndCast[string](el, 3, 3),
		getNthElementAndCast[string](el, 2, 1, 3, 8, 0),
	)
}

func reviewPublishedAt(el []any) *time.Time {
	timestampMicros := firstNonZero(
		getNthElementAndCast[float64](el, 1, 2),
		getNthElementAndCast[float64](el, 1, 3),
	)
	if timestampMicros == 0 {
		return nil
	}

	publishedAt := time.UnixMicro(int64(timestampMicros)).UTC()
	if publishedAt.Before(earliestReviewPublishedAt) {
		return nil
	}

	if publishedAt.After(time.Now().UTC().Add(reviewPublishedAtFutureSkew)) {
		return nil
	}

	return &publishedAt
}

func reviewProfilePicture(el []any) string {
	profilePic, err := decodeURL(getNthElementAndCast[string](el, 1, 4, 5, 1))
	if err == nil && profilePic != "" {
		return profilePic
	}

	return firstNonEmpty(
		getNthElementAndCast[string](el, 1, 2, 0),
		getNthElementAndCast[string](el, 0, 2, 0),
	)
}

func reviewAuthorName(el []any) string {
	return firstNonEmpty(
		getNthElementAndCast[string](el, 1, 4, 5, 0),
		getNthElementAndCast[string](el, 1, 4, 4),
		getNthElementAndCast[string](el, 0, 1),
	)
}

func reviewRating(el []any) int {
	return int(firstNonZero(
		getNthElementAndCast[float64](el, 2, 0, 0),
		getNthElementAndCast[float64](el, 2, 0),
		getNthElementAndCast[float64](el, 1, 0, 0),
	))
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}

	return 0
}

func reviewDescription(el []any) string {
	return firstNonEmpty(
		getNthElementAndCast[string](el, 2, 15, 0, 0),
		getNthElementAndCast[string](el, 2, 15, 0),
		getNthElementAndCast[string](el, 3, 0),
	)
}

type getLinkSourceParams struct {
	arr    []any
	source []int
	link   []int
}

func getLinkSource(params getLinkSourceParams) []LinkSource {
	var result []LinkSource

	for i := range params.arr {
		item := getNthElementAndCast[[]any](params.arr, i)

		el := LinkSource{
			Source: getNthElementAndCast[string](item, params.source...),
			Link:   getNthElementAndCast[string](item, params.link...),
		}
		if el.Link != "" && el.Source != "" {
			result = append(result, el)
		}
	}

	return result
}

//nolint:gomnd // it's ok, I need the indexes
func getHours(darray []any) map[string][]string {
	// Try new structure first (as of Nov 2025) - darray[203][0]
	items := getNthElementAndCast[[]any](darray, 203, 0)
	if len(items) == 0 {
		// Fall back to old structure - darray[34][1]
		items = getNthElementAndCast[[]any](darray, 34, 1)
	}

	hours := make(map[string][]string, len(items))

	for _, item := range items {
		itemArray, ok := item.([]any)
		if !ok {
			continue
		}

		// New structure: [0] = day name, [3] = time slots array
		day := getNthElementAndCast[string](itemArray, 0)
		if day == "" {
			continue
		}

		// Try new structure for times
		timeSlotsI := getNthElementAndCast[[]any](itemArray, 3)
		if len(timeSlotsI) > 0 {
			// New format: each slot is [formatted_string, [[hour, min], [hour, min]]]
			times := make([]string, 0, len(timeSlotsI))

			for _, slot := range timeSlotsI {
				slotArray, ok := slot.([]any)
				if !ok || len(slotArray) == 0 {
					continue
				}

				// Get the formatted time string (e.g., "11 am–1:30 pm")
				timeStr := getNthElementAndCast[string](slotArray, 0)
				if timeStr != "" {
					times = append(times, timeStr)
				}
			}

			if len(times) > 0 {
				hours[day] = times
			}
		} else {
			// Fall back to old structure: [1] = times array
			timesI := getNthElementAndCast[[]any](itemArray, 1)
			times := make([]string, 0, len(timesI))

			for i := range timesI {
				if timeStr, ok := timesI[i].(string); ok {
					times = append(times, timeStr)
				}
			}

			if len(times) > 0 {
				hours[day] = times
			}
		}
	}

	return hours
}

func getPopularTimes(darray []any) map[string]map[int]int {
	items := getNthElementAndCast[[]any](darray, 84, 0) //nolint:gomnd // it's ok, I need the indexes
	popularTimes := make(map[string]map[int]int, len(items))

	dayOfWeek := map[int]string{
		1: "Monday",
		2: "Tuesday",
		3: "Wednesday",
		4: "Thursday",
		5: "Friday",
		6: "Saturday",
		7: "Sunday",
	}

	for ii := range items {
		item, ok := items[ii].([]any)
		if !ok {
			return nil
		}

		day := int(getNthElementAndCast[float64](item, 0))

		timesI := getNthElementAndCast[[]any](item, 1)

		times := make(map[int]int, len(timesI))

		for i := range timesI {
			t, ok := timesI[i].([]any)
			if !ok {
				return nil
			}

			v, ok := t[1].(float64)
			if !ok {
				return nil
			}

			h, ok := t[0].(float64)
			if !ok {
				return nil
			}

			times[int(h)] = int(v)
		}

		popularTimes[dayOfWeek[day]] = times
	}

	return popularTimes
}

func getNthElementAndCast[T any](arr []any, indexes ...int) T {
	var (
		defaultVal T
		idx        int
	)

	if len(indexes) == 0 {
		return defaultVal
	}

	for len(indexes) > 1 {
		idx, indexes = indexes[0], indexes[1:]

		if idx >= len(arr) {
			return defaultVal
		}

		next := arr[idx]

		if next == nil {
			return defaultVal
		}

		var ok bool

		arr, ok = next.([]any)
		if !ok {
			return defaultVal
		}
	}

	if len(indexes) == 0 || len(arr) == 0 {
		return defaultVal
	}

	if indexes[0] >= len(arr) {
		return defaultVal
	}

	ans, ok := arr[indexes[0]].(T)
	if !ok {
		return defaultVal
	}

	return ans
}

func stringSliceToString(s []string) string {
	return strings.Join(s, ", ")
}

func addOrMergeOption(options *[]Option, opt Option) {
	for i := range *options {
		if (*options)[i].Name != opt.Name {
			continue
		}

		(*options)[i].Enabled = (*options)[i].Enabled || opt.Enabled
		(*options)[i].Values = mergeStringSlices((*options)[i].Values, opt.Values)

		return
	}

	*options = append(*options, opt)
}

func getOptionValues(opt []any) []string {
	valuesI := getNthElementAndCast[[]any](opt, 2, 4, 1, 0, 0)
	values := make([]string, 0, len(valuesI))

	for i := range valuesI {
		value := getNthElementAndCast[string](valuesI, i, 2)
		if value == "" {
			value = getNthElementAndCast[string](valuesI, i, 3)
		}

		if value != "" {
			values = append(values, value)
		}
	}

	return values
}

func mergeStringSlices(current, next []string) []string {
	for _, value := range next {
		if !slices.Contains(current, value) {
			current = append(current, value)
		}
	}

	return current
}

func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%f", val)
	case nil:
		return ""
	default:
		d, _ := json.Marshal(v)
		return string(d)
	}
}

// extractStreetViewURL finds the Street View image and extracts the panoid to create a proper URL
func extractStreetViewURL(images []Image) string {
	for _, img := range images {
		if strings.Contains(img.Title, "Street View") {
			matches := panoidRegex.FindStringSubmatch(img.Image)
			if len(matches) > 1 {
				return fmt.Sprintf("https://www.google.com/maps/@?api=1&map_action=pano&pano=%s", matches[1])
			}
		}
	}

	return ""
}

func decodeURL(url string) (string, error) {
	quoted := `"` + strings.ReplaceAll(url, `"`, `\"`) + `"`

	unquoted, err := strconv.Unquote(quoted)
	if err != nil {
		return "", fmt.Errorf("failed to decode URL: %v", err)
	}

	return unquoted, nil
}

func extractActualURL(googleURL string) string {
	if googleURL == "" || !strings.HasPrefix(googleURL, "/url?q=") {
		return googleURL
	}

	parsedURL, err := url.Parse(googleURL)
	if err != nil {
		return googleURL
	}

	actualURL := parsedURL.Query().Get("q")
	if actualURL == "" {
		return googleURL
	}

	return actualURL
}

type EntryWithDistance struct {
	Entry    *Entry
	Distance float64
}

func filterAndSortEntriesWithinRadius(entries []*Entry, lat, lon, radius float64) []*Entry {
	withinRadiusIterator := func(yield func(EntryWithDistance) bool) {
		for _, entry := range entries {
			distance := entry.haversineDistance(lat, lon)
			if distance <= radius {
				if !yield(EntryWithDistance{Entry: entry, Distance: distance}) {
					return
				}
			}
		}
	}

	entriesWithDistance := slices.Collect(iter.Seq[EntryWithDistance](withinRadiusIterator))

	slices.SortFunc(entriesWithDistance, func(a, b EntryWithDistance) int {
		switch {
		case a.Distance < b.Distance:
			return -1
		case a.Distance > b.Distance:
			return 1
		default:
			return 0
		}
	})

	resultIterator := func(yield func(*Entry) bool) {
		for _, e := range entriesWithDistance {
			if !yield(e.Entry) {
				return
			}
		}
	}

	return slices.Collect(iter.Seq[*Entry](resultIterator))
}
