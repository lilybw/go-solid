import { createSignal } from "solid-js";
import "./login.css";

export default function LoginForm(props: { title?: string }) {
  const [user, setUser] = createSignal("");
  return (
    <div class="login">
      <h1>{props.title ?? "Sign in"}</h1>
      <input value={user()} onInput={(e) => setUser(e.currentTarget.value)} placeholder="username" />
      <button disabled={user().length === 0}>Log in as {user()}</button>
    </div>
  );
}
