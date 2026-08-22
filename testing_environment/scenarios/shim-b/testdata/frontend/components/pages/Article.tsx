// Navigation is synthesised by go_solid into the workspace's published surface;
// the component composes it into its own props.
import type { Navigation } from "../.go_solid/types/navigation";

export default function Article(props: { slug: string } & Navigation) {
  return (
    <article>
      <a href={props.currentPath}>{props.slug}</a>
      {props.backHref && <a href={props.backHref}>back</a>}
    </article>
  );
}
