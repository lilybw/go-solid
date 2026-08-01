package hmr

import (
	"strconv"
	"strings"
	"text/template"
)

var hmrClientTemplate = template.Must(template.New("hmr").Parse(
	`<script>
(function () {
	var path = {{.Path}};
	var comp = {{.Component}};
	function connect() {
		var proto = location.protocol === "https:" ? "wss:" : "ws:";
		var url = proto + "//" + location.host + path + "?c=" + encodeURIComponent(comp);
		var ws = new WebSocket(url);
		ws.onmessage = function () { location.reload(); };
		ws.onclose = function () {
			setTimeout(connect, 1000);
		};
	}
	connect();
})();
</script>`))

// ClientScript renders the injected hot-reload snippet for one component.
// Exported because it is called from package go_solid (which owns render0);
// package hmr cannot import go_solid, but go_solid imports hmr, so generation
// lives here and the result is passed into AssembleHTML as a string.
func ClientScript(path, component string) string {
	var b strings.Builder
	// Execute cannot fail: fixed template, fixed data shape. A failure would be
	// a programming error, not a runtime condition.
	_ = hmrClientTemplate.Execute(&b, struct {
		Path      string
		Component string
	}{
		Path:      strconv.Quote(path),
		Component: strconv.Quote(component),
	})
	return b.String()
}
