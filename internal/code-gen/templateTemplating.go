package code_gen

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/registry"
)

var (
	_jsTemplate   = template.Must(template.New("entry").Parse(jsTemplateText))
	_htmlTemplate = template.Must(template.New("html").Parse(htmlTemplateText))
)

const jsTemplateText = `
	import { render } from "solid-js/web";
	import {{.ImportClause}} from {{.ImportPath}};

	function readProps() {
		const el = document.getElementById({{.PropsMountId}});
		if (!el || !el.textContent) return {};
		try { return JSON.parse(el.textContent); } catch (error) {
			console.error("go_solid: Component " + {{.CompName}} + " could not read props from data island id " + {{.PropsMountId}} + ": invalid JSON: " + error);
			return {};
		}
	}

	const root = document.getElementById({{.CompMountId}});
	if (root) {
		// Server markup is a first paint, not a hydration target: render
		// appends, so anything already there has to go first.
		root.replaceChildren();
		render(() => Component(readProps()), root);
	} else {
		console.error("go_solid: Component " + {{.CompName}} + " could not mount: no element with id " + {{.CompMountId}} + " found in the HTML shell.");
	}
`

const htmlTemplateText = `
	<!doctype html>
	<html>
	<head>
		{{.Head}}
		{{- if .Styles}}
		{{.Styles}}
		{{- end}}
	</head>
	<body>
		<div id="{{.MountRootID}}">{{.SSR}}</div>
		<script id="{{.PropsMountID}}" type="application/json">{{.PropsJSON}}</script>
		<script type="module">{{.JS}}</script>
		{{- if .HMRScript}}
		{{.HMRScript}}
		{{- end}}
	</body>
	</html>
`

type entryTemplateData struct {
	// ImportClause is what stands between `import` and `from`: a default
	// import, or a named one aliased so the rest of the entry does not care
	// which of the two it got.
	ImportClause string
	ImportPath   string
	CompName     string
	PropsMountId string
	CompMountId  string
}

type htmlTemplateData struct {
	Head         string
	Styles       string // "" or a complete <style>...</style> element
	MountRootID  string
	PropsMountID string
	PropsJSON    string
	JS           string
	HMRScript    string // "" when HMR inactive; injected reload client otherwise
	SSR          string // "" when server rendering is off or the component needs the client
}

// GenerateEntry emits the module that mounts one component.
//
// comp.Export decides how the component is imported: empty takes the file's
// default export, a name takes that export and aliases it, so the mounting code
// below is the same either way.
func GenerateEntry(comp *registry.Component) (string, error) {
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.Path, comp.Ext))

	importClause := "Component"
	if comp.Export != "" {
		if !meta.ValidExportName(comp.Export) {
			return "", fmt.Errorf("go_solid: %q is not a name that can be imported (component %q)",
				comp.Export, comp.Name)
		}
		importClause = "{ " + comp.Export + " as Component }"
	}

	data := entryTemplateData{
		ImportClause: importClause,
		ImportPath:   strconv.Quote(importPath),
		CompName:     strconv.Quote(comp.Name),
		PropsMountId: strconv.Quote(derivePropsMountIdFromCompRootID(comp.MountRootID)),
		CompMountId:  strconv.Quote(comp.MountRootID),
	}

	var b strings.Builder
	if err := _jsTemplate.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// AssembleHTML builds the self-contained document. hmrScript is the injected
// hot-reload client; ssrHTML is the server-rendered first paint, empty when
// there is none.
func AssembleHTML(headSegment networking.HTMLHeadSegmentBuilder, propsJSON string, rendered *caching.Rendered, mountRootID, hmrScript, ssrHTML string) string {
	styles := ""
	if rendered.CSS != "" {
		styles = "<style>" + rendered.CSS + "</style>"
	}

	data := htmlTemplateData{
		Head:         headSegment.Build(),
		Styles:       styles,
		MountRootID:  mountRootID,
		PropsMountID: derivePropsMountIdFromCompRootID(mountRootID),
		PropsJSON:    inlineJSON(propsJSON),
		JS:           inlineJS(rendered.JS),
		HMRScript:    hmrScript,
		SSR:          ssrHTML,
	}

	var b strings.Builder
	if err := _htmlTemplate.Execute(&b, data); err != nil {
		panic("go_solid: AssembleHTML template execution failed: " + err.Error())
	}
	return b.String()
}

// This, not the marshaller's escaping, is what keeps the data island safe.
func inlineJSON(s string) string {
	return strings.ReplaceAll(s, "</", "<\\/")
}

// scriptClose matches the sequence that ends a <script> block. The HTML parser
// matches it without regard to case, so "</SCRIPT" closes the element just as
// "</script" does.
var scriptClose = regexp.MustCompile(`(?i)</script`)

// inlineJS makes a script body safe to place inside a <script> element
func inlineJS(js string) string {
	return scriptClose.ReplaceAllStringFunc(js, func(match string) string {
		return "<\\/" + match[len("</"):]
	})
}

func derivePropsMountIdFromCompRootID(rootID string) string {
	return "props-" + rootID
}
