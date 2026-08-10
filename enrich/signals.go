package enrich

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// tradeRoleKeywords maps the supply-chain position a company claims to the
// phrases it uses to claim it. Knowing whether a lead is a factory, a trading
// company or an end retailer is what separates a usable lead list from a
// directory dump.
var tradeRoleKeywords = map[string][]string{
	"manufacturer": {
		"manufacturer", "manufacturers", "manufacturing", "we manufacture",
		"our factory", "own factory", "factory direct", "oem", "odm",
		"production line", "fabricant", "hersteller", "fabricante",
		"produttore", "生产厂家", "制造商", "工厂",
	},
	"wholesaler": {
		"wholesale", "wholesaler", "bulk order", "bulk orders", "trade prices",
		"grossist", "grossiste", "mayorista", "grossista", "批发",
	},
	"distributor": {
		"distributor", "distributors", "distribution partner", "authorized dealer",
		"authorised dealer", "vertriebspartner", "distribuidor", "经销商",
	},
	"importer": {
		"importer", "we import", "import agent", "importeur", "importador", "进口商",
	},
	"exporter": {
		"exporter", "we export", "export to", "exports to", "export markets",
		"exportateur", "exportador", "出口商",
	},
	"retailer": {
		"retailer", "retail store", "our shop", "our store", "showroom",
		"einzelhandel", "minorista", "零售",
	},
	"service-provider": {
		"consultancy", "consulting services", "we provide services",
		"service provider", "agency services",
	},
}

// certificationPatterns detect quality and compliance marks. Each pattern is
// anchored on word boundaries so "CE" does not match inside "CERTIFICATE".
var certificationPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"ISO 9001", regexp.MustCompile(`(?i)\biso[\s\-:]?9001\b`)},
	{"ISO 14001", regexp.MustCompile(`(?i)\biso[\s\-:]?14001\b`)},
	{"ISO 45001", regexp.MustCompile(`(?i)\biso[\s\-:]?45001\b`)},
	{"ISO 13485", regexp.MustCompile(`(?i)\biso[\s\-:]?13485\b`)},
	{"ISO 22000", regexp.MustCompile(`(?i)\biso[\s\-:]?22000\b`)},
	{"IATF 16949", regexp.MustCompile(`(?i)\biatf[\s\-:]?16949\b`)},
	{"CE", regexp.MustCompile(`(?i)\bce[\s\-]?(?:mark|marking|certified|certificate|zertifi\w*)\b`)},
	{"FDA", regexp.MustCompile(`(?i)\bfda[\s\-]?(?:approved|registered|certified|cleared|compliance|compliant)\b`)},
	{"RoHS", regexp.MustCompile(`(?i)\brohs\b`)},
	{"REACH", regexp.MustCompile(`(?i)\breach\s+(?:compliant|compliance|certified|registered)\b`)},
	{"FSC", regexp.MustCompile(`(?i)\bfsc[\s\-]?(?:certified|certificate|mix|100%)\b`)},
	{"GOTS", regexp.MustCompile(`(?i)\bgots\b`)},
	{"OEKO-TEX", regexp.MustCompile(`(?i)\boeko[\s\-]?tex\b`)},
	{"BSCI", regexp.MustCompile(`(?i)\bbsci\b`)},
	{"SEDEX", regexp.MustCompile(`(?i)\bsedex\b`)},
	{"BRC", regexp.MustCompile(`(?i)\bbrcgs?\b`)},
	{"IFS", regexp.MustCompile(`(?i)\bifs\s+(?:food|broker|logistics)\b`)},
	{"HACCP", regexp.MustCompile(`(?i)\bhaccp\b`)},
	{"GMP", regexp.MustCompile(`(?i)\bgmp\b`)},
	{"HALAL", regexp.MustCompile(`(?i)\bhalal\b`)},
	{"KOSHER", regexp.MustCompile(`(?i)\bkosher\b`)},
	{"UL", regexp.MustCompile(`(?i)\bul[\s\-]?(?:listed|certified|recognized|recognised)\b`)},
	{"ETL", regexp.MustCompile(`(?i)\betl[\s\-]?(?:listed|certified)\b`)},
	{"CSA", regexp.MustCompile(`(?i)\bcsa[\s\-]?(?:certified|approved)\b`)},
	{"SGS", regexp.MustCompile(`(?i)\bsgs\b`)},
	{"TUV", regexp.MustCompile(`(?i)\bt[uü]v\b`)},
	{"GS", regexp.MustCompile(`(?i)\bgs[\s\-]?(?:mark|certified|zeichen)\b`)},
	{"FCC", regexp.MustCompile(`(?i)\bfcc\b`)},
	{"USDA Organic", regexp.MustCompile(`(?i)\busda\s+organic\b`)},
	{"EU Organic", regexp.MustCompile(`(?i)\beu\s+organic\b`)},
	{"Fairtrade", regexp.MustCompile(`(?i)\bfair\s?trade\b`)},
	{"GRS", regexp.MustCompile(`(?i)\bglobal\s+recycled\s+standard\b|\bgrs\s+certified\b`)},
}

