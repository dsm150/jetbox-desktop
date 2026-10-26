import { t } from "../i18n";
import type { State } from "../types";

/** Выход в интернет словами: что включено в службе; при выключенной защите — запомненное в коробке. */
export function exitText(s: State): string {
  const p = s.panel;
  if (p.exit) return p.exit;
  if (p.noExit || s.noExit) return t(s.lang, "exit.none");
  return s.exitName || t(s.lang, "home.exitNone");
}
