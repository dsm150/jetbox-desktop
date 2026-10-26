import { useState } from "react";
import { send } from "../bridge";
import { has, t } from "../i18n";
import type { State } from "../types";

export function SwitchBox({ s }: { s: State }) {
  return (
    <div className="screen">
      <h1>{t(s.lang, "switch.title")}</h1>
      <p>
        {s.boxName
          ? t(s.lang, "switch.message", s.boxName, s.linkBoxName)
          : t(s.lang, "switch.messageUnknown", s.linkBoxName)}
      </p>
      <div className="spacer" />
      <button className="primary" disabled={s.busy} onClick={() => send("confirmSwitch")}>{t(s.lang, "switch.confirm")}</button>
      <button className="secondary" onClick={() => send("cancelSwitch")}>{t(s.lang, "common.cancel")}</button>
    </div>
  );
}

export function ChooseExit({ s }: { s: State }) {
  const list = s.candidates ?? [];
  // Ни один выход не включён ядром — сейчас «без выхода».
  const noneActive = !list.some((c) => c.active);
  return (
    <div className="screen">
      <h1>{t(s.lang, "exit.title")}</h1>
      <p className="muted">{list.length ? t(s.lang, "exit.message") : t(s.lang, "exit.empty")}</p>
      <ul className="choices">
        {list.map((c) => (
          <li key={c.id}>
            <button className="choice" disabled={s.busy} onClick={() => send("chooseExit", c.id)}>
              <span className="choice-name">{c.id}</span>
              {c.active && <span className="choice-mark">{t(s.lang, "exit.active")}</span>}
            </button>
          </li>
        ))}
        {list.length > 0 && (
          <li>
            <button className="choice" disabled={s.busy} onClick={() => send("noExit")}>
              <span className="choice-name">{t(s.lang, "exit.none")}</span>
              {noneActive && <span className="choice-mark">{t(s.lang, "exit.noneActive")}</span>}
              <span className="choice-hint">{t(s.lang, "exit.noneHint")}</span>
            </button>
          </li>
        )}
      </ul>
      <div className="spacer" />
      <button className="secondary" disabled={s.busy} onClick={() => send("skipExit")}>{t(s.lang, "exit.skip")}</button>
    </div>
  );
}

export function OtherVPN({ s }: { s: State }) {
  return (
    <div className="screen">
      <h1>{t(s.lang, "othervpn.title")}</h1>
      <p>{t(s.lang, "othervpn.body", s.otherVpn)}</p>
      <div className="spacer" />
      <button className="primary" disabled={s.busy} onClick={() => send("recheck")}>{t(s.lang, "common.recheck")}</button>
      <button className="secondary" disabled={s.busy} onClick={() => send("ignoreOtherVpn")}>{t(s.lang, "othervpn.ignore")}</button>
    </div>
  );
}

export function ServiceMissing({ s }: { s: State }) {
  return (
    <div className="screen">
      <h1>{t(s.lang, "service.title")}</h1>
      <p>{t(s.lang, "service.body")}</p>
      <div className="spacer" />
      <button className="primary" onClick={() => send("recheck")}>{t(s.lang, "common.recheck")}</button>
    </div>
  );
}

export function ErrorScreen({ s }: { s: State }) {
  const [open, setOpen] = useState(false);
  // Вид отказа без своего текста (новый Kind в core/flow) — общий «Что-то пошло не так».
  const kind = s.errorKind && has(`error.${s.errorKind}.title`) ? s.errorKind : "Unknown";
  // Подключение по ссылке — тексты по виду отказа (они про ссылку). Щит, выбор выхода,
  // отвязка — заголовок по действию, текст по виду (или свой у действия).
  const op = s.errorOp && s.errorOp !== "connect" && has(`error.op.${s.errorOp}.title`) ? s.errorOp : "";
  const title = op ? `error.op.${op}.title` : `error.${kind}.title`;
  const body = op && has(`error.op.${op}.body`) ? `error.op.${op}.body` : `error.${kind}.body`;
  return (
    <div className="screen">
      <h1>{t(s.lang, title)}</h1>
      <p>{t(s.lang, body)}</p>
      {s.errorDetail && (
        <>
          <button className="link" aria-expanded={open} onClick={() => setOpen(!open)}>{t(s.lang, "error.details")}</button>
          {open && <pre className="details">{s.errorDetail}</pre>}
        </>
      )}
      <div className="spacer" />
      {s.canRetry && <button className="primary" onClick={() => send("connect")}>{t(s.lang, "common.retry")}</button>}
      <button className="secondary" onClick={() => send("dismiss")}>{t(s.lang, "common.close")}</button>
    </div>
  );
}
