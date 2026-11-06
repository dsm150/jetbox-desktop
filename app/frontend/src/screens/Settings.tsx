import { useEffect, useState } from "react";
import { send } from "../bridge";
import { t } from "../i18n";
import type { State } from "../types";
import { exitText } from "./exitText";
import { Page, Row, Section, SwitchRow } from "./Page";

// «Настройки» — как JetBoxSettingsScreen мобильных: JetBox (имя, выход), Приложение (автозапуск,
// автоподключение, версия, «О программе»). Сверх мобильных — «Отвязать компьютер» и «Удалить JetBox…» (штатное удаление ОС):
// на телефоне это удаление приложения.
export function Settings({ s, onClose, onAbout }: { s: State; onClose: () => void; onAbout: () => void }) {
  // Что подтверждаем: отвязку компьютера или удаление приложения.
  const [confirm, setConfirm] = useState<"forget" | "uninstall" | null>(null);
  // Удаление идёт (на macOS до ~20 с: пароль, снятие с учёта) — строка «Удаляем…»;
  // отказ в окне пароля/UAC снимает Busy, и строка возвращается.
  const [uninstalling, setUninstalling] = useState(false);
  useEffect(() => {
    if (!s.busy) setUninstalling(false);
  }, [s.busy]);
  const p = s.panel;
  // Автозапуск выключен, автоподключение включено — служба поднимет защиту при входе, а значка
  // JetBox не будет: предупреждаем. На macOS автозапуск ведёт .pkg — там не о чем.
  const hidden = s.autostart.supported && !s.autostart.enabled && !!s.boxName && s.autoConnect;
  return (
    <Page title={t(s.lang, "settings.title")} close={{ label: t(s.lang, "common.close"), onClick: onClose }}>
      <Section header={t(s.lang, "settings.sectionBox")}>
        {s.boxName ? (
          <>
            <Row label={t(s.lang, "settings.name")} value={s.boxName} />
            <hr />
            <Row
              label={t(s.lang, "settings.exit")}
              value={exitText(s)}
              onClick={p.canChangeExit ? () => send("changeExit") : undefined}
              disabled={s.busy}
            />
          </>
        ) : (
          <p className="row-note">{t(s.lang, "settings.noBox")}</p>
        )}
      </Section>
      <Section footer={confirm ? t(s.lang, confirm === "forget" ? "about.forgetConfirm" : "uninstall.confirm") : undefined}>
        {confirm ? (
          <>
            <Row
              label={t(s.lang, confirm === "forget" ? "about.forgetYes" : "uninstall.yes")}
              tone="danger"
              disabled={s.busy}
              onClick={() => {
                send(confirm);
                setConfirm(null);
                if (confirm === "forget") onClose();
                else setUninstalling(true);
              }}
            />
            <hr />
            <Row label={t(s.lang, "common.cancel")} tone="accent" onClick={() => setConfirm(null)} />
          </>
        ) : (
          <>
            {s.boxName && (
              <>
                <Row label={t(s.lang, "about.forget")} tone="danger" disabled={s.busy} onClick={() => setConfirm("forget")} />
                <hr />
              </>
            )}
            <Row
              label={t(s.lang, uninstalling ? "uninstall.progress" : "uninstall.row")}
              tone="danger"
              disabled={s.busy}
              onClick={() => setConfirm("uninstall")}
            />
          </>
        )}
      </Section>
      <Section header={t(s.lang, "settings.sectionApp")} footer={hidden ? t(s.lang, "settings.autoConnectHidden") : undefined}>
        {s.autostart.supported && (
          <>
            <SwitchRow
              label={t(s.lang, "settings.autostart")}
              on={s.autostart.enabled}
              onChange={(on) => send("setAutostart", on ? "on" : "off")}
            />
            <hr />
          </>
        )}
        {s.boxName && (
          <>
            <SwitchRow
              label={t(s.lang, "settings.autoConnect")}
              on={s.autoConnect}
              disabled={s.busy}
              onChange={(on) => send("setAutoConnect", on ? "on" : "off")}
            />
            <hr />
          </>
        )}
        <Row label={t(s.lang, "settings.version")} value={s.version} />
        <hr />
        <Row label={t(s.lang, "about.title")} onClick={onAbout} />
      </Section>
    </Page>
  );
}
