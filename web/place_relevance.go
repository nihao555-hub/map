package web

import (
	"regexp"
	"strings"
	"unicode"
)

// Electrical search head-terms (Maps + Chinese). When job keywords match this
// domain we drop consumer / unrelated Maps noise so results and intel stay on-brief.
var electricalKeywordHints = []string{
	"panel listrik", "alat listrik", "toko listrik", "instalasi listrik",
	"listrik", "elektrik", "electrical", "electric", "switchgear", "switchboard",
	"配电", "电气", "电柜", "开关柜", "配电柜", "配电箱", "电缆", "变压器",
	"mcb", "mdb", "sdp", "kontaktor", "breaker", "trafo", "transformer",
	"genset", "inverter", "kabel listrik", "box panel", "panel box",
}

// Phrase positives (substring OK).
var electricalPositivePhrases = []string{
	"panel listrik", "alat listrik", "toko listrik", "tukang listrik",
	"jasa instalasi listrik", "jasa teknik listrik", "penyedia peralatan listrik",
	"peralatan listrik", "instalasi listrik", "perusahaan tenaga",
	"lightning protection", "proteksi petir", "box panel", "panel box",
	"switchgear", "switchboard", "mekanikal elektrikal", "m&e",
}

// Word-boundary positives — avoid matching "electric" inside "electronic".
// Note: bare "instalasi" is NOT enough (Maps often labels AC/CCTV as
// "Jasa Instalasi Listrik"); require listrik/panel/etc. or the phrase below.
var electricalPositiveWords = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(` +
	`listrik|elektrik|elektro|electrical|electric|switchgear|switchboard|` +
	`trafo|transformer|kabel|kontaktor|breaker|mcb|mdb|sdp|genset|inverter|` +
	`otomasi|plc|mcc|gardu|tegangan|solar|surya|fotovolta|` +
	`mechatronic|panel` +
	`)(?:[^a-z0-9]|$)`)

// Titles that are clearly HVAC / CCTV / appliance work even when Maps puts them
// under an electrical category. Drop unless the title itself also looks electrical.
var electricalTitlePrimaryNoise = []string{
	"instalasi ac", "service ac", "servis ac", "ac central", "central ac",
	"ducting", "ac duct", "air conditioning",
	"instalasi cctv", "pasang cctv", "pasang kamera",
	"servis kulkas", "service kulkas", "mesin cuci",
}

// Hard noise for electrical jobs: consumer / civic / unrelated services.
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

// Categories that are too broad unless the title itself looks electrical.
var electricalBroadCategories = []string{
	"toko elektronik", "pemasok komponen elektronik", "produsen elektronik",
	"grosir elektronik", "grosir aksesori elektronik", "bengkel elektronik",
	"kantor perusahaan", "produsen", "toko bahan bangunan", "toko perlengkapan rumah",
	"kontraktor umum", "kontraktor", "jasa konstruksi", "perusahaan konstruksi",
	"jasa tukang", "jasa renovasi", "gudang", "desainer interior",
}

// isElectricalKeywordJob reports whether the scrape intent is electrical / power equipment.
func isElectricalKeywordJob(keywords []string) bool {
	blob := strings.ToLower(strings.Join(keywords, " "))
	if blob == "" {
		return false
	}
	for _, h := range electricalKeywordHints {
		if strings.Contains(blob, h) {
			return true
		}
	}
	return false
}

func placeTextBlob(p Place) string {
	parts := []string{
		p.Title, p.Category, p.Address, p.Descriptions, p.About, p.CompleteAddress,
	}
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

func hasElectricalPositive(text string) bool {
	if text == "" {
		return false
	}
	if containsAny(text, electricalPositivePhrases) {
		return true
	}
	return electricalPositiveWords.MatchString(text)
}

// PlaceRelevantToKeywords keeps Maps hits that match the job's search domain.
// Non-electrical jobs currently pass through unchanged (no domain filter).
func PlaceRelevantToKeywords(p Place, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	if !isElectricalKeywordJob(keywords) {
		return true
	}
	blob := placeTextBlob(p)
	title := strings.ToLower(p.Title)
	cat := strings.ToLower(p.Category)

	// Hard-drop scrap / secondhand / phone-resale listings even when they mention
	// electrical words: junk dealers list "kabel"/"panel" as materials they buy.
	hardDrop := []string{
		"hp jadul", "handphone jadul", "lapak scrup", "scrap", "scrup",
		"barang bekas", "besi bekas", "besi tua", "rongsok", "rosok", "loakan",
		"pengepul", "jual beli bekas",
		"polsek", "polres", "kantor polisi",
	}
	if containsAny(blob, hardDrop) {
		return false
	}

	// Maps often miscategorizes AC/CCTV shops as "Jasa Instalasi Listrik".
	// Trust the title: if it is HVAC/CCTV-primary and lacks an electrical signal
	// in the title itself, drop — category alone must not rescue it.
	if containsAny(title, electricalTitlePrimaryNoise) && !hasElectricalPositive(title) {
		return false
	}

	if containsAny(blob, electricalNoiseHints) && !hasElectricalPositive(title+" "+cat) {
		return false
	}
	if containsAny(blob, electricalNoiseHints) && !hasElectricalPositive(blob) {
		return false
	}

	// Broad categories need an explicit electrical signal in the title (or strong cat).
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

	// Token overlap with keywords (e.g. user typed a brand-ish phrase).
	for _, kw := range keywords {
		for _, tok := range keywordTokens(kw) {
			if len(tok) < 4 {
				continue
			}
			if tok == "electric" || tok == "panel" || tok == "power" {
				continue
			}
			if strings.Contains(blob, tok) {
				return true
			}
		}
	}
	return false
}

func keywordTokens(kw string) []string {
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

// FilterRelevantPlaces drops off-brief Maps noise for the job keywords.
func FilterRelevantPlaces(places []Place, keywords []string) []Place {
	if len(places) == 0 || !isElectricalKeywordJob(keywords) {
		return places
	}
	out := make([]Place, 0, len(places))
	for _, p := range places {
		if PlaceRelevantToKeywords(p, keywords) {
			out = append(out, p)
		}
	}
	return out
}

// FilterRelevantPlacesLite is the compact-payload variant.
func FilterRelevantPlacesLite(places []PlaceLite, keywords []string) []PlaceLite {
	if len(places) == 0 || !isElectricalKeywordJob(keywords) {
		return places
	}
	out := make([]PlaceLite, 0, len(places))
	for _, p := range places {
		full := Place{
			Title: p.Title, Category: p.Category, Address: p.Address,
			Phone: p.Phone, Website: p.Website, Emails: p.Emails,
			PlaceID: p.PlaceID, Cid: p.Cid,
		}
		if PlaceRelevantToKeywords(full, keywords) {
			out = append(out, p)
		}
	}
	return out
}
