package jetboxlink

import (
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
		"jetbox://connect#k=" + key + "&m=https://Mgmt.Example.NET/":      "https://mgmt.example.net",
		"jetbox://connect#k=" + key + "&m=https://mgmt.example.net:33073": "https://mgmt.example.net:33073",
	}
	for raw, want := range cases {
		if got := payload(t, raw).ManagementURL; got != want {
			t.Errorf("%q: %q, want %q", raw, got, want)
		}
	}
}

func TestParameterOrderAndUnknownParametersDoNotMatter(t *testing.T) {
	p := payload(t, "jetbox://connect#zz=1&e=node&m=https://mgmt.example.net&k="+key+"&v=9")
	if p.ExitNode != "node" || p.ManagementURL != "https://mgmt.example.net" {
		t.Fatalf("got %+v", p)
	}
}

func TestCabinetParameterPIsIgnored(t *testing.T) {
	base := "https://box.jet-net.net/connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=box-ivanov-office"
	withP := payload(t, base+"&p=desktop")
	withoutP := payload(t, base)
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
	for _, raw := range []string{"https://box.jet-net.net/connect#m=https://mgmt.example.net", "jetbox://connect#k="} {
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

func TestNonHttpsManagementIsBroken(t *testing.T) {
	for _, raw := range []string{
		"jetbox://connect#k=" + key + "&m=http://mgmt.example.net",
		"jetbox://connect#k=" + key + "&m=mgmt.example.net",
		"jetbox://connect#k=" + key + "&m=javascript:alert(1)",
	} {
		if outcome(raw) != Broken {
			t.Errorf("%q: want Broken", raw)
		}
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
	if got := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.example.net").DisplayName(); got != "mgmt.example.net" {
		t.Error(got)
	}
}

func TestBoxIdentityDistinguishesBoxes(t *testing.T) {
	a := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.example.net&e=one")
	b := payload(t, "jetbox://connect#k="+key+"&m=https://mgmt.example.net&e=two")
	sameAsA := payload(t, "jetbox://connect#k="+key+"&m=https://MGMT.example.net/&e=one&n=x")
	if a.BoxIdentity() == b.BoxIdentity() || a.BoxIdentity() != sameAsA.BoxIdentity() {
		t.Fatal(a.BoxIdentity(), b.BoxIdentity(), sameAsA.BoxIdentity())
	}
}
