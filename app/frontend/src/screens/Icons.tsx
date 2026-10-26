// Значки интерфейса — векторные аналоги SF Symbols из JetBox_Android (JetBoxIcons.kt), окно 24×24.

function gearPath(): string {
  const w = 1.6;
  const tip = 10.15 - w / 2;
  const root = 7.45;
  const p = (r: number, deg: number) =>
    `${(12 + r * Math.cos((deg * Math.PI) / 180)).toFixed(3)} ${(12 + r * Math.sin((deg * Math.PI) / 180)).toFixed(3)}`;
  let d = "";
  for (let k = 0; k < 8; k++) {
    const c = -90 + 45 * k;
    d += `${k === 0 ? "M" : "L"}${p(root, c - 14.5)}L${p(tip, c - 10.5)}A${tip} ${tip} 0 0 1 ${p(tip, c + 10.5)}`;
    d += `L${p(root, c + 14.5)}A${root} ${root} 0 0 1 ${p(root, c + 30.5)}`;
  }
  return d + "Z";
}
const GEAR = gearPath();

/** gearshape — кнопка настроек в шапке. */
export function Gear() {
  return (
    <svg viewBox="0 0 24 24" className="icon" aria-hidden>
      <path d={GEAR} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <circle cx="12" cy="12" r="2.95" fill="none" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  );
}

/** house.fill — панель дома. */
export function House() {
  return (
    <svg viewBox="0 0 24 24" className="icon" aria-hidden>
      <path d="M2 11.4 12 3.2 22 11.4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"
        strokeLinejoin="round" />
      <path d="M17.3 8.4V4.6Q17.3 4 17.9 4H19.3Q19.9 4 19.9 4.6V10.5Z" fill="currentColor" />
      <path fillRule="evenodd" fill="currentColor"
        d="M12 5.5 20 12.05V20Q20 21.6 18.4 21.6H5.6Q4 21.6 4 20V12.05ZM9.5 14.1H14.5V19.5H9.5Z" />
    </svg>
  );
}

/** chevron.right — переход к подробностям. */
export function Chevron() {
  return (
    <svg viewBox="0 0 24 24" className="icon" aria-hidden>
      <path d="M9.5 6.5 15 12l-5.5 5.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"
        strokeLinejoin="round" />
    </svg>
  );
}
