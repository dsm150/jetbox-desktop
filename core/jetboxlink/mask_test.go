package jetboxlink

import (
	"encoding/json"
	"os"
	"testing"
)

// Общие векторы маски management: те же гоняют служба и телефоны.
func TestAllowedManagementVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/mgmt-mask.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Suffix string `json:"suffix"`
		Cases  []struct {
			M   string `json:"m"`
			OK  bool   `json:"ok"`
			Why string `json:"why"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Suffix != ManagementSuffix {
		t.Fatalf("суффикс в векторах %q, в коде %q", v.Suffix, ManagementSuffix)
	}
	if len(v.Cases) < 30 {
		t.Fatalf("векторов %d — файл обрезан?", len(v.Cases))
	}
	for _, c := range v.Cases {
		if got := AllowedManagement(c.M); got != c.OK {
			t.Errorf("%q (%s): получили %v, ждали %v", c.M, c.Why, got, c.OK)
		}
	}
	if !AllowedManagement(DefaultManagementURL) {
		t.Error("DefaultManagementURL вне маски")
	}
}