// registrationTextPatterns find legal identifiers in page text (typically the
// footer or an Impressum page). The label is captured so we can report which
// register the number belongs to.
var registrationTextPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	// EU VAT: two-letter country prefix plus 2-13 alphanumerics.
	{"vat", regexp.MustCompile(`(?i)\b(?:vat(?:\s*(?:no|number|id|reg(?:\.|istration)?))?|ust[\s\-]?idnr|umsatzsteuer[\s\-]?id|btw|tva|iva|nif|cif|mwst)\.?\s*[:#]?\s*((?:[A-Z]{2}\s?)?[0-9A-Z][0-9A-Z\s\-]{5,15}[0-9A-Z])`)},
	{"registration", regexp.MustCompile(`(?i)\b(?:company\s+(?:reg(?:\.|istration)?\s*(?:no|number)?|number)|reg(?:\.|istration)?\s*(?:no|number)|handelsregister|hrb|siret|siren|kvk|nzbn|abn|acn|cnpj|rut)\.?\s*[:#]?\s*([0-9][0-9A-Z\s\-./]{4,20}[0-9A-Z])`)},
	{"tax", regexp.MustCompile(`(?i)\b(?:tax\s*(?:id|no|number)|ein|tin|steuernummer)\.?\s*[:#]?\s*([0-9][0-9A-Z\s\-]{4,18}[0-9A-Z])`)},
	{"duns", regexp.MustCompile(`(?i)\bd[\s\-]?u[\s\-]?n[\s\-]?s\s*(?:number|no)?\.?\s*[:#]?\s*(\d{2}[\d\s\-]{5,11}\d)`)},
}

// foundedPatterns capture the year a company says it started trading.
var foundedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:established|founded|since|est\.?|operating\s+since|in\s+business\s+since|serving\s+\w+\s+since)\s*(?:in\s+)?(\d{4})\b`),
	regexp.MustCompile(`(?i)\b(?:gegründet|seit)\s*(?:im\s+Jahr\s+)?(\d{4})\b`),
	regexp.MustCompile(`(?i)\b(?:fundada|fundado|desde)\s*(?:en\s+)?(\d{4})\b`),
	regexp.MustCompile(`(?i)(?:成立于|创建于|创立于|始于)\s*(\d{4})`),
	regexp.MustCompile(`(?i)\b(\d{4})\s*年成立`),
}

// employeePatterns capture published headcount, either as a range or a single
// figure with a qualifier.
var employeePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(\d{1,3}(?:[,.]\d{3})?)\s*(?:-|–|to)\s*(\d{1,3}(?:[,.]\d{3})?)\s+(?:employees|staff|people|workers|mitarbeiter|empleados)\b`),
	regexp.MustCompile(`(?i)\b(?:over|more\s+than|about|approx\.?|approximately|around|nearly|team\s+of)\s+(\d{1,3}(?:[,.]\d{3})?)\+?\s+(?:employees|staff|people|workers|professionals|mitarbeiter|empleados)\b`),
	regexp.MustCompile(`(?i)\b(\d{1,3}(?:[,.]\d{3})?)\+?\s+(?:employees|staff\s+members|skilled\s+workers|mitarbeiter|empleados)\b`),
	regexp.MustCompile(`(?i)(?:员工|职工)\s*(\d{1,5})\s*(?:余|多)?\s*人`),
}

// platformSignatures identify the CMS or commerce stack from markup left in
// the HTML. Knowing the stack tells you whether the company sells online and
// how technically sophisticated its marketing is.
var platformSignatures = []struct {
	platform  string
	ecommerce bool
	needles   []string
}{
	{"shopify", true, []string{"cdn.shopify.com", "shopify-features", "myshopify.com", "Shopify.theme"}},
	{"woocommerce", true, []string{"woocommerce", "wc-add-to-cart", "wp-content/plugins/woocommerce"}},
	{"magento", true, []string{"mage/cookies", "Magento_", "/static/version", "magento"}},
	{"prestashop", true, []string{"prestashop", "/modules/ps_"}},
	{"bigcommerce", true, []string{"bigcommerce.com", "cdn11.bigcommerce.com"}},
	{"shopware", true, []string{"shopware", "/widgets/checkout"}},
	{"salesforce-commerce", true, []string{"demandware.static", "on/demandware.store"}},
	{"squarespace-commerce", true, []string{"squarespace-cdn.com/universal/scripts-compressed/commerce"}},
	{"wix-stores", true, []string{"wixstores", "ecom-platform"}},
	{"opencart", true, []string{"index.php?route=product", "catalog/view/theme"}},
	{"wordpress", false, []string{"wp-content/", "wp-includes/", "wp-json"}},
	{"squarespace", false, []string{"squarespace.com", "static1.squarespace.com"}},
	{"wix", false, []string{"wix.com", "wixstatic.com", "_wixCssStates"}},
	{"webflow", false, []string{"webflow.com", "wf-form", "data-wf-page"}},
	{"drupal", false, []string{"/sites/default/files", "Drupal.settings", "drupal.js"}},
	{"joomla", false, []string{"/media/jui/", "joomla"}},
	{"hubspot-cms", false, []string{"hs-scripts.com", "hubspot.net/hubfs"}},
	{"typo3", false, []string{"typo3temp", "typo3conf"}},
	{"weebly", false, []string{"weebly.com", "weeblycloud"}},
	{"godaddy-website-builder", false, []string{"img1.wsimg.com"}},
}

