package placeref

import (
	"regexp"
	"strings"
	"unicode"
)

// Place is the minimal text fields needed for relevance filtering.
type Place struct {
	Title        string
	Category     string
	Address      string
	Descriptions string
	About        string
}

type domain int

const (
	domainGeneric domain = iota
	domainElectrical
	domainFood
	domainRetail
	domainHotel
	domainMedical
	domainEducation
	domainAuto
)

var electricalKeywordHints = []string{
	"panel listrik", "alat listrik", "toko listrik", "instalasi listrik",
	"listrik", "elektrik", "electrical", "electric", "switchgear", "switchboard",
	"配电", "电气", "电柜", "开关柜", "配电柜", "配电箱", "电缆", "变压器",
	"mcb", "mdb", "sdp", "kontaktor", "breaker", "trafo", "transformer",
	"genset", "inverter", "kabel listrik", "box panel", "panel box",
}

var foodKeywordHints = []string{
	"cafe", "café", "coffee", "kopi", "kedai kopi", "coffee shop",
	"restaurant", "restoran", "warung", "makanan", "food", "culinary",
	"bakery", "roti", "dessert", "bar ", "pub", "bistro", "eatery",
	"火锅", "咖啡", "咖啡馆", "餐厅", "美食", "奶茶", "小吃", "面馆", "烧烤",
	"ramen", "sushi", "pizza", "burger", "seafood", "steak", "dimsum",
	"kedai makan", "rumah makan", "warung makan", "kuliner",
}

var retailKeywordHints = []string{
	"toko", "shop", "store", "boutique", "mall tenant", "supermarket",
	"minimarket", "零售", "商店", "超市", "便利店",
}

var hotelKeywordHints = []string{
	"hotel", "motel", "inn", "hostel", "resort", "penginapan", "酒店", "宾馆",
}

var medicalKeywordHints = []string{
	"klinik", "clinic", "rumah sakit", "hospital", "apotek", "pharmacy",
	"dokter", "dentist", "医院", "诊所", "药店",
}

var educationKeywordHints = []string{
	"sekolah", "school", "universitas", "university", "kampus", "bimbel",
	"kursus", "les ", "学院", "学校", "大学", "培训",
}

var autoKeywordHints = []string{
	"bengkel", "mobil", "motor", "otomotif", "car wash", "dealer mobil",
	"汽车", "修车", "摩托",
}

var electricalPositivePhrases = []string{
	"panel listrik", "alat listrik", "toko listrik", "tukang listrik",
	"jasa instalasi listrik", "jasa teknik listrik", "penyedia peralatan listrik",
	"peralatan listrik", "instalasi listrik", "perusahaan tenaga",
	"lightning protection", "proteksi petir", "box panel", "panel box",
	"switchgear", "switchboard", "mekanikal elektrikal", "m&e",
}

