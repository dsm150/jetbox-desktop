import { useEffect, useRef, useState } from "react";

// Щит главного экрана — он же кнопка. Порт JetBoxShield из JetBox iOS (вариант А «Спокойный») по его
// Android-порту 1:1: приглашающее дыхание кольца, хвост по кругу на подключении, при успехе — подскок
// щита и одна волна, затем ровное дыхание свечения. «Уменьшение движения» — только смена цвета.
// Значки — векторные аналоги SF Symbols из JetBox_Android (JetBoxIcons.kt), окно 24×24.
// «offline» — четвёртая фаза (JetBoxShield.kt OFFLINE): здесь «выход не отвечает» — цвет warn, без колец.
export type ShieldPhase = "off" | "working" | "on" | "offline";

const OUTLINE = 1.25;
const SLASH = 1.55;
const SECONDARY_ALPHA = 0.5;

/** Силуэт щита SF Symbols; inset — толщина обводки: контур сжимается, чтобы снаружи совпасть с заливкой. */
function shieldPath(inset = 0): string {
  const k = 1 - inset / 17.5;
  const x = (v: number) => +(12 + (v - 12) * k).toFixed(3);
  const y = x;
  return [
    `M${x(12.7)} ${y(1.62)}`,
    `L${x(19.6)} ${y(4.1)}`,
    `Q${x(20.77)} ${y(4.55)} ${x(20.77)} ${y(5.75)}`,
    `L${x(20.77)} ${y(12.2)}`,
    `C${x(20.77)} ${y(16.8)} ${x(16.5)} ${y(20.0)} ${x(12.9)} ${y(22.15)}`,
    `Q${x(12)} ${y(22.7)} ${x(11.1)} ${y(22.15)}`,
    `C${x(7.5)} ${y(20.0)} ${x(3.23)} ${y(16.8)} ${x(3.23)} ${y(12.2)}`,
    `L${x(3.23)} ${y(5.75)}`,
    `Q${x(3.23)} ${y(4.55)} ${x(4.4)} ${y(4.1)}`,
    `L${x(11.3)} ${y(1.62)}`,
    `Q${x(12)} ${y(1.38)} ${x(12.7)} ${y(1.62)}Z`,
  ].join("");
}

const FILL = shieldPath();
const LINE = shieldPath(OUTLINE);
const SLASH_A: [number, number] = [1.3, 1.9];
const SLASH_B: [number, number] = [21.5, 21.6];

/** «Всё, кроме полосы шириной 2×gap вдоль отрезка a→b» — разрыв контура у косой черты. */
function bandCut(a: [number, number], b: [number, number], gap: number): string {
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const len = Math.hypot(dx, dy);
  const [ux, uy] = [dx / len, dy / len];
  const [nx, ny] = [uy, -ux];
  const far = 60;
  return [1, -1]
    .map((side) => {
      const ox = a[0] + nx * gap * side;
      const oy = a[1] + ny * gap * side;
      const p = (px: number, py: number) => `${px.toFixed(3)} ${py.toFixed(3)}`;
      return `M${p(ox - ux * far, oy - uy * far)}L${p(ox + ux * far, oy + uy * far)}` +
        `L${p(ox + ux * far + nx * far * side, oy + uy * far + ny * far * side)}` +
        `L${p(ox - ux * far + nx * far * side, oy - uy * far + ny * far * side)}Z`;
    })
    .join("");
}
const CUT = bandCut(SLASH_A, SLASH_B, 1.9);

export function Shield({
  phase,
  label,
  disabled,
  onClick,
}: {
  phase: ShieldPhase;
  label: string;
  disabled?: boolean;
  onClick?: () => void;
}) {
  // Успех празднуется только на переходе в «Под защитой», не при открытии уже подключённым.
  const prev = useRef(phase);
  const [celebration, setCelebration] = useState(0);
  useEffect(() => {
    if (phase === "on" && prev.current !== "on") setCelebration((n) => n + 1);
    prev.current = phase;
  }, [phase]);
  const celebrating = phase === "on" && celebration > 0;

  const face = (
    <>
      <span className="shield-deco" aria-hidden />
      <span className="shield-disc" aria-hidden />
      <span key={`${phase}-${celebration}`} className={`shield-glyph${celebrating ? " pop" : ""}`} aria-hidden>
        <svg viewBox="0 0 24 24"><Symbol phase={phase} /></svg>
      </span>
      {celebrating && <span key={celebration} className="shield-wave" aria-hidden />}
    </>
  );
  const cls = `shield ${phase}${onClick ? " action" : ""}`;
  if (!onClick) {
    return <div className={cls} role="img" aria-label={label}>{face}</div>;
  }
  return (
    <button className={cls} disabled={disabled} onClick={onClick} aria-label={label}>
      {face}
    </button>
  );
}

function Symbol({ phase }: { phase: ShieldPhase }) {
  if (phase === "on") {
    // checkmark.shield.fill палитрой: сплошной щит цвета фазы и белая галочка.
    return (
      <>
        <path d={FILL} fill="currentColor" />
        <path d="M7.64 12.34 10.97 15.66 15.59 7.77" fill="none" stroke="#fff" strokeWidth="1.9"
          strokeLinecap="round" strokeLinejoin="round" />
      </>
    );
  }
  if (phase === "offline") {
    // exclamationmark.shield.fill: щит заливкой с непрозрачностью 0,5 и знак восклицания.
    return (
      <>
        <path d={FILL} fill="currentColor" fillOpacity={SECONDARY_ALPHA} />
        <path d="M12 6.9V13.4" stroke="currentColor" strokeWidth="2.3" strokeLinecap="round" />
        <circle cx="12" cy="17.1" r="1.35" fill="currentColor" />
      </>
    );
  }
  if (phase === "working") {
    // shield.lefthalf.filled: контур + залитая левая половина.
    return (
      <>
        <clipPath id="jb-shield-left"><path d="M0 0H12V24H0Z" /></clipPath>
        <path d={LINE} fill="none" stroke="currentColor" strokeWidth={OUTLINE} strokeLinejoin="round" />
        <path d={LINE} fill="currentColor" clipPath="url(#jb-shield-left)" />
      </>
    );
  }
  // shield.slash: контур — вторичный слой, косая черта — основной; контур разорван у черты.
  return (
    <>
      <clipPath id="jb-shield-cut"><path d={CUT} /></clipPath>
      <path d={LINE} fill="none" stroke="currentColor" strokeOpacity={SECONDARY_ALPHA} strokeWidth={OUTLINE}
        strokeLinejoin="round" clipPath="url(#jb-shield-cut)" />
      <path d={`M${SLASH_A.join(" ")}L${SLASH_B.join(" ")}`} stroke="currentColor" strokeWidth={SLASH}
        strokeLinecap="round" />
    </>
  );
}
