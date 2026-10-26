import { t } from "../i18n";
import type { State } from "../types";
import { Gear } from "./Icons";
import { Mark } from "./Mark";

/** Шапка главного экрана — знак JetBox и кнопка настроек, как у мобильных. */
export function Header({ s, onSettings }: { s: State; onSettings: () => void }) {
  return (
    <header className="home-header">
      <Mark small />
      <span className="wordmark">JetBox</span>
      <button className="glass-icon" onClick={onSettings} aria-label={t(s.lang, "settings.title")}><Gear /></button>
    </header>
  );
}
