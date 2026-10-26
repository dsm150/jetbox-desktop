//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// systemLang — первый язык из «Язык и регион» (AppleLanguages).
func systemLang() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return "en"
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.Trim(strings.TrimSpace(line), `",()`)
		if line != "" {
			return normalizeLang(line)
		}
	}
	return "en"
}