// ecommerceNeedles catch a cart on sites whose platform we could not name.
var ecommerceNeedles = []string{
	"add to cart", "add to basket", "shopping cart", "proceed to checkout",
	"in den warenkorb", "añadir al carrito", "ajouter au panier",
	"aggiungi al carrello", "加入购物车",
}

// tradeMarkets are the regions and countries worth reporting when a company
// says where it sells. The list is limited to major trade markets so a stray
// mention of a city or a shipping note does not turn into a market claim.
var tradeMarkets = []string{
	"Europe", "European Union", "North America", "South America",
	"Latin America", "Central America", "Middle East", "Southeast Asia",
	"South Asia", "East Asia", "Central Asia", "Africa", "West Africa",
	"East Africa", "North Africa", "Oceania", "Scandinavia", "Balkans",
	"Benelux", "Caribbean", "Gulf", "GCC", "Worldwide", "Global",
	"United States", "USA", "Canada", "Mexico", "Brazil", "Argentina",
	"Chile", "Colombia", "Peru", "United Kingdom", "Ireland", "Germany",
	"France", "Italy", "Spain", "Portugal", "Netherlands", "Belgium",
	"Luxembourg", "Switzerland", "Austria", "Denmark", "Sweden", "Norway",
	"Finland", "Iceland", "Poland", "Czech Republic", "Slovakia", "Hungary",
	"Romania", "Bulgaria", "Greece", "Croatia", "Slovenia", "Serbia",
	"Ukraine", "Russia", "Turkey", "Israel", "Saudi Arabia",
	"United Arab Emirates", "UAE", "Qatar", "Kuwait", "Oman", "Bahrain",
	"Egypt", "Morocco", "Tunisia", "Algeria", "Nigeria", "Ghana", "Kenya",
	"Ethiopia", "Tanzania", "South Africa", "India", "Pakistan",
	"Bangladesh", "Sri Lanka", "China", "Hong Kong", "Taiwan", "Japan",
	"South Korea", "Vietnam", "Thailand", "Malaysia", "Singapore",
	"Indonesia", "Philippines", "Cambodia", "Myanmar", "Australia",
	"New Zealand",
}

// marketContextNeedles gate market detection: a country name only counts as a
// market claim when it appears near language about selling or shipping.
var marketContextNeedles = []string{
	"export", "exports", "exporting", "market", "markets", "customers in",
	"clients in", "we ship", "shipping to", "delivery to", "distribute",
	"distribution", "sell to", "selling to", "present in", "operate in",
	"operations in", "worldwide", "countries", "serving", "supply to",
	"exportiert", "märkte", "exportamos", "mercados", "出口", "市场",
}

// DetectTradeRoles reports the supply-chain positions the page text claims.
func DetectTradeRoles(text string) []string {
	lower := strings.ToLower(text)

	out := make([]string, 0, len(tradeRoleKeywords))

	// Iterate a fixed order so results are stable across runs.
	for _, role := range []string{
		"manufacturer", "wholesaler", "distributor", "importer",
		"exporter", "retailer", "service-provider",
	} {
		for _, needle := range tradeRoleKeywords[role] {
			if strings.Contains(lower, needle) {
				out = append(out, role)

				break
			}
		}
	}

	return out
}

// DetectCertifications reports the quality and compliance marks a page claims.
func DetectCertifications(text string) []string {
	out := make([]string, 0, len(certificationPatterns))

	for _, pattern := range certificationPatterns {
		if pattern.re.MatchString(text) {
			out = append(out, pattern.label)
		}
	}

	return out
}

// DetectFoundedYear finds the founding year, ignoring values that cannot be a
// company's founding year (future dates, pre-industrial dates).
func DetectFoundedYear(text string) int {
	for _, pattern := range foundedPatterns {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			year, err := strconv.Atoi(match[1])
			if err != nil {
				continue
			}

			if plausible := plausibleYear(year); plausible > 0 {
				return plausible
			}
		}
	}

	return 0
}

