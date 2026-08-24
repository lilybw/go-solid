export default function Dashboard(props: {
  title: string;
  unread: number;
  note?: string;
}) {
  return (
    <section>
      <h1>{props.title}</h1>
      <p>{props.unread} unread</p>
      {props.note && <small>{props.note}</small>}
    </section>
  );
}
