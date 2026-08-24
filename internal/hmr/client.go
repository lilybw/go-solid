package hmr

import (
	"strconv"
	"strings"
	"text/template"

	"github.com/lilybw/go-solid/internal/meta"
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

type UrlPrefix = string

// ClientScript renders the injected hot-reload snippet for one component.
func ClientScript(path UrlPrefix, component meta.QualifiedName) string {
	var b strings.Builder
	_ = hmrClientTemplate.Execute(&b, struct {
		Path      UrlPrefix
		Component meta.QualifiedName
	}{
		Path:      strconv.Quote(path),
		Component: strconv.Quote(component),
	})
	return b.String()
}
