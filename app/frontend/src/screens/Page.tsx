import { useEffect, useState, type ReactNode } from "react";
import { Chevron } from "./Icons";

// Страница настроек — как JBPage/JBSection/JBListRow мобильных: шапка с заголовком по центру
// (слева «назад» или справа капсула «Закрыть»), ниже секции-карточки со строками.
export function Page({ title, back, close, children }: {
  title: string;
  back?: { label: string; onClick: () => void };
  close?: { label: string; onClick: () => void };
  children: ReactNode;
}) {
  return (
    <div className="page">
      <header className="page-bar">
        <span className="page-slot">
          {back && (
            <button className="glass-icon back" onClick={back.onClick} aria-label={back.label}><Chevron /></button>
          )}
        </span>
        <h1 className="page-title">{title}</h1>
        <span className="page-slot end">
          {close && <button className="capsule" onClick={close.onClick}>{close.label}</button>}
        </span>
      </header>
      <div className="page-body">{children}</div>
    </div>
  );
}

export function Section({ header, footer, children }: { header?: string; footer?: string; children: ReactNode }) {
  return (
    <section className={`section${header ? "" : " bare"}`}>
      {header && <h2 className="section-header">{header}</h2>}
      <div className="section-card">{children}</div>
      {footer && <p className="section-footer">{footer}</p>}
    </section>
  );
}

/** Строка «подпись — значение»; с onClick нажимается вся строка, справа шеврон. */
export function Row({ label, value, tone, onClick, disabled }: {
  label: string;
  value?: string;
  tone?: "accent" | "danger";
  onClick?: () => void;
  disabled?: boolean;
}) {
  const body = (
    <>
      <span className={`row-label${tone ? ` tone-${tone}` : value !== undefined ? " tone-muted" : ""}`}>{label}</span>
      {value !== undefined && <span className="row-value">{value}</span>}
      {onClick && !tone && <span className="row-chevron"><Chevron /></span>}
    </>
  );
  return onClick
    ? <button className="row" onClick={onClick} disabled={disabled}>{body}</button>
    : <div className="row">{body}</div>;
}

/**
 * Строка-переключатель — как Toggle/Switch мобильных: подпись слева, тумблер справа, нажимается
 * вся строка. Тумблер сразу встаёт в новое положение и до ответа приложения не нажимается (двойной
 * клик иначе выполнился бы в обратном порядке; без приглушения — ответ обычно мгновенный); ответа нет за 3 с (отказ) — возвращается к тому,
 * что прислало приложение.
 */
export function SwitchRow({ label, on, onChange, disabled }: {
  label: string;
  on: boolean;
  onChange: (on: boolean) => void;
  disabled?: boolean;
}) {
  const [pending, setPending] = useState<boolean | null>(null);
  useEffect(() => setPending(null), [on]);
  useEffect(() => {
    if (pending === null) return;
    const timer = setTimeout(() => setPending(null), 3000);
    return () => clearTimeout(timer);
  }, [pending]);
  const shown = pending ?? on;
  return (
    <button
      className="row"
      role="switch"
      aria-checked={shown}
      disabled={disabled}
      aria-disabled={pending !== null || undefined}
      onClick={() => {
        if (pending !== null) return;
        setPending(!shown);
        onChange(!shown);
      }}
    >
      <span className="row-label">{label}</span>
      <span className={`switch${shown ? " on" : ""}`} aria-hidden="true" />
    </button>
  );
}
