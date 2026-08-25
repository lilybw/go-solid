import { JSX } from "solid-js/jsx-runtime";

interface ReturnsProps {

}

// Every position the return-statement splice has to get right, in one
// component: a parenthesized return, a bare return, and a return inside a
// callback nested in JSX. TopBar covers only the first.
export default function Returns(props: ReturnsProps): JSX.Element {
    const rows = ["a", "b"];
    return (
        <div class="Returns" data-testid="returns-container">
            {bare()}
            <ul>
                {rows.map((r) => {
                    return <li>{r}</li>;
                })}
            </ul>
        </div>
    )
}

function bare(): JSX.Element {
    return <span>bare</span>;
}
