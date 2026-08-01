package internal

import (
	"html/template"
	"path/filepath"
	"strconv"
	"strings"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/networking"
)

// Templates are parsed once at package init; a malformed template is a
// programming error, so Must-panic at startup rather than per render.
var (
	_jsTemplate   = template.Must(template.New("entry").Parse(jsTemplateText))
	_htmlTemplate = template.Must(template.New("html").Parse(htmlTemplateText))
)

const jsTemplateText = `
	import { render } from "solid-js/web";
	import Component from {{.ImportPath}};

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
	<div id="{{.MountRootID}}"></div>
	<script id="{{.PropsMountID}}" type="application/json">{{.PropsJSON}}</script>
	<script type="module">{{.JS}}</script>
	</body>
	</html>
`

// entryTemplateData holds the already-quoted values interpolated into the entry
// module. Every field is a JS string literal (produced by strconv.Quote), so the
// template inserts them into expression position without further quoting.
type entryTemplateData struct {
	ImportPath   string
	CompName     string
	PropsMountId string
	CompMountId  string
}

// htmlTemplateData holds the pieces of the final document. Head, Styles, JS and
// PropsJSON are all pre-escaped/pre-built; the template does no escaping.
type htmlTemplateData struct {
	Head         string
	Styles       string // "" or a complete <style>...</style> element
	MountRootID  string
	PropsMountID string
	PropsJSON    string
	JS           string
}

// generateEntry produces the entry .tsx that imports the component by absolute
// path and mounts it. Props flow via the data island, keeping the server as the
// source of truth for data.
func GenerateEntry(comp Component) (string, error) {
	// Absolute import path (without extension) so the generated entry resolves
	// the component no matter which temp directory esbuild reads it from. This
	// path is consumed only at bundle time by esbuild; it never reaches the browser.
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.AbsPath, comp.Ext))

	data := entryTemplateData{
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

// assembleHTML builds the self-contained index.html returned to the client. It
// inlines the bundled JS as a module script, inlines CSS as a <style> element,
// and embeds props as a JSON data island. Nothing is served externally.
func AssembleHTML(headSegment networking.HTMLHeadSegmentBuilder, propsJSON string, rendered *caching.Rendered, mountRootID string) string {
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
	}

	var b strings.Builder
	// Execute only errors on template/data-shape bugs, which Must + a fixed
	// struct rule out; a render-time failure here would be a programming error.
	if err := _htmlTemplate.Execute(&b, data); err != nil {
		panic("go_solid: assembleHTML template execution failed: " + err.Error())
	}
	return b.String()
}

func inlineJSON(s string) string {
	return strings.ReplaceAll(s, "</", "<\\/")
}

func inlineJS(js string) string {
	return strings.ReplaceAll(js, "</script", "<\\/script")
}

func derivePropsMountIdFromCompRootID(rootID string) string {
	return "props-" + rootID
}
