package jetboxlink

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Кейсы перенесены из JetBox_iOS/NetBirdTests/JetBoxLinkTests.swift.

const key = "0EF79C2F-1A34-4B21-9C0D-1BF529332998"

func payload(t *testing.T, raw string) Payload {
	t.Helper()
	p, out := Parse(raw)
	if out != OK {
		t.Fatalf("Parse(%q) = %v, want OK", raw, out)
	}
	return p
}

func outcome(raw string) Outcome {
	_, out := Parse(raw)
	return out
}

func TestUniversalLinkFullContract(t *testing.T) {
	p := payload(t, "https://box.jet-net.net/connect#k="+key+"&m=https%3A%2F%2Fnetbird.jet-net.net&e=box-ivanov-office")
	if p.SetupKey != key || p.ManagementURL != "https://netbird.jet-net.net" || p.ExitNode != "box-ivanov-office" || p.BoxName != "" {
		t.Fatalf("got %+v", p)
	}
}

func TestUniversalLinkWithHumanName(t *testing.T) {
	p := payload(t, "https://box.jet-net.net/connect#k="+key+"&e=box&n=%D0%9E%D1%84%D0%B8%D1%81%20%D0%BD%D0%B0%20%D0%A2%D0%B2%D0%B5%D1%80%D1%81%D0%BA%D0%BE%D0%B9")
	if p.BoxName != "Офис на Тверской" || p.DisplayName() != "Офис на Тверской" {
		t.Fatalf("got %+v", p)
	}
}

func TestUniversalLinkTrailingSlashAndSubpath(t *testing.T) {
	payload(t, "https://box.jet-net.net/connect/#k="+key)
	payload(t, "https://box.jet-net.net/connect/ios#k="+key)
}

func TestHostAndSchemeAreCaseInsensitive(t *testing.T) {
	payload(t, "HTTPS://BOX.JET-NET.NET/connect#k="+key)
	payload(t, "JETBOX://CONNECT#k="+key)
}

