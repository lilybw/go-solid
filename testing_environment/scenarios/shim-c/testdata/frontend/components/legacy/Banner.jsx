// A .jsx file, untyped, whose default export is an arrow bound to a local name.
const Banner = (props) => <div class="banner">{props.text}</div>;

export const Dismiss = (props) => <button>{props.label}</button>;

export default Banner;
