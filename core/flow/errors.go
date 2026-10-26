package flow

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Kind — вид отказа, по нему приложение выбирает текст (LINK-CONTRACT §4.4).
type Kind int

const (
	KindNone              Kind = iota
	KindUnknown                // «Не получилось подключиться»
	KindServiceMissing         // служба JetBox не отвечает — переустановить
	KindNoInternet             // нет интернета
	KindServerUnreachable      // management недоступен
	KindLinkExpired            // ключ протух / отозван / исчерпан
	KindLinkBroken             // ссылка повреждена (разбор)
	KindNeedsBrowserLogin      // сервер просит вход через браузер
	KindOtherVPN               // включён другой VPN (Detail — что именно)
	KindOtherBox               // ссылка другой коробки — нужно подтверждение (§4.2); пустой Detail — «другая JetBox»
	// KindNeedsLink — компьютер помнит коробку, но служба не зарегистрирована:
	// открыть ссылку подключения ещё раз.
	KindNeedsLink
)

var kindNames = [...]string{"None", "Unknown", "ServiceMissing", "NoInternet", "ServerUnreachable",
	"LinkExpired", "LinkBroken", "NeedsBrowserLogin", "OtherVPN", "OtherBox", "NeedsLink"}

func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("Kind(%d)", int(k))
	}
	return kindNames[k]
}

// Error — отказ сценария.
type Error struct {
	Kind   Kind
	Detail string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Detail, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Detail)
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf достаёт вид отказа из цепочки ошибок.
func KindOf(err error) Kind {
	if err == nil {
		return KindNone
	}
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return KindUnknown
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// classifyLogin сводит ошибку Login/Up службы к виду отказа. Тексты management апстрима
// не стабильны, поэтому смотрим и код, и подстроки. Порядок важен:
//   - «originally enrolled via SSO» — пир заведён через браузер, ключом не войти;
//   - InvalidArgument про setup-key — ключ в ссылке испорчен (формат);
//   - упоминание ключа или «couldn't add peer» — ключ протух/отозван/исчерпан;
//   - PermissionDenied/Unauthenticated — только про истёкший вход пира это ссылка;
//     прочие такие коды служба отдаёт сама на проверки прав, это не ссылка;
//   - «нет маршрута» приходит и с кодом Unavailable, но это отсутствие интернета.
//
// На деле служба набирает management с grpc.WithBlock без WithReturnConnectionError,
// и без интернета отдаёт лишь «context deadline exceeded» → ServerUnreachable;
// уточнение до NoInternet делает Flow через Config.Online.
func classifyLogin(err error) Kind {
	if err == nil {
		return KindNone
	}
	st, _ := status.FromError(err)
	msg := strings.ToLower(st.Message())
	if msg == "" {
		msg = strings.ToLower(err.Error())
	}
	code := st.Code()
	switch {
	case strings.Contains(msg, "originally enrolled via sso"):
		return KindNeedsBrowserLogin
	case code == codes.InvalidArgument && containsAny(msg, "setup-key", "setup key"):
		return KindLinkBroken
	case containsAny(msg, "setup key", "setup-key", "setupkey", "couldn't add peer"):
		return KindLinkExpired
	case code == codes.PermissionDenied || code == codes.Unauthenticated:
		if containsAny(msg, "login has expired", "peer login") {
			return KindLinkExpired
		}
		return KindUnknown
	case containsAny(msg, "network is unreachable", "no route to host", "unreachable network"):
		return KindNoInternet
	case code == codes.DeadlineExceeded || code == codes.Unavailable,
		containsAny(msg, "no such host", "connection refused", "i/o timeout", "deadline exceeded"):
		return KindServerUnreachable
	}
	return KindUnknown
}
