package flow

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storedIdentity — строка удостоверения записи с полями m, e, n (nil — поля нет) так,
// как её писали приложения: без n при e — старый формат `хост|e` (до 1.0.6).
func storedIdentity(t *testing.T, m string, e, n *string) string {
	t.Helper()
	u, err := url.Parse(m)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case e != nil && n != nil:
		return host + "|" + *e + "|" + *n
	case e != nil:
		return host + "|" + *e
	case n != nil:
		return host + "|n:" + *n
	}
	return host + "|"
}

// Общие векторы удостоверения, раздел compare (LINK-CONTRACT 1.3 §2.11, источник — iOS):
// запись против ссылки → same (без вопроса) | ask. Отпечатка ключа у записи нет — решает
// удостоверение (SameConnection), и то же отвечает SameBox (sameBox в Connect, OnLink).
func TestIdentityCompareVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "jetboxlink", "testdata", "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Compare []struct {
			Stored struct {
				M string  `json:"m"`
				E *string `json:"e"`
				N *string `json:"n"`
			} `json:"stored"`
			Link, Expect, About string
		} `json:"compare"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Compare) < 10 {
		t.Fatalf("сравнений %d — файл обрезан?", len(v.Compare))
	}
	for _, c := range v.Compare {
		b := Box{Identity: storedIdentity(t, c.Stored.M, c.Stored.E, c.Stored.N), ManagementURL: c.Stored.M}
		l := link(t, c.Link)
		want := c.Expect == "same"
		if got := SameBox(b, l); got != want {
			t.Errorf("%s: SameBox(%q, %q) = %v, want %s", c.About, b.Identity, l.BoxIdentity(), got, c.Expect)
		}
		if got := SameConnection(b, l); got != want {
			t.Errorf("%s: SameConnection без отпечатка = %v, want %s", c.About, got, c.Expect)
		}
	}
}

// Переход со старых записей в Connect, с отпечатком ключа (он решает раньше удостоверения,
// но ссылка с новым ключом — не «та же», дальше решает удостоверение).
func TestOldIdentityTransition(t *testing.T) {
	const cloudIvan = "jetbox://connect#k=" + key + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=cl-exit&n=JetBox%20%C2%B7%20ivan&t=jbd_a&x=0"
	const cloudPetr = "jetbox://connect#k=" + otherKey + "&m=https%3A%2F%2Fnetbird.jet-net.net&e=cl-exit&n=JetBox%20%C2%B7%20petr&t=jbd_b&x=0"

	t.Run("старый хост|cl-exit + ссылка другого абонента — вопрос", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		b := registeredBox(t, cloudIvan)
		b.Identity = "netbird.jet-net.net|cl-exit" // запись до 1.0.6
		_ = s.Save(b)
		_, err := f.Connect(context.Background(), link(t, cloudPetr), Options{}, nil)
		if KindOf(err) != KindOtherBox || hasCall(d.calls, "AddProfile") {
			t.Fatalf("err %v calls %v", err, d.calls)
		}
	})
	t.Run("старый хост|cl-exit + «Новая ссылка» того же абонента — тоже вопрос (общий e без n)", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		b := registeredBox(t, cloudPetr)
		b.Identity = "netbird.jet-net.net|cl-exit"
		_ = s.Save(b)
		if _, err := f.Connect(context.Background(), link(t, cloudIvan), Options{}, nil); KindOf(err) != KindOtherBox {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("старый хост|sv-demo-exit + та же коробка с n — без вопроса, новая личность", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		b := registeredBox(t, demoLink)
		b.Identity = "netbird.jet-net.net|sv-demo-exit"
		_ = s.Save(b)
		if _, err := f.Connect(context.Background(), link(t, reissuedDemoLink), Options{}, nil); err != nil {
			t.Fatal(err)
		}
		if !hasCall(d.calls, "AddProfile") {
			t.Fatalf("новый ключ — новая личность: %v", d.calls)
		}
		if got, _, _ := s.Load(); got.Identity != "netbird.jet-net.net|sv-demo-exit|Demo" {
			t.Fatalf("сохранять в новом формате: %q", got.Identity)
		}
	})
	t.Run("облако 1.0.5 хост|t:… — вопрос, не совпадает ни с чем", func(t *testing.T) {
		d := &fakeDaemon{status: "Connected", nets: demoNets}
		f, s := newFlow(t, d)
		b := registeredBox(t, cloudA)
		b.Identity = "netbird.jet-net.net|t:0123456789abcdef0123456789abcdef"
		_ = s.Save(b)
		if _, err := f.Connect(context.Background(), link(t, cloudB), Options{}, nil); KindOf(err) != KindOtherBox {
			t.Fatalf("err %v", err)
		}
		// Та же ссылка (тот же ключ) — отпечаток решает раньше удостоверения: без вопроса.
		d2 := &fakeDaemon{status: "Connected", nets: demoNets}
		f2, s2 := newFlow(t, d2)
		_ = s2.Save(b)
		if _, err := f2.Connect(context.Background(), link(t, cloudA), Options{}, nil); err != nil || hasCall(d2.calls, "AddProfile") {
			t.Fatalf("err %v calls %v", err, d2.calls)
		}
	})
}
