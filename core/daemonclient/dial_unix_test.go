//go:build !windows

package daemonclient

import "testing"

func TestDialRejectsUnknownScheme(t *testing.T) {
	if _, err := Dial("ftp://x"); err == nil {
		t.Fatal("ждали ошибку для ftp://")
	}
}

func TestDialTCPIsLazy(t *testing.T) {
	c, err := Dial("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}