// earliestPlausibleFoundingYear predates every company likely to have a
// website while still rejecting obvious parse errors such as street numbers.
const earliestPlausibleFoundingYear = 1600

func plausibleYear(year int) int {
	if year < earliestPlausibleFoundingYear || year > time.Now().UTC().Year() {
		return 0
	}

	return year
}

// DetectEmployeeRange finds a published headcount, normalized to either
// "min-max" or "N+".
func DetectEmployeeRange(text string) string {
	for _, pattern := range employeePatterns {
		match := pattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}

		normalize := func(s string) string {
			return strings.NewReplacer(",", "", ".", "").Replace(s)
		}

		if len(match) >= 3 && match[2] != "" {
			return normalize(match[1]) + "-" + normalize(match[2])
		}

		return normalize(match[1]) + "+"
	}

	return ""
}

// ExtractRegistrationIDsFromText finds VAT, company-register, tax and DUNS
// numbers printed on the page.
func ExtractRegistrationIDsFromText(text, source string) []RegistrationID {
	var out []RegistrationID

	for _, pattern := range registrationTextPatterns {
		for _, match := range pattern.re.FindAllStringSubmatch(text, -1) {
			value := normalizeRegistrationValue(match[1])
			if value == "" {
				continue
			}

			out = append(out, RegistrationID{
				Kind:    pattern.kind,
				Value:   value,
				Country: registrationCountry(pattern.kind, value),
				Source:  source,
			})
		}
	}

	return out
}

// minRegistrationDigits rejects fragments such as "No. 12" that the looser
// register patterns would otherwise pick up.
const minRegistrationDigits = 5

func normalizeRegistrationValue(raw string) string {
	value := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
	value = strings.Trim(value, " -.,;:/")

	if len(digitsOnly(value)) < minRegistrationDigits {
		return ""
	}

	return value
}

// registrationCountry reads the ISO country prefix that EU VAT numbers carry.
func registrationCountry(kind, value string) string {
	if kind != "vat" || len(value) < 3 {
		return ""
	}

	prefix := value[:2]
	for i := 0; i < 2; i++ {
		if prefix[i] < 'A' || prefix[i] > 'Z' {
			return ""
		}
	}

	return prefix
}

// DetectPlatform identifies the CMS/commerce stack and whether the site sells
// online. Detection walks the signature list in order, so specific commerce
// platforms win over the generic CMS they run on.
func DetectPlatform(html string) (platform string, hasEcommerce bool) {
	lower := strings.ToLower(html)

	for _, signature := range platformSignatures {
		for _, needle := range signature.needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				platform = signature.platform
				hasEcommerce = signature.ecommerce

				break
			}
		}

		if platform != "" {
			break
		}
	}

	if !hasEcommerce {
		for _, needle := range ecommerceNeedles {
			if strings.Contains(lower, needle) {
				hasEcommerce = true

				break
			}
		}
	}

	return platform, hasEcommerce
}

// DetectMarkets reports the countries and regions a page claims to serve. A
// country only counts when the surrounding sentence talks about selling,
// exporting or shipping, which keeps office addresses out of the list.
func DetectMarkets(text string) []string {
	lower := strings.ToLower(text)

	sentences := splitSentences(lower)

	relevant := make([]string, 0, len(sentences))

	for _, sentence := range sentences {
		for _, needle := range marketContextNeedles {
			if strings.Contains(sentence, needle) {
				relevant = append(relevant, sentence)

				break
			}
		}
	}

	if len(relevant) == 0 {
		return nil
	}

	joined := strings.Join(relevant, " | ")

	out := make([]string, 0, 8)

	for _, market := range tradeMarkets {
		if containsWord(joined, strings.ToLower(market)) {
			out = append(out, market)
		}
	}

	return out
}

// sentenceTerminators split running text into sentence-like chunks for
// context-gated matching.
const sentenceTerminators = ".!?;\n|•"

func splitSentences(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune(sentenceTerminators, r)
	})
}

// containsWord reports whether needle appears in haystack on word boundaries,
// so "Oman" does not match inside "Romania".
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}

	offset := 0

	for {
		idx := strings.Index(haystack[offset:], needle)
		if idx < 0 {
			return false
		}

		start := offset + idx
		end := start + len(needle)

		beforeOK := start == 0 || !isWordByte(haystack[start-1])
		afterOK := end == len(haystack) || !isWordByte(haystack[end])

		if beforeOK && afterOK {
			return true
		}

		offset = start + 1
		if offset >= len(haystack) {
			return false
		}
	}
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		b >= 0x80 // keep multi-byte letters (CJK, accented Latin) as word bytes
}
