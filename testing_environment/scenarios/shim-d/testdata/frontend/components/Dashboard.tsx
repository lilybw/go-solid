import S from "go-solid/static";
import type { DashboardProps } from "./types.js";

export default function Dashboard(props: DashboardProps) {
  return (
    <main>
      <img src={S.images.logo} alt="" />
      <h1>{props.title}</h1>
    </main>
  );
}
