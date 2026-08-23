// Panel is the shape a selector is for: one file, several components, plus the
// odds and ends a real module accumulates around them.
import type { PanelProps, SectionProps } from "./types.js";

export const TITLE = "Panel";

export interface Decoration {
  tone: string;
}

export function Header(props: SectionProps) {
  return <header>{props.label}</header>;
}

export const Footer = (props: SectionProps) => <footer>{props.label}</footer>;

// Declared, exported under another name. The selector uses the outward name.
function Sidebar(props: SectionProps) {
  return <aside>{props.label}</aside>;
}
export { Sidebar as Aside };

// Declared and deliberately not exported: reachable from inside the file only.
function Scratch(props: SectionProps) {
  return <div>{props.label}</div>;
}

export default function Panel(props: PanelProps) {
  return (
    <section>
      <Header label={props.title} />
      <Scratch label={props.title} />
      <Footer label={props.title} />
    </section>
  );
}
