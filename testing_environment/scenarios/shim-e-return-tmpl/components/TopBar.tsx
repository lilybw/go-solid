import { JSX } from "solid-js/jsx-runtime";
import "./TopBar.css"

interface TopBarProps {

}

// Include this to include the top bar with HOTS logo, balance and user indication
export default function TopBar(props: TopBarProps): JSX.Element {
    return (
        <div class="TopBar" data-testid="top-bar-container">
            <div class="horizontal-container">
                <div>LOGO</div>
                <div>Balance</div>
                <div>User</div>
            </div>
        </div>
    )
}