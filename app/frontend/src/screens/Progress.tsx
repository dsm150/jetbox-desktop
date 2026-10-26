import { t } from "../i18n";
import type { State } from "../types";
import { Shield } from "./Shield";

export function Progress({ s }: { s: State }) {
  const key = s.stage ? `progress.${s.stage}` : "progress.registering";
  return (
    <div className="screen center">
      <Shield phase="working" label={t(s.lang, key)} />
      <h2 role="status" aria-live="polite">{t(s.lang, key)}</h2>
    </div>
  );
}
