//go:build !windows && !darwin

package main

import "os"

func systemLang() string { return normalizeLang(os.Getenv("LANG")) }
