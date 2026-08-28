import S from "@go_solid/static"


export function HOTSLogo() {
    return (
        <div class="HOTSLogo" data-testid="hots-logo">
            <img class="hots-icon" src={S.img.hots_logo_p.svg}></img>
            <h1>HOTS</h1>
        </div>
    )
}