var electricalPositiveWords = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(` +
	`listrik|elektrik|elektro|electrical|electric|switchgear|switchboard|` +
	`trafo|transformer|kabel|kontaktor|breaker|mcb|mdb|sdp|genset|inverter|` +
	`otomasi|plc|mcc|gardu|tegangan|solar|surya|fotovolta|` +
	`mechatronic|panel` +
	`)(?:[^a-z0-9]|$)`)

var electricalTitlePrimaryNoise = []string{
	"instalasi ac", "service ac", "servis ac", "ac central", "central ac",
	"ducting", "ac duct", "air conditioning",
	"instalasi cctv", "pasang cctv", "pasang kamera",
	"servis kulkas", "service kulkas", "mesin cuci",
}

var electricalNoiseHints = []string{
	"polsek", "polres", "kantor polisi", "police",
	"handphone", "hp store", "gadget", "smartphone",
	"laundry", "mesin cuci", "mesin pengering",
	"warung", "cafe", "café", "coffee", "restoran", "restaurant", "makanan",
	"bimbel", "kursus", "sekolah", "universitas", "kampus",
	"hotel", "motel", "spa", "salon", "barber", "klinik", "rumah sakit", "apotek",
	"bank ", " atm", "masjid", "gereja", "minimarket", "supermarket", "indomaret", "alfamart",
	"mall ", "gym", "fitness", "karaoke", "billiard",
	"kolam renang", "pool contractor", "kontraktor kolam",
	"oven microwave", "kulkas", "freezer", "chiller showcase",
	"service ac", "bengkel ac", "bengkel mobil", "bengkel motor",
	"desainer interior", "interior design", "jasa periklanan", "advertising",
	"pabrik kertas", "paper mill", "fotokopi", "print shop",
	"payment", "fintech", "asuransi", "insurance",
	"electronic city", "toko elektronik", "komponen elektronik",
	"hp jadul", "handphone jadul", "lapak scrup", "scrap", "scrup",
}

var electricalBroadCategories = []string{
	"toko elektronik", "pemasok komponen elektronik", "produsen elektronik",
	"grosir elektronik", "grosir aksesori elektronik", "bengkel elektronik",
	"kantor perusahaan", "produsen", "toko bahan bangunan", "toko perlengkapan rumah",
	"kontraktor umum", "kontraktor", "jasa konstruksi", "perusahaan konstruksi",
	"jasa tukang", "jasa renovasi", "gudang", "desainer interior",
}

var foodPositivePhrases = []string{
	"kedai kopi", "coffee shop", "coffee house", "warung kopi", "rumah kopi",
	"kedai makan", "rumah makan", "warung makan", "food court",
	"bakery", "dessert cafe", "cafe &", "café &",
}

var foodPositiveWords = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(` +
	`cafe|café|coffee|kopi|restoran|restaurant|warung|bakery|bistro|eatery|` +
	`kuliner|makanan|dessert|pastry|espresso|latte|roastery|bar|pub|` +
	`pizza|burger|ramen|sushi|seafood|steak|dimsum|noodle|` +
	`火锅|咖啡|餐厅|美食|奶茶|烧烤` +
	`)(?:[^a-z0-9]|$)`)

// Civic / institutional noise that almost never belongs in B2B lead scrapes
// unless the job itself is searching that vertical.
var universalHardNoise = []string{
	"polsek", "polres", "polda", "kantor polisi", "police station", "police dept",
	"kedutaan", "embassy", "konsulat", "consulate",
	"kantor kelurahan", "kantor kecamatan", "balai kota", "city hall",
	"pengadilan", "court house", "kejaksaan",
	"masjid agung", "gereja katedral", // leave small places of worship alone for food (halal nearby) — only big landmarks? Actually drop places of worship as primary category below.
}

var universalHardNoiseCategories = []string{
	"kantor polisi", "police", "government office", "city hall",
	"embassy", "consulate", "courthouse", "fire station", "pemadam kebakaran",
	"military", "pangkalan", "markas",
}

var scrapHardDrop = []string{
	"hp jadul", "handphone jadul", "lapak scrup", "scrap", "scrup",
	"barang bekas", "besi bekas", "besi tua", "rongsok", "rosok", "loakan",
	"pengepul", "jual beli bekas",
}

// Relevant reports whether a Maps hit belongs on the job brief.
func Relevant(p Place, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	blob := textBlob(p)
	title := strings.ToLower(p.Title)
	cat := strings.ToLower(p.Category)
	dom := detectDomain(keywords)

	if containsAny(blob, scrapHardDrop) {
		return false
	}
	if isUniversalCivicNoise(title, cat, blob, dom) {
		return false
	}

	switch dom {
	case domainElectrical:
		return relevantElectrical(p, keywords, blob, title, cat)
	case domainFood:
		return relevantFood(p, keywords, blob, title, cat)
	case domainHotel:
		return relevantDomain(p, keywords, blob, title, cat, hotelKeepSignals, hotelNoise)
	case domainMedical:
		return relevantDomain(p, keywords, blob, title, cat, medicalKeepSignals, medicalNoise)
	case domainEducation:
		return relevantDomain(p, keywords, blob, title, cat, educationKeepSignals, educationNoise)
	case domainAuto:
		return relevantDomain(p, keywords, blob, title, cat, autoKeepSignals, autoNoise)
	case domainRetail:
		return relevantRetail(p, keywords, blob, title, cat)
	default:
		return relevantGeneric(p, keywords, blob, title, cat)
	}
}

