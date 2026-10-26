//go:build windows

package main

import "golang.org/x/sys/windows"

func systemLang() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return "en"
	}
	return normalizeLang(langs[0])
}
