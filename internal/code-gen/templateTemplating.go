package code_gen

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/sources"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/registry"
)

var (
	_jsTemplate             = template.Must(template.New("entry").Parse(jsTemplateText))
	_htmlTemplate           = template.Must(template.New("html").Parse(htmlTemplateText))
	_htmlDataIslandTemplate = template.Must(template.New("dataIsland").Parse(htmlDataIslandTemplateText))
)

const jsTemplateText = `
	import { render as __gs_render } from "solid-js/web";
{{- if .Module}}

{{.Module}}
	const __gs_component = {{.Binding}};
{{- else}}
	import {{.ImportClause}} from {{.ImportPath}};
{{- end}}

	function __gs_readProps() {
		const el = document.getElementById({{.PropsMountId}});
		if (!el || !el.textContent) return {};
		try { return JSON.parse(el.textContent); } catch (error) {
			console.error("go_solid: Component " + {{.CompName}} + " could not read props from data island id " + {{.PropsMountId}} + ": invalid JSON: " + error);
			return {};
		}
	}

	const __gs_root = document.getElementById({{.CompMountId}});
	if (__gs_root) {
		// Server markup is a first paint, not a hydration target: render
		// appends, so anything already there has to go first.
		__gs_root.replaceChildren();
		__gs_render(() => __gs_component(__gs_readProps()), __gs_root);
	} else {
		console.error("go_solid: Component " + {{.CompName}} + " could not mount: no element with id " + {{.CompMountId}} + " found in the HTML shell.");
	}
`

const htmlDataIslandTemplateText = `
	<script id="{{.MountID}}" type="application/json">{{.JSON}}</script>
`

type htmlDataIslandTemplateData struct {
	MountID meta.HTMLElementID
	JSON    meta.JSONString
}

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
		<script type="module">{{.JS}}</script>
		{{- if .HMRScript}}
		{{.HMRScript}}
		{{- end}}
		{{- if .DataIslands}}
		{{.DataIslands}}
		{{- end}}
	</body>
	</html>
`

type entryTemplateData struct {
	// ImportClause is what stands between `import` and `from`: a default
	// import, or a named one aliased so the rest of the entry does not care
	// which of the two it got. Every name the entry introduces is prefixed,
	// since an inlined component shares this scope.
	ImportClause string
	ImportPath   string
	// Module is the whole of a component that has no file behind it, and
	// Binding is the export within it to mount. Set instead of the import pair.
	Module       string
	Binding      string
	CompName     string
	PropsMountId string
	CompMountId  string
}

type htmlTemplateData struct {
	Head        string
	Styles      string // "" or a complete <style>...</style> element
	MountRootID string
	// ENSURE THEY ARE ALL PROPERLY ESCAPED AND MADE USING htmlDataIslandTemplate
	DataIslands string
	JS          string
	HMRScript   string // "" when HMR inactive; injected reload client otherwise
	SSR         string // "" when server rendering is off or the component needs the client
}

// GenerateEntry emits the module that mounts one component.
//
// comp.Export decides how the component is reached: empty takes the file's
// default export, a name takes that export and aliases it, so the mounting code
// below is the same either way. A source with nothing behind it to import is
// written into the entry instead.
func GenerateEntry(comp *registry.Component, source sources.Source) (string, error) {
	if comp.Export != "" && !meta.ValidExportName(comp.Export) {
		return "", fmt.Errorf("go_solid: %q is not a name that can be imported (component %q)",
			comp.Export, comp.Name)
	}

	data := entryTemplateData{
		CompName:     strconv.Quote(comp.Name),
		PropsMountId: strconv.Quote(DerivePropsMountIdFromCompRootID(comp.MountRootID)),
		CompMountId:  strconv.Quote(comp.MountRootID),
	}

	switch {
	case !source.Inline:
		data.ImportClause = "__gs_component"
		if comp.Export != "" {
			data.ImportClause = "{ " + comp.Export + " as __gs_component }"
		}
		data.ImportPath = strconv.Quote(filepath.ToSlash(strings.TrimSuffix(comp.Path, comp.Ext)))

	case comp.Export == "":
		return "", fmt.Errorf(
			"go_solid: component %q has no file to import and names no export to mount", comp.Name)

	default:
		data.Module, data.Binding = source.Text, comp.Export
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
func AssembleHTML(headSegment networking.HTMLHeadSegmentBuilder, rendered *caching.Rendered, mountRootID, hmrScript, ssrHTML string) (string, error) {
	styles := ""
	if rendered.CSS != "" {
		styles = "<style>" + rendered.CSS + "</style>"
	}

	islands, err := dataIslands(rendered.DataIslands, mountRootID)
	if err != nil {
		return "", err
	}

	data := htmlTemplateData{
		Head:        headSegment.Build(),
		Styles:      styles,
		MountRootID: mountRootID,
		DataIslands: islands,
		JS:          inlineJS(rendered.JS),
		HMRScript:   hmrScript,
		SSR:         ssrHTML,
	}

	var b strings.Builder
	if err := _htmlTemplate.Execute(&b, data); err != nil {
		return "", fmt.Errorf("go_solid: AssembleHTML template execution failed: %s", err.Error())
	}
	return b.String(), nil
}

// EMPTY_ISLAND is what an island holds when nothing filled it. The entry parses
// whatever it finds, so an island is never left without a body.
const EMPTY_ISLAND meta.JSONString = "{}"

// dataIslands renders the islands, leading with the props island the entry
// reads. That one is written whether or not anything filled it: the shell always
// offers the mount its props, and the rest follow in a fixed order so the same
// render is always the same document.
func dataIslands(held map[meta.HTMLElementID]meta.JSONString, mountRootID string) (string, error) {
	props := DerivePropsMountIdFromCompRootID(mountRootID)
	mounts := append([]meta.HTMLElementID{props},
		slices.DeleteFunc(slices.Sorted(maps.Keys(held)),
			func(mount meta.HTMLElementID) bool { return mount == props })...)

	var out strings.Builder
	for _, mount := range mounts {
		json := meta.Or(held[mount], EMPTY_ISLAND)
		data := htmlDataIslandTemplateData{MountID: mount, JSON: inlineJSON(json)}
		if err := _htmlDataIslandTemplate.Execute(&out, data); err != nil {
			return "", fmt.Errorf("[go-solid]: Error preparing data island template for island %s:%s", mount, err.Error())
		}
		out.WriteString("\n")
	}
	return out.String(), nil
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

func DerivePropsMountIdFromCompRootID(rootID string) string {
	return "props-" + rootID
}
