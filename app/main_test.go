package main

import (
	"strings"
	"testing"
)

func TestLinkFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--autostart"}, ""},
		{[]string{"jetbox://connect?d=abc"}, "jetbox://connect?d=abc"},
		{[]string{"  JetBox://connect?d=abc "}, "JetBox://connect?d=abc"},
		{[]string{"--autostart", "jetbox:connect"}, "jetbox:connect"},
		{[]string{"https://jet-net.net"}, ""},
	}
	for _, c := range cases {
		if got := linkFromArgs(c.args); got != c.want {
			t.Errorf("linkFromArgs(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestHasArg(t *testing.T) {
	if !hasArg([]string{"x", autostartFlag}, autostartFlag) {
		t.Error("--autostart not found")
	}
	if hasArg([]string{"--autostart=1"}, autostartFlag) {
		t.Error("--autostart=1 must not match")
	}
}

func TestRedactLinks(t *testing.T) {
	const key = "3F2504E0-4F89-41D3-9A0C-0305E82C3301"
	cases := []struct{ in, want string }{
		{"no links here", "no links here"},
		{`args=["jetbox://connect#k=` + key + `" "--autostart"]`,
			`args=["<link ok key=••••••••…3301 len=55>" "--autostart"]`},
		{"JetBox:connect#k=" + key + " tail",
			"<link ok key=••••••••…3301 len=53> tail"},
		{"https://box.jet-net.net/connect#k=" + key,
			"<link ok key=••••••••…3301 len=70>"},
		{"jetbox://connect#k=short", "<link broken len=24>"},
		{"a jetbox:x b jetbox://connect#k=" + key + "\n",
			"a <link not-ours len=8> b <link ok key=••••••••…3301 len=55>\n"},
	}
	for _, c := range cases {
		got := redactLinks(c.in)
		if got != c.want {
			t.Errorf("redactLinks(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.Contains(strings.ToUpper(got), "3F2504E0") {
			t.Errorf("key leaked: %q", got)
		}
	}
}

func TestRedactWriter(t *testing.T) {
	var sb strings.Builder
	w := redactWriter{&sb}
	in := "link jetbox://connect#k=3F2504E0-4F89-41D3-9A0C-0305E82C3301\n"
	n, err := w.Write([]byte(in))
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	if strings.Contains(sb.String(), "3F2504E0") {
		t.Errorf("key leaked: %q", sb.String())
	}
}
