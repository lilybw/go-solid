package internal

import (
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/networking"
)

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

func GenerateEntry(comp Component) (string, error) {
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

// AssembleHTML builds the self-contained document. hmrScript is the injected
// hot-reload client, generated in package go_solid and passed in as a string
// ("" when HMR is inactive). It is passed in rather than generated here so that
// package internal never imports package hmr — hmr already imports internal, so
// the reverse would be an import cycle.
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

func inlineJSON(s string) string {
	return strings.ReplaceAll(s, "</", "<\\/")
}

func inlineJS(js string) string {
	return strings.ReplaceAll(js, "</script", "<\\/script")
}

func derivePropsMountIdFromCompRootID(rootID string) string {
	return "props-" + rootID
}
