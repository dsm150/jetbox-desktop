/** Время в сети часами, как на телефоне: «12:39» или «1:02:07». */
export function clock(sinceUnix: number, now = Date.now()): string {
  if (!sinceUnix) return "—";
  const sec = Math.max(0, Math.floor(now / 1000 - sinceUnix));
  const h = Math.floor(sec / 3600);
  const mm = String(Math.floor((sec % 3600) / 60));
  const ss = String(sec % 60).padStart(2, "0");
  return h ? `${h}:${mm.padStart(2, "0")}:${ss}` : `${mm}:${ss}`;
}

export function bytes(n: number, lang: string): string {
  const units = lang === "ru" ? ["Б", "КБ", "МБ", "ГБ", "ТБ"] : ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const num = i === 0 ? String(v) : v.toFixed(1);
  return `${lang === "ru" ? num.replace(".", ",") : num} ${units[i]}`;
}
