import type { ShieldPhase } from "./Shield";

// Живой фон под «стеклом» — порт JetBoxBackdrop (iOS/Android): три мягких пятна фирменного цвета
// медленно дрейфуют; под защитой пятна насыщаются фирменным синим. «Уменьшение движения» — статично.
export function Backdrop({ phase }: { phase: ShieldPhase }) {
  return (
    <div className={`backdrop ${phase}`} aria-hidden>
      <span className="blob big" />
      <span className="blob side" />
      <span className="blob low" />
    </div>
  );
}