func TestForeignHostIsNotOurs(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/connect#k=" + key,
		"https://box.jet-net.net.evil.com/connect#k=" + key,
	} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestForeignPathIsNotOurs(t *testing.T) {
	for _, raw := range []string{
		"https://box.jet-net.net/#k=" + key,
		"https://box.jet-net.net/connected#k=" + key,
	} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestCustomSchemeMatchesUniversalLink(t *testing.T) {
	frag := "#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=box"
	a := payload(t, "https://box.jet-net.net/connect"+frag)
	b := payload(t, "jetbox://connect"+frag)
	if a != b {
		t.Fatalf("%+v != %+v", a, b)
	}
}

// Windows может отдать ссылку приложению со слэшем после хоста или без
// слэшей вовсе — всё это та же ссылка.
func TestCustomSchemeWindowsVariants(t *testing.T) {
	payload(t, "jetbox://connect/#k="+key)
	payload(t, "jetbox:connect#k="+key)
	payload(t, "  jetbox://connect#k="+key+"  ")
}

func TestForeignSchemeIsNotOurs(t *testing.T) {
	for _, raw := range []string{"netbird://connect#k=" + key, "jetbox://disconnect#k=" + key} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestManagementDefaultsWhenMissingOrEmpty(t *testing.T) {
	for _, raw := range []string{"jetbox://connect#k=" + key, "jetbox://connect#k=" + key + "&m="} {
		if got := payload(t, raw).ManagementURL; got != DefaultManagementURL {
			t.Errorf("%q: %q", raw, got)
		}
	}
}

func TestManagementNormalisation(t *testing.T) {
	cases := map[string]string{
		"jetbox://connect#k=" + key + "&m=https://Mgmt.Jet-Net.NET/":      "https://mgmt.jet-net.net",
		"jetbox://connect#k=" + key + "&m=https://mgmt.jet-net.net:33073": "https://mgmt.jet-net.net:33073",
	}
	for raw, want := range cases {
		if got := payload(t, raw).ManagementURL; got != want {
			t.Errorf("%q: %q, want %q", raw, got, want)
		}
	}
}

func TestParameterOrderAndUnknownParametersDoNotMatter(t *testing.T) {
	p := payload(t, "jetbox://connect#zz=1&e=node&m=https://mgmt.jet-net.net&k="+key+"&v=9")
	if p.ExitNode != "node" || p.ManagementURL != "https://mgmt.jet-net.net" {
		t.Fatalf("got %+v", p)
	}
}

func TestCabinetParameterPIsIgnored(t *testing.T) {
	base := "https://box.jet-net.net/connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=box-ivanov-office"
	withP := payload(t, base+"&p=desktop")
	withoutP := payload(t, base)
	if withP.Platform != "desktop" || withoutP.Platform != "" {
		t.Fatalf("p: %q %q", withP.Platform, withoutP.Platform)
	}
	withP.Platform = ""
	if withP != withoutP || withP.BoxIdentity() != withoutP.BoxIdentity() {
		t.Fatalf("%+v != %+v", withP, withoutP)
	}
}

func TestParameterNamesAreCaseInsensitive(t *testing.T) {
	if got := payload(t, "jetbox://connect#K="+key+"&E=node").ExitNode; got != "node" {
		t.Fatal(got)
	}
}

func TestExitNodeKeepsCaseAndSpaces(t *testing.T) {
	if got := payload(t, "jetbox://connect#k="+key+"&e=Box%20Ivanov").ExitNode; got != "Box Ivanov" {
		t.Fatal(got)
	}
	if got := payload(t, "jetbox://connect#k="+key+"&e=BoxIvanov").ExitNode; got != "BoxIvanov" {
		t.Fatal(got)
	}
}

func TestPlusIsNotASpaceInFragment(t *testing.T) {
	if got := payload(t, "jetbox://connect#k="+key+"&e=a+b").ExitNode; got != "a+b" {
		t.Fatal(got)
	}
}

func TestEqualsSignInsideValueSurvives(t *testing.T) {
	if got := payload(t, "jetbox://connect#k="+key+"&n=a=b").BoxName; got != "a=b" {
		t.Fatal(got)
	}
}

// %2520 в имени — это литерал «%20», декодируем один раз.
func TestSingleDecode(t *testing.T) {
	if got := payload(t, "jetbox://connect#k="+key+"&n=a%2520b").BoxName; got != "a%20b" {
		t.Fatal(got)
	}
}

func TestMissingFragmentIsNotOurs(t *testing.T) {
	for _, raw := range []string{"https://box.jet-net.net/connect", "https://box.jet-net.net/connect#"} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestMissingKeyIsNotOurs(t *testing.T) {
	for _, raw := range []string{"https://box.jet-net.net/connect#m=https://mgmt.jet-net.net", "jetbox://connect#k="} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestMalformedKeyIsBroken(t *testing.T) {
	for _, raw := range []string{
		"jetbox://connect#k=not-a-uuid",
		"jetbox://connect#k=" + key + "X",
		"jetbox://connect#k=" + key[:35],
		"jetbox://connect#k=" + strings.Repeat("Z", 36),
	} {
		if outcome(raw) != Broken {
			t.Errorf("%q: want Broken", raw)
		}
	}
}

// Маска (LINK-CONTRACT §2.3) проверяется до нормализации: не https, без схемы, мусор —
// «не на сервер JetBox», а не «повреждена».
func TestNonHttpsManagementIsForeign(t *testing.T) {
	for _, raw := range []string{
		"jetbox://connect#k=" + key + "&m=http://mgmt.jet-net.net",
		"jetbox://connect#k=" + key + "&m=mgmt.jet-net.net",
		"jetbox://connect#k=" + key + "&m=javascript:alert(1)",
	} {
		if outcome(raw) != Foreign {
			t.Errorf("%q: want Foreign", raw)
		}
	}
}

// m вне маски — Foreign до любого действия; без m — management по умолчанию (в маске).
func TestManagementMaskInParse(t *testing.T) {
	if outcome("jetbox://connect#k="+key) != OK {
		t.Error("без m — OK")
	}
	for _, m := range []string{
		"https://evil.example",
		"https://netbird.jet-net.net/api",      // путь: normalizedManagement бы пропустил
		"https%3A%2F%2Fevil.example",           // после percent-decoding — чужой
		"https://jet-net.net",                  // сам домен
		"https://netbird.jet-net.net.evil.com", // наш хост в начале чужого
		"https://1.2.3.4",
		"https://netbird.jet-net.net:",
	} {
		raw := "jetbox://connect#k=" + key + "&m=" + m
		if got := outcome(raw); got != Foreign {
			t.Errorf("%q: %v, want Foreign", raw, got)
		}
	}
	// Ключ испорчен — Broken раньше маски.
	if got := outcome("jetbox://connect#k=bad&m=https://evil.example"); got != Broken {
		t.Errorf("испорченный ключ: %v, want Broken", got)
	}
	if p := payload(t, "jetbox://connect#k="+key+"&m=https%3A%2F%2Fbox2.jet-net.net%3A33073"); p.ManagementURL != "https://box2.jet-net.net:33073" {
		t.Errorf("закодированный m в маске: %q", p.ManagementURL)
	}
}

func TestGarbageFragmentDoesNotCrash(t *testing.T) {
	for _, raw := range []string{"jetbox://connect#&&&===", "jetbox://connect#k", "jetbox://connect#%zz"} {
		if outcome(raw) != NotOurs {
			t.Errorf("%q: want NotOurs", raw)
		}
	}
}

func TestKeyIsNeverShownInFull(t *testing.T) {
	masked := payload(t, "jetbox://connect#k="+key).MaskedKey()
	if strings.Contains(masked, key) || !strings.HasSuffix(masked, key[32:]) || strings.Count(masked, "•") != 8 {
		t.Fatal(masked)
	}
}

func TestDisplayNameFallsBackFromNameToExitToHost(t *testing.T) {
	if got := payload(t, "jetbox://connect#k="+key+"&n=%D0%94%D0%BE%D0%BC&e=node").DisplayName(); got != "Дом" {
		t.Error(got)
	}
	if got := payload(t, "jetbox://connect#k="+key+"&e=node").DisplayName(); got != "node" {
		t.Error(got)
	}
	if got := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.jet-net.net").DisplayName(); got != "mgmt.jet-net.net" {
		t.Error(got)
	}
}

func TestBoxIdentityDistinguishesBoxes(t *testing.T) {
	a := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.jet-net.net&e=one")
	b := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.jet-net.net&e=two")
	sameAsA := payload(t, "jetbox://connect#k="+key+"&m=https://MGMT.jet-net.net/&e=one&n=x")
	// Ссылка без n и с n той же коробки (свой e) — одно подключение (сравнение без n).
	if a.BoxIdentity() == b.BoxIdentity() || !SameIdentity(a.IdentityFields(), sameAsA.IdentityFields()) ||
		SameIdentity(a.IdentityFields(), b.IdentityFields()) {
		t.Fatal(a.BoxIdentity(), b.BoxIdentity(), sameAsA.BoxIdentity())
	}
}

// Облачная ссылка Кабинета: `#k=…&m=…&n=…&p=windows&t=<токен устройства>`, выхода нет.
const cloudLink = "https://box.jet-net.net/connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&n=%D0%98%D0%B2%D0%B0%D0%BD%D0%BE%D0%B2&p=windows&t=dev-token-aaaa"

func TestCloudLinkDeviceTokenAndPlatform(t *testing.T) {
	p := payload(t, cloudLink)
	if p.DeviceToken != "dev-token-aaaa" || p.Platform != "windows" || p.BoxName != "Иванов" || p.ExitNode != "" {
		t.Fatalf("got %+v", p)
	}
}

func TestBoxIdentityRules(t *testing.T) {
	cloud := payload(t, cloudLink)
	otherCloud := payload(t, strings.Replace(cloudLink, "dev-token-aaaa", "dev-token-bbbb", 1))
	box := payload(t, cloudLink+"&e=box-ivanov")
	old := payload(t, "jetbox://connect#k="+key+"&m=https://NetBird.jet-net.net")
	named := payload(t, "jetbox://connect#k="+key+"&m=https://NetBird.jet-net.net&n=Dom")

	// t в удостоверение не входит: «Новая ссылка» того же абонента — то же удостоверение.
	if got, want := cloud.BoxIdentity(), "netbird.jet-net.net|n:Иванов"; got != want {
		t.Fatalf("облако без e: %q, want %q", got, want)
	}
	if strings.Contains(cloud.BoxIdentity(), "dev-token") || cloud.BoxIdentity() != otherCloud.BoxIdentity() {
		t.Fatal("токен устройства в удостоверении")
	}
	if box.BoxIdentity() != "netbird.jet-net.net|box-ivanov|Иванов" {
		t.Fatalf("e и n: %q", box.BoxIdentity())
	}
	if old.BoxIdentity() != "netbird.jet-net.net|" || named.BoxIdentity() != "netbird.jet-net.net|n:Dom" {
		t.Fatalf("старые ссылки: %q %q", old.BoxIdentity(), named.BoxIdentity())
	}
}

// Общие векторы удостоверения (LINK-CONTRACT 1.3 §2.11, источник — iOS), раздел cases.
// Раздел compare гоняет core/flow (SameConnection/sameBox).
func TestBoxIdentityVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cases []struct{ Link, Identity, About string } `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) < 14 {
		t.Fatalf("случаев %d — файл обрезан?", len(v.Cases))
	}
	for _, c := range v.Cases {
		p := payload(t, c.Link)
		if got := p.BoxIdentity(); got != c.Identity {
			t.Errorf("%s: %q, want %q", c.About, got, c.Identity)
		}
		// Разбор строки обратно даёт те же части, что у ссылки.
		if got, want := ParseIdentity(p.BoxIdentity()), p.IdentityFields(); got != want {
			t.Errorf("%s: ParseIdentity %+v, want %+v", c.About, got, want)
		}
	}
}

func TestParseIdentityFormats(t *testing.T) {
	cases := map[string]IdentityFields{
		"h|sv-x|Офис":  {Host: "h", Exit: "sv-x", Name: "Офис", Known: true},
		"h|sv-x|a|b":   {Host: "h", Exit: "sv-x", Name: "a|b", Known: true},
		"h|sv-x|":      {Host: "h", Exit: "sv-x", Known: true},
		"h|sv-x":       {Host: "h", Exit: "sv-x", Known: true}, // до 1.0.6
		"h|n:Дом":      {Host: "h", Name: "Дом", Known: true},
		"h|":           {Host: "h", Known: true},
		"h|t:0123abcd": {Host: "h"}, // облако 1.0.5 — неизвестно
		"":             {Known: true},
	}
	for id, want := range cases {
		if got := ParseIdentity(id); got != want {
			t.Errorf("%q: %+v, want %+v", id, got, want)
		}
	}
	// Запись облака 1.0.5 ни с чем не совпадает — даже с той же ссылкой без e и n.
	if SameIdentity(ParseIdentity("h|t:0123abcd"), ParseIdentity("h|")) {
		t.Error("хост|t:… совпал")
	}
	if !SharedExit("cl-exit") || !SharedExit("cl-eu") || SharedExit("sv-demo-exit") {
		t.Error("SharedExit")
	}
}

func TestKeyFingerprint(t *testing.T) {
	a := payload(t, "jetbox://connect#k="+key)
	upper := payload(t, "jetbox://connect#k="+strings.ToLower(key))
	other := payload(t, "jetbox://connect#k=1EF79C2F-1A34-4B21-9C0D-1BF529332998")
	fp := a.KeyFingerprint()
	if len(fp) != 32 || fp != upper.KeyFingerprint() || fp == other.KeyFingerprint() {
		t.Fatal(fp, upper.KeyFingerprint(), other.KeyFingerprint())
	}
	if strings.Contains(strings.ToLower(fp), strings.ToLower(key[len(key)-4:])) {
		t.Fatal("в отпечатке кусок ключа")
	}
	// Тот же алгоритм, что в iOS: SHA-256(lowercase(key)), первые 16 байт hex.
	if fp != "7a5d83e156ee9d5beb7073134aeb20ba" {
		t.Fatal(fp)
	}
}

func TestNoDefaultExit(t *testing.T) {
	base := "jetbox://connect#k=1EF79C2F-1A34-4B21-9C0D-1BF529332998&m=https%3A%2F%2Fnetbird.jet-net.net&t=tok"
	for raw, want := range map[string]bool{base + "&x=0": true, base + "&X=0": true, base + "&x=1": false, base: false} {
		p, out := Parse(raw)
		if out != OK || p.NoDefaultExit != want {
			t.Errorf("%s: %v %v", raw, out, p.NoDefaultExit)
		}
	}
}

// Одна завершающая точка FQDN срезается: в службу уходит адрес без неё, удостоверение
// с точкой = без точки (находка Android).
func TestManagementTrailingDot(t *testing.T) {
	p := payload(t, "jetbox://connect#k="+key+"&m=https%3A%2F%2FNetBird.jet-net.net.%3A33073&e=sv-x&n=X")
	if p.ManagementURL != "https://netbird.jet-net.net:33073" {
		t.Fatalf("в службу: %q", p.ManagementURL)
	}
	if p.BoxIdentity() != "netbird.jet-net.net|sv-x|X" {
		t.Fatalf("удостоверение: %q", p.BoxIdentity())
	}
	// Запись, сохранённая с точкой (до 1.0.6), — тот же хост.
	if !SameIdentity(ParseIdentity("netbird.jet-net.net.|sv-x|X"), p.IdentityFields()) {
		t.Fatal("удостоверение с точкой не совпало")
	}
}