func detectDomain(keywords []string) domain {
	blob := strings.ToLower(strings.Join(keywords, " "))
	switch {
	case containsAny(blob, electricalKeywordHints):
		return domainElectrical
	case containsAny(blob, foodKeywordHints):
		return domainFood
	case containsAny(blob, hotelKeywordHints):
		return domainHotel
	case containsAny(blob, medicalKeywordHints):
		return domainMedical
	case containsAny(blob, educationKeywordHints):
		return domainEducation
	case containsAny(blob, autoKeywordHints):
		return domainAuto
	case containsAny(blob, retailKeywordHints):
		return domainRetail
	default:
		return domainGeneric
	}
}

func isUniversalCivicNoise(title, cat, blob string, dom domain) bool {
	if containsAny(cat, universalHardNoiseCategories) {
		return true
	}
	if containsAny(blob, universalHardNoise) {
		return true
	}
	// Police / government in title even when category is wrong.
	if containsAny(title, []string{"polsek", "polres", "polda", "kantor polisi"}) {
		return true
	}
	// Schools / hospitals are noise unless that domain is requested.
	if dom != domainEducation && containsAny(cat, []string{"sekolah", "school", "universitas", "university", "kampus", "college"}) {
		if !containsAny(title, foodPositivePhrases) { // campus cafe edge case handled in food
			return true
		}
	}
	if dom != domainMedical && containsAny(cat, []string{"rumah sakit", "hospital", "klinik", "clinic", "puskesmas"}) {
		return true
	}
	if dom != domainHotel && containsAny(cat, []string{"hotel", "motel", "hostel", "resort"}) &&
		!hasFoodPositive(title+" "+cat) {
		return true
	}
	return false
}

func relevantElectrical(p Place, keywords []string, blob, title, cat string) bool {
	if containsAny(title, electricalTitlePrimaryNoise) && !hasElectricalPositive(title) {
		return false
	}
	// Single noise gate: noise wins unless title/category carry an electrical signal.
	if containsAny(blob, electricalNoiseHints) && !hasElectricalPositive(title+" "+cat) {
		return false
	}
	if containsAny(cat, electricalBroadCategories) {
		if hasElectricalPositive(title) {
			return true
		}
		if !hasElectricalPositive(cat) {
			return false
		}
	}
	if hasElectricalPositive(blob) {
		return true
	}
	return keywordTokenHit(blob, keywords, map[string]bool{
		"electric": true, "panel": true, "power": true, "listrik": true,
	})
}

func relevantFood(p Place, keywords []string, blob, title, cat string) bool {
	foodNoise := []string{
		"polsek", "polres", "kantor polisi",
		"toko listrik", "panel listrik", "bengkel mobil", "bengkel motor",
		"laundry", "mesin cuci", "gym", "fitness", "karaoke",
		"bank ", " atm", "kantor perusahaan", "coworking",
		"dealer mobil", "showroom", "electronic city", "toko elektronik",
		"apotek", "klinik gigi", "rumah sakit",
		"sekolah", "universitas", "bimbel", "kursus mengemudi",
		"masjid", "gereja", "pura ", "vihara",
		"indomaret", "alfamart", "minimarket", // convenience unless title is cafe
		"pom bensin", "spbu", "gas station",
		"fotokopi", "print shop", "wartel",
	}
	if containsAny(blob, foodNoise) && !hasFoodPositive(title+" "+cat) {
		return false
	}
	// Convenience stores / malls without F&B signal.
	if containsAny(cat, []string{"minimarket", "convenience store", "supermarket", "department store", "shopping mall"}) &&
		!hasFoodPositive(title) {
		return false
	}
	if hasFoodPositive(title) || hasFoodPositive(cat) || hasFoodPositive(blob) {
		return true
	}
	return keywordTokenHit(blob, keywords, map[string]bool{
		"shop": true, "store": true, "place": true,
	})
}

