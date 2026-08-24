export default function LoginForm() {
  return (
    <form>
      <input name="username" placeholder="username" />
      <input name="password" type="password" placeholder="password" />
      <button type="submit">Log in</button>
    </form>
  );
}
