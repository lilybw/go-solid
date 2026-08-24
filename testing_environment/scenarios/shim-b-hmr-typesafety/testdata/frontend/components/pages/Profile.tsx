interface ProfileProps {
  userId: string;
  displayName: string;
}

export default function Profile(props: ProfileProps) {
  return (
    <header>
      <h1>{props.displayName}</h1>
      <span data-user={props.userId} />
    </header>
  );
}
