import { send } from "../bridge";
import { t } from "../i18n";
import type { State } from "../types";
import { Mark } from "./Mark";
import { Page } from "./Page";

// «О программе» — как JetBoxAboutScreen мобильных: карточка со знаком, описание, ссылки, мелкий шрифт.
export function About({ s, onBack }: { s: State; onBack: () => void }) {
  return (
    <Page title={t(s.lang, "about.title")} back={{ label: t(s.lang, "about.back"), onClick: onBack }}>
      <div className="about-card">
        <div className="about-brand">
          <Mark small />
          <span className="wordmark big">JetBox</span>
        </div>
        <p className="about-body">{t(s.lang, "about.body")}</p>
        <button className="link left" onClick={() => send("openSite")}>{t(s.lang, "about.site")}</button>
        <button className="link left" onClick={() => send("openPrivacy")}>{t(s.lang, "about.privacy")}</button>
        <p className="fine">{t(s.lang, "about.legal")}</p>
      </div>
    </Page>
  );
}
