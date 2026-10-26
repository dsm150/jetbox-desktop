import light from "../assets/jb-light.png";
import dark from "../assets/jb-dark.png";

export function Mark({ small = false }: { small?: boolean }) {
  return (
    <picture className={small ? "mark small" : "mark"}>
      <source srcSet={dark} media="(prefers-color-scheme: dark)" />
      <img src={light} alt="JetBox" />
    </picture>
  );
}
