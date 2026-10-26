// Package jetboxlink разбирает ссылку подключения из Кабинета.
//
// Контракт формата — Antigravity/JetBox_iOS/LINK-CONTRACT.md (ред. 1.1).
// Это перенос JetBoxLink.swift из iOS-клиента один в один: те же правила,
// те же исходы. Ключ и параметры живут во ФРАГМЕНТЕ (после `#`), фрагмент
// не уходит на сервер, поэтому setup key не попадает в логи веб-сервера.
package jetboxlink

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	// UniversalLinkHost — домен Кабинета.
	UniversalLinkHost = "box.jet-net.net"
	// UniversalLinkPath — путь ссылки (`/connect` и `/connect/*`).
	UniversalLinkPath = "/connect"
	// CustomScheme — своя схема; на компьютере это основной вход.
	CustomScheme = "jetbox"
	// DefaultManagementURL — страховка для старых ссылок без `m=`.
	DefaultManagementURL = "https://netbird.jet-net.net"
)

// Payload — полезная нагрузка ссылки.
type Payload struct {
	// SetupKey: 36 символов, валидный UUID. Целиком нигде не показывается.
	SetupKey string
	// ManagementURL всегда https, без хвостового слэша.
	ManagementURL string
	// ExitNode — Network ID маршрута-выхода коробки, пусто если не прислан.
	ExitNode string
	// BoxName — человекочитаемое имя коробки, пусто если не прислано.
	BoxName string
}

// Outcome — исход разбора.
type Outcome int

const (
	// NotOurs — не наша ссылка.
	NotOurs Outcome = iota
	// Broken — ссылка наша, но испорчена: «Ссылка повреждена», в сеть не идём.
	Broken
	// OK — ссылка наша и разобрана.
	OK
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// MaskedKey — маска ключа для экрана и логов: видны только последние 4 символа.
func (p Payload) MaskedKey() string {
	if len(p.SetupKey) <= 4 {
		return "••••"
	}
	return strings.Repeat("•", 8) + "…" + p.SetupKey[len(p.SetupKey)-4:]
}

// DisplayName — что показать клиенту как имя его коробки.
func (p Payload) DisplayName() string {
	if p.BoxName != "" {
		return p.BoxName
	}
	if p.ExitNode != "" {
		return p.ExitNode
	}
	if u, err := url.Parse(p.ManagementURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return p.ManagementURL
}

// BoxIdentity — признак «та же коробка»: management + выход (§4.2).
func (p Payload) BoxIdentity() string {
	host := strings.ToLower(p.ManagementURL)
	if u, err := url.Parse(p.ManagementURL); err == nil && u.Hostname() != "" {
		host = strings.ToLower(u.Hostname())
	}
	return host + "|" + p.ExitNode
}

// Parse разбирает ссылку: `jetbox://connect#…` или `https://box.jet-net.net/connect#…`.
func Parse(raw string) (Payload, Outcome) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !isJetBoxURL(u) {
		return Payload{}, NotOurs
	}

	// RawFragment есть, только если фрагмент нельзя восстановить из Fragment
	// без потерь; EscapedFragment отдаёт исходную закодированную строку.
	// Декодируем сами, по одному разу на значение — как percentEncodedFragment в iOS.
	frag := u.EscapedFragment()
	if frag == "" {
		return Payload{}, NotOurs
	}

	params := parameters(frag)

	key := params["k"]
	if key == "" {
		return Payload{}, NotOurs
	}
	if !IsValidSetupKey(key) {
		return Payload{}, Broken
	}

	mgmt, ok := normalizedManagement(params["m"])
	if !ok {
		return Payload{}, Broken
	}

	return Payload{
		SetupKey:      key,
		ManagementURL: mgmt,
		ExitNode:      params["e"],
		BoxName:       params["n"],
	}, OK
}

// IsValidSetupKey — формат NetBird: ровно 36 символов и валидный UUID.
func IsValidSetupKey(key string) bool {
	return len(key) == 36 && uuidRe.MatchString(key)
}

func isJetBoxURL(u *url.URL) bool {
	switch strings.ToLower(u.Scheme) {
	case CustomScheme:
		// jetbox://connect#… — хост `connect`. Встречается и jetbox:connect#…
		// (без слэшей), его Windows иногда отдаёт как opaque.
		if strings.EqualFold(u.Host, "connect") {
			return true
		}
		return strings.EqualFold(strings.TrimSuffix(u.Opaque, "/"), "connect")
	case "https":
		if !strings.EqualFold(u.Hostname(), UniversalLinkHost) {
			return false
		}
		path := strings.ToLower(u.Path)
		return path == UniversalLinkPath || strings.HasPrefix(path, UniversalLinkPath+"/")
	}
	return false
}

// parameters разбирает `k=…&m=…&e=…`. Имена регистронезависимы, значения
// percent-декодируются, `+` остаётся плюсом, неизвестные параметры игнорируются.
func parameters(fragment string) map[string]string {
	result := map[string]string{}
	for _, pair := range strings.Split(fragment, "&") {
		if pair == "" {
			continue
		}
		name, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, dup := result[name]; dup {
			continue
		}
		if dec, err := url.PathUnescape(value); err == nil {
			value = dec
		}
		result[name] = strings.TrimSpace(value)
	}
	return result
}

// normalizedManagement: только https, обязателен хост, хвостовой слэш срезаем.
// Пустое значение и отсутствие параметра — дефолт приложения.
func normalizedManagement(value string) (string, bool) {
	if value == "" {
		return DefaultManagementURL, true
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
		return "", false
	}
	normalized := "https://" + strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		if _, err := strconv.Atoi(port); err != nil {
			return "", false
		}
		normalized += ":" + port
	}
	if p := u.Path; p != "" && p != "/" {
		normalized += strings.TrimSuffix(p, "/")
	}
	return normalized, true
}
