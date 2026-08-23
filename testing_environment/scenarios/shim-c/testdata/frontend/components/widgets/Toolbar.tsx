// Named exports only: no default, so the bare name resolves to nothing and the
// components here are reachable only by selector.
export function Primary(props: { action: string }) {
  return <button>{props.action}</button>;
}

export function Secondary(props: { action: string }) {
  return <button class="secondary">{props.action}</button>;
}
