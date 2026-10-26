package main

import "strings"

var trayRU = map[string]string{
	"tooltip":    "JetBox",
	"on":         "JetBox: под защитой",
	"off":        "JetBox: защита выключена",
	"connecting": "JetBox: подключаем…",
	"exitDown":   "JetBox: выход не отвечает",
	"nobox":      "JetBox не подключён",
	"service":    "Служба JetBox не отвечает",
	"turnOn":     "Включить защиту",
	"turnOff":    "Выключить защиту",
	"open":       "Открыть JetBox",
	"quit":       "Выход",
}

var trayEN = map[string]string{
	"tooltip":    "JetBox",
	"on":         "JetBox: protected",
	"off":        "JetBox: protection is off",
	"connecting": "JetBox: connecting…",
	"exitDown":   "JetBox: the exit is not responding",
	"nobox":      "No JetBox connected",
	"service":    "JetBox service is not responding",
	"turnOn":     "Turn protection on",
	"turnOff":    "Turn protection off",
	"open":       "Open JetBox",
	"quit":       "Quit",
}

func tr(lang, key string) string {
	if lang == "ru" {
		return trayRU[key]
	}
	return trayEN[key]
}

// normalizeLang: русский — "ru", всё остальное — "en".
func normalizeLang(tag string) string {
	if strings.HasPrefix(strings.ToLower(tag), "ru") {
		return "ru"
	}
	return "en"
}
