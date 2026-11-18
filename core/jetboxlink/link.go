// Package jetboxlink разбирает ссылку подключения из Кабинета.
//
// Контракт формата — Antigravity/JetBox_iOS/LINK-CONTRACT.md (ред. 1.1).
// Это перенос JetBoxLink.swift из iOS-клиента один в один: те же правила,
// те же исходы. Ключ и параметры живут во ФРАГМЕНТЕ (после `#`), фрагмент
// не уходит на сервер, поэтому setup key не попадает в логи веб-сервера.
package jetboxlink

import (
	"crypto/sha256"
	"encoding/hex"
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
	// DeviceToken — токен устройства из облачной ссылки Кабинета (`t=`): свой на
	// каждое устройство, «Новая ссылка» в Кабинете выдаёт новый. Секрет: целиком
	// не показывается и не хранится; в удостоверение не входит (LINK-CONTRACT 1.3 §2.11).
	DeviceToken string
	// Platform — кабинетный параметр `p` («что подключаем», например windows).
	// На поведение приложения не влияет (LINK-CONTRACT §2.8).
	Platform string
	// NoDefaultExit — `x=0` (LINK-CONTRACT 1.2 §2.10): после подключения выход не включать
	// и не выбирать; любое другое значение или отсутствие — обычный выбор выхода (§4.3).
	NoDefaultExit bool
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
	// Foreign — ссылка наша по виду, но management (`m`) вне маски *ManagementSuffix
	// (LINK-CONTRACT §2.3, план 1.0.6): «Ссылка ведёт не на сервер JetBox» до любого
	// действия и до вопроса о замене, в службу не идём.
	Foreign
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

// BoxIdentity — удостоверение подключения (LINK-CONTRACT 1.3 §2.11, общие векторы —
// testdata/identity.json) для вопроса о замене подключения:
//   - есть выход `e` — `хост(m)|e|n` (без n — `хост|e|`): облачные абоненты делят один e
//     (cl-…), различает их n; «Новая ссылка» той же коробки n не меняет;
//   - иначе есть имя `n` — `хост(m)|n:<n>` (старые ссылки без e);
//   - иначе — `хост(m)|`.
//
// Хост — в нижнем регистре, без порта. Токен устройства `t` не входит: он меняется при
// «Новой ссылке». Сравнивать удостоверения — SameIdentity (старые записи без n).
// Новая ли личность устройства (ключ WireGuard), решает не она, а KeyFingerprint.
func (p Payload) BoxIdentity() string {
	f := p.IdentityFields()
	switch {
	case f.Exit != "":
		return f.Host + "|" + f.Exit + "|" + f.Name
	case f.Name != "":
		return f.Host + "|n:" + f.Name
	}
	return f.Host + "|"
}

// IdentityFields — части удостоверения: хост management, выход e, имя n. Known=false —
// запись, по которой судить нельзя (облако 1.0.5 `хост|t:…`: e и n неизвестны).
type IdentityFields struct {
	Host, Exit, Name string
	Known            bool
}

// IdentityFields — части удостоверения ссылки.
func (p Payload) IdentityFields() IdentityFields {
	host := strings.ToLower(p.ManagementURL)
	if u, err := url.Parse(p.ManagementURL); err == nil && u.Hostname() != "" {
		host = strings.ToLower(u.Hostname())
	}
	host = strings.TrimSuffix(host, ".") // FQDN с точкой — тот же хост
	return IdentityFields{Host: host, Exit: p.ExitNode, Name: p.BoxName, Known: true}
}

// ParseIdentity — части сохранённого удостоверения в любом формате, что писали
// приложения: `хост|e|n` (1.0.6), `хост|e` (до 1.0.6 — без n), `хост|n:<n>`, `хост|`,
// `хост|t:<отпечаток>` (облако 1.0.5 — неизвестно). n может содержать «|».
func ParseIdentity(id string) IdentityFields {
	host, rest, _ := strings.Cut(id, "|")
	f := IdentityFields{Host: strings.TrimSuffix(host, "."), Known: true}
	switch {
	case rest == "":
	case strings.HasPrefix(rest, "n:"):
		f.Name = strings.TrimPrefix(rest, "n:")
	case strings.HasPrefix(rest, "t:"):
		f.Known = false
	default:
		f.Exit, f.Name, _ = strings.Cut(rest, "|")
	}
	return f
}

// SharedExit — выход общий для многих абонентов (облако: префикс `cl-`, LINK-CONTRACT
// 1.3 §2.11): по нему одному подключения не различить.
func SharedExit(e string) bool { return strings.HasPrefix(e, "cl-") }

// SameIdentity — запись и ссылка — одно подключение (без вопроса о замене):
// хост и e совпадают; n сравнивается (с учётом регистра), если он есть у обоих; если n
// нет у записи (старый формат `хост|e`) или у ссылки, сравнение без n — только для
// своего e коробки: общий e (cl-…) без n — «неизвестно», значит, вопрос. Без e — по
// хосту и n (`хост|n:<n>`, `хост|`). Запись облака 1.0.5 (`хост|t:…`) — неизвестно.
func SameIdentity(stored, link IdentityFields) bool {
	if !stored.Known || !link.Known || stored.Host != link.Host || stored.Exit != link.Exit {
		return false
	}
	if stored.Exit == "" || (stored.Name != "" && link.Name != "") {
		return stored.Name == link.Name
	}
	return !SharedExit(stored.Exit)
}

// KeyFingerprint — отпечаток setup key: по нему отличаем «та же ссылка» от «новая
// ссылка», не храня сам ключ. Как JetBoxIdentity.fingerprint(of:) в iOS: регистр
// UUID не важен.
func (p Payload) KeyFingerprint() string {
	return Fingerprint(strings.ToLower(p.SetupKey))
}

// Fingerprint — hex первых 16 байт SHA-256 строки (32 символа), как secretFingerprint
// в Android. Регистр не меняет: setup key приводит к нижнему регистру KeyFingerprint.
func Fingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:16])
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

	// Маска — к заданному m как есть (после percent-decoding), до нормализации: та
	// пропускает путь, а маска его запрещает. Пустой m — DefaultManagementURL, он в маске.
	if m := params["m"]; m != "" && !AllowedManagement(m) {
		return Payload{}, Foreign
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
		DeviceToken:   params["t"],
		Platform:      params["p"],
		NoDefaultExit: params["x"] == "0",
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
	// Одна завершающая точка FQDN срезается: в службу уходит адрес без неё (находка Android).
	normalized := "https://" + strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
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
