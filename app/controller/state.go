// Package controller — экраны приложения JetBox поверх core/flow, без Wails.
package controller

// Screen — какой экран показывает окно.
type Screen string

const (
	ScreenNoBox          Screen = "nobox"
	ScreenReady          Screen = "ready"
	ScreenSwitch         Screen = "switch"
	ScreenProgress       Screen = "progress"
	ScreenHome           Screen = "home"
	ScreenChooseExit     Screen = "choose-exit"
	ScreenOtherVPN       Screen = "other-vpn"
	ScreenServiceMissing Screen = "service-missing"
	ScreenError          Screen = "error"
)

// Действие, на котором случился отказ (State.ErrorOp): по нему окно выбирает
// заголовок ошибки, по ErrorKind — текст.
const (
	OpConnect    = "connect"     // «Подключить» по ссылке (и разбор ссылки)
	OpReconnect  = "reconnect"   // щит: включить защиту
	OpDisconnect = "disconnect"  // щит: выключить защиту
	OpChooseExit = "choose-exit" // выбор выхода
	OpForget     = "forget"      // «Отвязать компьютер»
	OpUninstall  = "uninstall"   // «Удалить JetBox…»
)

// Panel — панель дома.
type Panel struct {
	ServiceUp bool `json:"serviceUp"`
	// ServiceState — состояние службы как есть: Idle, Connecting, Connected,
	// NeedsLogin, LoginFailed, SessionExpired; пусто — служба не отвечает.
	ServiceState string `json:"serviceState"`
	// Connected — «Под защитой»: служба и management Connected, выхода нет или его пир поднят.
	Connected bool  `json:"connected"`
	Relayed   bool  `json:"relayed"`
	SinceUnix int64 `json:"sinceUnix"` // «в сети с» по нашим часам; 0 — не в сети
	RxBytes   int64 `json:"rxBytes"`
	TxBytes   int64 `json:"txBytes"`
	HomeMs    int64 `json:"homeMs"`    // -1 — не мерили или выхода нет (плитка скрыта)
	NeedsLink bool  `json:"needsLink"` // служба не зарегистрирована — нужна ссылка заново
	// Exit — выход, включённый в службе сейчас; "" — выхода нет или сети неизвестны.
	Exit string `json:"exit"`
	// NoExit — служба Connected, сети известны, выхода нет: «Без выхода — только сети».
	NoExit bool `json:"noExit"`
	// ExitDown — выход выбран, а его пир не поднялся за exitGrace: «Выход не отвечает».
	ExitDown bool `json:"exitDown"`
	// CanChangeExit — выходов больше одного: в подробностях ссылка «Сменить».
	CanChangeExit bool `json:"canChangeExit"`
}

// ExitCandidate — выход на экране «Выберите выход».
type ExitCandidate struct {
	ID     string `json:"id"`
	Active bool   `json:"active"` // ядро включило его сейчас
}

// State — всё, что рисует окно и трей. Ключа здесь нет — только маска.
type State struct {
	// Seq растёт с каждым изменением; окно отбрасывает состояние со Seq меньше уже показанного.
	Seq         uint64          `json:"seq"`
	Lang        string          `json:"lang"`
	Version     string          `json:"version"`
	Screen      Screen          `json:"screen"`
	Stage       string          `json:"stage"` // registering | connecting | choosing-exit
	BoxName     string          `json:"boxName"`
	LinkBoxName string          `json:"linkBoxName"`
	Host        string          `json:"host"`
	KeyMask     string          `json:"keyMask"`
	ExitName    string          `json:"exitName"`
	NoExit      bool            `json:"noExit"` // в коробке запомнено «без выхода — только сети»
	Candidates  []ExitCandidate `json:"candidates"`
	ErrorKind   string          `json:"errorKind"`
	ErrorOp     string          `json:"errorOp"` // Op* — на каком действии отказ
	ErrorDetail string          `json:"errorDetail"`
	CanRetry    bool            `json:"canRetry"` // только при ожидающей ссылке: повтор — это Connect
	OtherVPN    string          `json:"otherVpn"`
	Busy        bool            `json:"busy"`
	Panel       Panel           `json:"panel"`
}
