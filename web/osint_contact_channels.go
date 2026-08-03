package web

import (
	"net/url"
	"regexp"
	"strings"
)

// 多渠道联系方式（不只邮箱）：官网/Maps 电话、WhatsApp、社媒；并据此补决策人可触达字段与组织架构。

var (
	waMeFindRe = regexp.MustCompile(`(?i)(?:https?://)?(?:wa\.me/|api\.whatsapp\.com/send\?[^"'>\s]*phone=)(\+?\d{8,15})`)
	waLabelRe  = regexp.MustCompile(`(?i)whatsapp[^0-9+]{0,24}(\+?\d[\d\s\-().]{7,18}\d)`)
	socialURLRe = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:instagram\.com|facebook\.com|fb\.com|linkedin\.com/(?:in|company)|t\.me|telegram\.me|twitter\.com|x\.com|tiktok\.com|youtube\.com|youtu\.be)/[^\s"'<>]+`)
	telLinkRe  = regexp.MustCompile(`(?i)tel:(\+?[\d\s\-().]{7,20})`)
)

// harvestContactChannelsFromHTML 从页面抽取电话 / WhatsApp / 社媒（外贸触达不只靠邮箱）。
func harvestContactChannelsFromHTML(html string) (phones []string, whatsapp string, socials map[string]string) {
	socials = map[string]string{}
	if strings.TrimSpace(html) == "" {
		return nil, "", socials
	}
	text := html
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "%2F", "/")
	text = strings.ReplaceAll(text, "%2f", "/")
	text = strings.ReplaceAll(text, "%3F", "?")
	text = strings.ReplaceAll(text, "%3f", "?")
	text = strings.ReplaceAll(text, "%3D", "=")
	text = strings.ReplaceAll(text, "%3d", "=")
	text = strings.ReplaceAll(text, "%2B", "+")
	text = strings.ReplaceAll(text, "%2b", "+")

	low := strings.ToLower(text)
	if strings.Contains(low, "chat.whatsapp.com/") {
		text = regexp.MustCompile(`(?i)https?://chat\.whatsapp\.com/[^\s"'<>]+`).ReplaceAllString(text, "")
	}

	if m := waMeFindRe.FindStringSubmatch(text); len(m) >= 2 {
		whatsapp = normalizeWADigits(m[1])
	} else if m := waLabelRe.FindStringSubmatch(text); len(m) >= 2 {
		whatsapp = normalizeWADigits(m[1])
	}

	for _, m := range telLinkRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			phones = append(phones, strings.TrimSpace(m[1]))
		}
	}
	phones = append(phones, phoneFindRe.FindAllString(stripTags(text), -1)...)
	phones = filterPhones(phones)

	for _, raw := range socialURLRe.FindAllString(text, -1) {
		raw = strings.Split(raw, "?")[0]
		raw = strings.TrimRight(raw, "/\"'")
		lowU := strings.ToLower(raw)
		switch {
		case strings.Contains(lowU, "linkedin.com/"):
			if socials["linkedin"] == "" {
				socials["linkedin"] = raw
			}
		case strings.Contains(lowU, "instagram.com"):
			if socials["instagram"] == "" && !strings.Contains(lowU, "/p/") {
				socials["instagram"] = raw
			}
		case strings.Contains(lowU, "facebook.com"), strings.Contains(lowU, "fb.com"):
			if socials["facebook"] == "" && !strings.Contains(lowU, "/posts/") && !strings.Contains(lowU, "/tr") {
				socials["facebook"] = raw
			}
		case strings.Contains(lowU, "t.me/"), strings.Contains(lowU, "telegram.me/"):
			if socials["telegram"] == "" {
				socials["telegram"] = raw
			}
		case strings.Contains(lowU, "twitter.com"), strings.Contains(lowU, "x.com/"):
			if socials["twitter"] == "" {
				socials["twitter"] = raw
			}
		case strings.Contains(lowU, "tiktok.com"):
			if socials["tiktok"] == "" {
				socials["tiktok"] = raw
			}
		case strings.Contains(lowU, "youtube.com"), strings.Contains(lowU, "youtu.be"):
			if socials["youtube"] == "" {
				socials["youtube"] = raw
			}
		}
	}
	if whatsapp != "" {
		phones = mergeUnique(phones, []string{whatsapp})
		socials["whatsapp"] = whatsapp
	}
	return phones, whatsapp, socials
}

func normalizeWADigits(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 8 || len(d) > 15 {
		return ""
	}
	return "+" + d
}

