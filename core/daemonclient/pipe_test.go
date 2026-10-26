package daemonclient

import "testing"

func TestPipePath(t *testing.T) {
	got, err := pipePath("npipe://jetbox")
	if err != nil || got != `\\.\pipe\ProtectedPrefix\Administrators\jetbox` {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"npipe://", `npipe://..\netbird`, "npipe://a/b", `npipe://\\evil\pipe\x`} {
		if p, err := pipePath(bad); err == nil {
			t.Errorf("pipePath(%q) = %q, ждали ошибку", bad, p)
		}
	}
}
