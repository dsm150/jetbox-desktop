import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { send } from "../bridge";
import { bytes, clock } from "../format";
import { t } from "../i18n";
import type { State } from "../types";
import { Backdrop } from "./Backdrop";
import { Header } from "./Header";
import { Chevron, House } from "./Icons";
import { exitText } from "./exitText";
import { Shield, type ShieldPhase } from "./Shield";

// Главный экран — как у JetBox iOS/Android: шапка (знак JetBox и настройки), щит-кнопка, состояние,
// внизу панель дома с цифрами. Подробности (выход в интернет, трафик) раскрываются по нажатию на панель.
export function Home({ s, onSettings }: { s: State; onSettings: () => void }) {
  const p = s.panel;
  const on = p.connected;
  // Выход выбран, а его пир не поднялся за 15 с — щит «offline», «Выход не отвечает».
  const exitDown = !on && p.exitDown;
  // Служба подключается или уже подключена, а пир выхода ещё нет — щит «подключаем»,
  // текст «Подключаем…». Нажатие в этих состояниях выключает защиту (controller.Toggle).
  // Щит шлёт намерение — то, что он показывал: повторный клик не включит обратно.
  const connecting = !on && !exitDown && (p.serviceState === "Connecting" || p.serviceState === "Connected");
  const svcOn = on || connecting || exitDown;
  const phase: ShieldPhase = on ? "on" : exitDown ? "offline" : connecting ? "working" : "off";
  // Имя выхода: что включено в службе, иначе запомненное в коробке.
  const exitName = p.exit || s.exitName;
  const [details, setDetails] = useState(false);
  const now = useNow(on);
  return (
    <div className="screen home">
      <Backdrop phase={phase} />
      <Header s={s} onSettings={onSettings} />
      <div className="spacer" />
      <Shield
        phase={phase}
        disabled={s.busy || p.needsLink}
        onClick={() => send("toggle", svcOn ? "off" : "on")}
        label={svcOn ? t(s.lang, "home.turnOff") : t(s.lang, "home.turnOn")}
      />
      <h1 className={`status${on ? " on" : exitDown ? " warn" : ""}`} role="status" aria-live="polite">
        {on
          ? t(s.lang, "home.connected")
          : exitDown
            ? t(s.lang, "home.exitDown")
            : connecting
              ? t(s.lang, "progress.connecting")
              : t(s.lang, "home.disconnected")}
      </h1>
      {p.needsLink && <p className="muted center-text">{t(s.lang, "home.needsLink")}</p>}
      <div className="spacer" />
      <section className="dock">
        <button className="dock-head" onClick={() => setDetails(!details)} aria-expanded={details}>
          <span className="dock-house"><House /></span>
          <span className="dock-titles">
            <span className="dock-name">{s.boxName}</span>
            <span className="dock-sub">
              {on
                ? t(s.lang, "dock.on")
                : exitDown
                  ? exitName
                    ? t(s.lang, "dock.exitDown", exitName)
                    : t(s.lang, "home.exitDown")
                  : connecting
                    ? t(s.lang, "dock.connecting")
                    : t(s.lang, "dock.off")}
            </span>
          </span>
          <span className={`dock-chevron${details ? " open" : ""}`}><Chevron /></span>
        </button>
        {on && (
          // «До дома» не измерен — плитки нет (не «—»); без выхода нет и «соединения»:
          // напрямую/ретранслятор — это про пир выхода.
          <div className="metrics">
            <Metric value={clock(p.sinceUnix, now)} caption={t(s.lang, "dock.uptime")} />
            {p.homeMs >= 0 && <Metric value={t(s.lang, "panel.ms", String(p.homeMs))} caption={t(s.lang, "dock.latency")} />}
            {!p.noExit && (
              <Metric value={p.relayed ? t(s.lang, "panel.relayed") : t(s.lang, "panel.direct")} caption={t(s.lang, "dock.route")} />
            )}
          </div>
        )}
        {details && (
          <dl className="dock-details">
            <dt>{t(s.lang, "dock.exit")}</dt>
            <dd>
              {exitText(s)}
              {p.canChangeExit && (
                <>
                  {" "}
                  <button className="link inline" disabled={s.busy} onClick={() => send("changeExit")}>
                    {t(s.lang, "home.changeExit")}
                  </button>
                </>
              )}
            </dd>
            {on && (
              <>
                <dt>{t(s.lang, "panel.traffic")}</dt>
                <dd>{bytes(p.rxBytes, s.lang)} / {bytes(p.txBytes, s.lang)}</dd>
              </>
            )}
          </dl>
        )}
      </section>
    </div>
  );
}

function Metric({ value, caption }: { value: string; caption: string }) {
  // Как на телефоне: длинное значение («Ретранслятор») уменьшается шагами 0,5 от 16 до 12.
  const ref = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    let size = 16;
    el.style.fontSize = `${size}px`;
    while (el.scrollWidth > el.clientWidth && size > 12) {
      size -= 0.5;
      el.style.fontSize = `${size}px`;
    }
  }, [value]);
  return (
    <div className="metric">
      <span ref={ref} className="metric-value">{value}</span>
      <span className="metric-caption">{caption}</span>
    </div>
  );
}

/** Текущее время, раз в секунду, пока идёт счётчик «в сети». */
function useNow(ticking: boolean): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!ticking) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [ticking]);
  return now;
}