var hotelKeepSignals = []string{"hotel", "motel", "inn", "hostel", "resort", "penginapan", "guest house", "guesthouse"}
var hotelNoise = []string{"cafe", "restaurant only", "toko", "bengkel", "polsek", "sekolah", "klinik"}

var medicalKeepSignals = []string{"klinik", "clinic", "rumah sakit", "hospital", "apotek", "pharmacy", "puskesmas", "dokter", "dental", "dentist"}
var medicalNoise = []string{"cafe", "restaurant", "toko listrik", "bengkel", "polsek", "hotel", "karaoke"}

var educationKeepSignals = []string{"sekolah", "school", "universitas", "university", "kampus", "bimbel", "kursus", "academy", "college"}
var educationNoise = []string{"cafe", "restaurant", "bengkel", "polsek", "hotel", "karaoke", "gym"}

var autoKeepSignals = []string{"bengkel", "mobil", "motor", "otomotif", "car wash", "dealer", "sparepart", "ban ", "oli "}
var autoNoise = []string{"cafe", "restaurant", "polsek", "sekolah", "klinik", "hotel", "toko listrik"}

func relevantDomain(_ Place, keywords []string, blob, title, cat string, keep, noise []string) bool {
	if containsAny(blob, noise) && !containsAny(title+" "+cat, keep) {
		return false
	}
	if containsAny(title+" "+cat, keep) || containsAny(blob, keep) {
		return true
	}
	return keywordTokenHit(blob, keywords, nil)
}

func relevantRetail(_ Place, keywords []string, blob, title, cat string) bool {
	retailNoise := []string{
		"polsek", "polres", "rumah sakit", "klinik", "sekolah", "universitas",
		"hotel", "motel", "karaoke", "gym", "fitness",
	}
	if containsAny(blob, retailNoise) {
		return false
	}
	if keywordTokenHit(blob, keywords, map[string]bool{"toko": true, "shop": true, "store": true}) {
		return true
	}
	// Keep retail-ish categories when keyword is generic "toko".
	if containsAny(cat, []string{"toko", "shop", "store", "boutique", "pasar", "market"}) {
		return true
	}
	_ = title
	return false
}

func relevantGeneric(_ Place, keywords []string, blob, title, cat string) bool {
	// Always drop obvious off-domain civic/consumer clutter for generic scrapes.
	genericNoise := []string{
		"polsek", "polres", "kantor polisi", "police",
		"karaoke", "billiard", "kolam renang",
	}
	if containsAny(blob, genericNoise) {
		return false
	}
	if keywordTokenHit(blob, keywords, nil) {
		return true
	}
	// Soft keep: category or title shares a multi-byte / long token with keywords.
	_ = title
	_ = cat
	return false
}

func hasElectricalPositive(text string) bool {
	if text == "" {
		return false
	}
	if containsAny(text, electricalPositivePhrases) {
		return true
	}
	return electricalPositiveWords.MatchString(text)
}

func hasFoodPositive(text string) bool {
	if text == "" {
		return false
	}
	if containsAny(text, foodPositivePhrases) {
		return true
	}
	return foodPositiveWords.MatchString(text)
}

func textBlob(p Place) string {
	parts := []string{p.Title, p.Category, p.Address, p.Descriptions, p.About}
	return strings.ToLower(strings.Join(parts, " "))
}

func containsAny(hay string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func keywordTokenHit(blob string, keywords []string, skip map[string]bool) bool {
	for _, kw := range keywords {
		for _, tok := range KeywordTokens(kw) {
			if len([]rune(tok)) < 3 {
				continue
			}
			if skip != nil && skip[tok] {
				continue
			}
			if strings.Contains(blob, tok) {
				return true
			}
		}
	}
	return false
}

// KeywordTokens splits a keyword into alphanumeric / CJK tokens.
func KeywordTokens(kw string) []string {
	kw = strings.ToLower(strings.TrimSpace(kw))
	if kw == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range kw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// Filter returns only relevant places.
func Filter(places []Place, keywords []string) []Place {
	if len(places) == 0 {
		return places
	}
	out := make([]Place, 0, len(places))
	for _, p := range places {
		if Relevant(p, keywords) {
			out = append(out, p)
		}
	}
	return out
}
