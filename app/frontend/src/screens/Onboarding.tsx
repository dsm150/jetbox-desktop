import { send } from "../bridge";
import { t } from "../i18n";
import type { State } from "../types";
import { Backdrop } from "./Backdrop";
import { Header } from "./Header";
import { Mark } from "./Mark";
import { Shield } from "./Shield";

// «JetBox не подключён» — главный экран мобильных без коробки: выключенный щит (не кнопка),
// внизу карточка с подсказкой открыть ссылку.
export function NoBox({ s, onSettings }: { s: State; onSettings: () => void }) {
  return (
    <div className="screen home">
      <Backdrop phase="off" />
      <Header s={s} onSettings={onSettings} />
      <div className="spacer" />
      <Shield phase="off" label={t(s.lang, "home.disconnected")} />
      <h1 className="status">{t(s.lang, "home.disconnected")}</h1>
      <div className="spacer" />
      <section className="dock nobox">
        <h2>{t(s.lang, "nobox.title")}</h2>
        <p>{t(s.lang, "nobox.body")}</p>
      </section>
    </div>
  );
}

export function Ready({ s }: { s: State }) {
  return (
    <div className="screen">
      <Mark small />
      <p className="caption">{t(s.lang, "ready.caption")}</p>
      <h1>{s.linkBoxName}</h1>
      <dl className="facts">
        <dt>{t(s.lang, "ready.server")}</dt>
        <dd>{s.host}</dd>
        <dt>{t(s.lang, "ready.key")}</dt>
        <dd className="mono">{s.keyMask}</dd>
      </dl>
      <p className="muted">{t(s.lang, "ready.hint")}</p>
      <div className="spacer" />
      <p className="fine">
        {t(s.lang, "privacy.notice")}{" "}
        <button className="link inline" onClick={() => send("openPrivacy")}>{t(s.lang, "privacy.more")}</button>
      </p>
      <button className="primary" disabled={s.busy} onClick={() => send("connect")}>{t(s.lang, "ready.button")}</button>
    </div>
  );
}
