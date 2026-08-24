import S from "go-solid/static";
import type { PanelProps } from "../types.js";

// Several components in one file, reachable by selector.
export function Header(props: PanelProps) {
  return <header>{props.label}</header>;
}

export function Footer(props: PanelProps) {
  return (
    <footer>
      <img src={S.images.icons.tick} alt="" />
      {props.label}
    </footer>
  );
}

export const TITLE = "Sidebar";
