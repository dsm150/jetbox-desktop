import { useEffect, useRef, useState } from "react";
import { onState, send } from "./bridge";
import type { State } from "./types";
import { NoBox, Ready } from "./screens/Onboarding";
import { Progress } from "./screens/Progress";
import { Home } from "./screens/Home";
import { ChooseExit, ErrorScreen, OtherVPN, ServiceMissing, SwitchBox } from "./screens/Dialogs";
import { About } from "./screens/About";
import { Settings } from "./screens/Settings";

export default function App() {
  const [s, setS] = useState<State | null>(null);
  // Настройки и «О программе» — страницы поверх дома и «не подключён», как у мобильных.
  const [page, setPage] = useState<"settings" | "about" | null>(null);
  const lastSeq = useRef<number | null>(null);

  useEffect(() => {
    // События могут прийти не по порядку (ответ на hello и рассылка контроллера):
    // показываем только состояние новее уже показанного.
    const off = onState((next) => {
      if (lastSeq.current !== null && next.seq <= lastSeq.current) return;
      lastSeq.current = next.seq;
      setS(next);
    });
    send("hello");
    return off;
  }, []);

  useEffect(() => {
    if (s) document.documentElement.lang = s.lang;
  }, [s?.lang]);

  // Страницы живут только поверх дома и «не подключён»: ушли с них (ссылка, ошибка,
  // выбор выхода) — страница закрывается, иначе по возвращении домой окно снова откроется на ней.
  const pageAllowed = s?.screen === "home" || s?.screen === "nobox";
  useEffect(() => {
    if (!pageAllowed) setPage(null);
  }, [pageAllowed]);

  if (!s) return <div className="screen" />;
  if (page === "about" && pageAllowed) return <About s={s} onBack={() => setPage("settings")} />;
  if (page === "settings" && pageAllowed) {
    return <Settings s={s} onClose={() => setPage(null)} onAbout={() => setPage("about")} />;
  }

  switch (s.screen) {
    case "nobox":
      return <NoBox s={s} onSettings={() => setPage("settings")} />;
    case "ready":
      return <Ready s={s} />;
    case "switch":
      return <SwitchBox s={s} />;
    case "progress":
      return <Progress s={s} />;
    case "home":
      return <Home s={s} onSettings={() => setPage("settings")} />;
    case "choose-exit":
      return <ChooseExit s={s} />;
    case "other-vpn":
      return <OtherVPN s={s} />;
    case "service-missing":
      return <ServiceMissing s={s} />;
    case "error":
      return <ErrorScreen s={s} />;
  }
}
