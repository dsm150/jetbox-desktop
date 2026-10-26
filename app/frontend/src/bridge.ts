import { Events } from "@wailsio/runtime";
import type { State } from "./types";

export type Cmd =
  | "hello" | "connect" | "confirmSwitch" | "cancelSwitch" | "ignoreOtherVpn" | "recheck"
  | "chooseExit" | "noExit" | "changeExit" | "skipExit" | "toggle" | "forget" | "uninstall" | "dismiss" | "openPrivacy" | "openSite";

export function send(cmd: Cmd, arg = ""): void {
  void Events.Emit("jetbox:cmd", { cmd, arg });
}

export function onState(cb: (s: State) => void): () => void {
  return Events.On("jetbox:state", (ev: { data: State }) => cb(ev.data));
}
