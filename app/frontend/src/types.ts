export type Screen =
  | "nobox" | "ready" | "switch" | "progress" | "home"
  | "choose-exit" | "other-vpn" | "service-missing" | "error";

export interface Panel {
  serviceUp: boolean;
  /** Состояние службы как есть (Idle, Connecting, Connected, NeedsLogin…); "" — служба не отвечает. */
  serviceState: string;
  connected: boolean;
  relayed: boolean;
  sinceUnix: number;
  rxBytes: number;
  txBytes: number;
  /** «До дома», мс; -1 — не мерили или выхода нет: плитка скрыта. */
  homeMs: number;
  needsLink: boolean;
  /** Выход, включённый в службе сейчас; "" — нет или неизвестно. */
  exit: string;
  /** Служба подключена без выхода — «только сети». */
  noExit: boolean;
  /** Выход выбран, а его пир не поднялся за 15 с — «Выход не отвечает». */
  exitDown: boolean;
  /** Выходов больше одного — ссылка «Сменить». */
  canChangeExit: boolean;
}

export interface ExitCandidate {
  id: string;
  /** Ядро включило его сейчас. */
  active: boolean;
}

export interface State {
  /** Растёт с каждым изменением; окно не показывает состояние старше уже показанного. */
  seq: number;
  lang: "ru" | "en";
  version: string;
  screen: Screen;
  stage: string;
  boxName: string;
  linkBoxName: string;
  /** Экран замены: ссылка той же коробки с новым ключом («обновим подключение»). */
  host: string;
  keyMask: string;
  exitName: string;
  /** В коробке запомнено «без выхода — только сети». */
  noExit: boolean;
  /** «Подключаться автоматически» — из коробки; без коробки false (строка скрыта). */
  autoConnect: boolean;
  /** «Запускать JetBox при входе»: supported=false — ОС не наша забота (macOS), строка скрыта. */
  autostart: { supported: boolean; enabled: boolean };
  candidates: ExitCandidate[] | null;
  errorKind: string;
  /** На каком действии отказ: connect | reconnect | disconnect | choose-exit | forget. */
  errorOp: string;
  errorDetail: string;
  canRetry: boolean;
  otherVpn: string;
  busy: boolean;
  panel: Panel;
}
