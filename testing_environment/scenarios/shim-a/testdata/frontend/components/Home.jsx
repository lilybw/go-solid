export default function Home(props) {
  return <h1>Hello, {props.name ?? "world"}</h1>;
}