// deriveWhatsAppFromPhone 地图有电话但无 WhatsApp 时，用 wa.me 可触达号作为渠道（东南亚外贸常用）。
func deriveWhatsAppFromPhone(phone, existingWA string) string {
	if strings.TrimSpace(existingWA) != "" {
		return strings.TrimSpace(existingWA)
	}
	wa := normalizeWADigits(phone)
	if wa == "" {
		return ""
	}
	// 过短/明显非手机的固话也可留作 tel；仍生成 wa 候选，业务侧可点开验证
	return wa
}

func mergeSocialMaps(dst map[string]string, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if dst[k] == "" {
			dst[k] = v
		}
	}
	return dst
}

// enrichMultiChannelContacts 汇总 Maps + 官网渠道，并挂到决策人。
func enrichMultiChannelContacts(intel *PlaceIntel, place Place) {
	if intel == nil {
		return
	}
	if intel.Socials == nil {
		intel.Socials = map[string]string{}
	}

	phone := firstNonEmpty(place.Phone, place.WhatsApp)
	wa := deriveWhatsAppFromPhone(firstNonEmpty(place.WhatsApp, place.Phone), place.WhatsApp)
	if phone != "" {
		intel.Phones = mergeUnique(intel.Phones, []string{phone})
	}
	if wa != "" {
		intel.Phones = mergeUnique(intel.Phones, []string{wa})
		if intel.Socials["whatsapp"] == "" {
			intel.Socials["whatsapp"] = wa
		}
	}

	// 官网已挖到的 WA 优先
	if siteWA := intel.Socials["whatsapp"]; siteWA != "" {
		wa = siteWA
		intel.Phones = mergeUnique(intel.Phones, []string{siteWA})
	}

	for i := range intel.DecisionMakers {
		d := &intel.DecisionMakers[i]
		if d.Phone == "" {
			d.Phone = firstNonEmpty(phone, wa)
		}
		if d.WhatsApp == "" {
			d.WhatsApp = wa
		}
		if d.LinkedIn == "" && intel.Socials["linkedin"] != "" && !strings.Contains(strings.ToLower(intel.Socials["linkedin"]), "/company/") {
			// 公司页不直接塞给人；个人页才挂
		}
	}

	// 无决策人但有任一渠道时，造一条「可触达业务联系」方便 UI
	if len(intel.DecisionMakers) == 0 {
		email := ""
		if len(intel.ExtraEmails) > 0 {
			email = intel.ExtraEmails[0]
		}
		if email != "" || phone != "" || wa != "" || intel.Socials["linkedin"] != "" || intel.Socials["facebook"] != "" {
			intel.DecisionMakers = append(intel.DecisionMakers, DecisionMaker{
				Name:       "",
				Title:      "Business contact",
				Email:      email,
				Phone:      firstNonEmpty(phone, wa),
				WhatsApp:   wa,
				LinkedIn:   intel.Socials["linkedin"],
				Source:     "multi-channel",
				Evidence:   "maps/website contact channels",
				Confidence: "medium",
			})
		}
	}

	intel.OrgStructure = mergeOrgUnits(intel.OrgStructure, orgUnitsFromChannels(intel, place.Title))
}

func orgUnitsFromChannels(intel *PlaceIntel, company string) []OrgUnit {
	company = strings.TrimSpace(company)
	var out []OrgUnit
	if intel.Socials != nil {
		if u := intel.Socials["linkedin"]; u != "" {
			role := "linkedin-company"
			if strings.Contains(strings.ToLower(u), "/in/") {
				role = "linkedin-people"
			}
			out = append(out, OrgUnit{Name: company, Role: role, Evidence: u})
		}
		if intel.Socials["facebook"] != "" {
			out = append(out, OrgUnit{Name: company, Role: "social-facebook", Evidence: intel.Socials["facebook"]})
		}
		if intel.Socials["instagram"] != "" {
			out = append(out, OrgUnit{Name: company, Role: "social-instagram", Evidence: intel.Socials["instagram"]})
		}
	}
	if len(intel.Phones) > 0 || (intel.Socials != nil && intel.Socials["whatsapp"] != "") {
		out = append(out, OrgUnit{Name: company, Role: "contact-center", Evidence: "phone/whatsapp channel"})
	}
	if len(intel.ExtraEmails) > 0 {
		out = append(out, OrgUnit{Name: company, Role: "email-channel", Evidence: "public email inbox"})
	}
	return out
}

func whatsappLink(digits string) string {
	d := normalizeWADigits(digits)
	if d == "" {
		return ""
	}
	return "https://wa.me/" + strings.TrimPrefix(d, "+")
}

func absSocialURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http") {
		return raw
	}
	if u, err := url.Parse("https://" + strings.TrimPrefix(raw, "//")); err == nil && u.Host != "" {
		return u.String()
	}
	return raw
}
