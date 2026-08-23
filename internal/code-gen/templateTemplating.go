package code_gen

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	caching "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/shared/networking"
	"github.com/lilybw/go-solid/shared/registry"
)

var (
	_jsTemplate   = template.Must(template.New("entry").Parse(jsTemplateText))
	_htmlTemplate = template.Must(template.New("html").Parse(htmlTemplateText))
)

// TODO: The component itself knows nothing of the root HTML element id, nor cares for it. Thus it should not be part of the cache key.
// as the same component can easily be mounted on multiple ids
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
		{{- if .HMRScript}}
		{{.HMRScript}}
		{{- end}}
	</body>
	</html>
`

type entryTemplateData struct {
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
}

func GenerateEntry(comp *registry.Component) (string, error) {
	importPath := filepath.ToSlash(strings.TrimSuffix(comp.Path, comp.Ext))

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

// AssembleHTML builds the self-contained document. hmrScript is the injected
// hot-reload client
func AssembleHTML(headSegment networking.HTMLHeadSegmentBuilder, propsJSON string, rendered *caching.Rendered, mountRootID, hmrScript string) string {
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
	}

	var b strings.Builder
	if err := _htmlTemplate.Execute(&b, data); err != nil {
		panic("go_solid: AssembleHTML template execution failed: " + err.Error())
	}
	return b.String()
}

// inlineJSON makes a JSON document safe to place inside a
// <script type="application/json"> element, whose content the HTML parser ends
// at the first "</script". Escaping "</" as "<\/" — a legal JSON string escape
// — removes the only way out of the block.
//
// This, not the marshaller's escaping, is what keeps the data island safe.
func inlineJSON(s string) string {
	return strings.ReplaceAll(s, "</", "<\\/")
}

// scriptClose matches the sequence that ends a <script> block. The HTML parser
// matches it without regard to case, so "</SCRIPT" closes the element just as
// "</script" does.
var scriptClose = regexp.MustCompile(`(?i)</script`)

// inlineJS makes a script body safe to place inside a <script> element, whose
// content the HTML parser ends at the first "</script" in any case. The
// original casing is kept, so a string literal that held "</SCRIPT>" still
// evaluates to it at runtime.
func inlineJS(js string) string {
	return scriptClose.ReplaceAllStringFunc(js, func(match string) string {
		return "<\\/" + match[len("</"):]
	})
}

func derivePropsMountIdFromCompRootID(rootID string) string {
	return "props-" + rootID
}
