import { createSignal, onMount } from "solid-js";
export default function Version() {
  const [v, setV] = createSignal("…");
  onMount(() => setV("1.1.0"));
  return <span class="version">{v()}</span>;
}
