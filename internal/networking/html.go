package networking

import (
	"maps"
	"slices"
	"sort"

	"github.com/lilybw/go-solid/internal/meta"
)

type HTMLHeadSegmentBuilder interface {
	AddUnique(key, value string) HTMLHeadSegmentBuilder
	Add(tag HTMLTag) HTMLHeadSegmentBuilder

	AddLink(rel, href string) HTMLHeadSegmentBuilder
	// SetTitle sets the <title> tag in the HTML head. If called multiple times, the last call wins.
	SetTitle(title string) HTMLHeadSegmentBuilder
	// DeterministicOutput sets the builder to produce deterministic output for testing purposes. The order of tags in the output will be consistent across runs, which is useful for unit tests that compare generated HTML.
	DeterministicOutput() HTMLHeadSegmentBuilder
	// Formats the accumulated HTML head segment and returns it as a string. The caller is responsible for inserting it into the <head>...</head> section of the HTML document.
	Build() string
}

type HTMLTagName = string

type HTMLTag struct {
	Name              HTMLTagName
	HTMLTagAttributes map[string]string
	InnerHTML         string
}

type htmlHeadSegmentBuilder struct {
	//This map is read naively: i.e. kv pair [k;v] becomes <k>v</k> in the HTML head. For more complex needs, use a custom HTML head template.
	unique        map[HTMLTagName]string
	rest          []HTMLTag
	deterministic bool
}

func generateEmptyHeadSegmentBuilderTemplate() *htmlHeadSegmentBuilder {
	instance := &htmlHeadSegmentBuilder{
		unique:        make(map[HTMLTagName]string),
		rest:          make([]HTMLTag, 0),
		deterministic: false,
	}
	instance.SetTitle("go-solid")
	return instance
}

var _htmlHeadBuilderTemplate = generateEmptyHeadSegmentBuilderTemplate()

func SetHTMLHeadSegmentTemplate(fn meta.Configurator[HTMLHeadSegmentBuilder]) {
	_htmlHeadBuilderTemplate = generateEmptyHeadSegmentBuilderTemplate()
	fn(_htmlHeadBuilderTemplate)
}

func NewHTMLHeadSegmentBuilder() HTMLHeadSegmentBuilder {
	instance := &htmlHeadSegmentBuilder{
		unique:        make(map[HTMLTagName]string),
		rest:          make([]HTMLTag, 0),
		deterministic: _htmlHeadBuilderTemplate.deterministic,
	}
	instance.rest = slices.Clone(_htmlHeadBuilderTemplate.rest)
	instance.unique = maps.Clone(_htmlHeadBuilderTemplate.unique)
	return instance
}

func (this *htmlHeadSegmentBuilder) AddLink(rel, href string) HTMLHeadSegmentBuilder {
	this.rest = append(this.rest, HTMLTag{
		Name: "link",
		HTMLTagAttributes: map[string]string{
			"href": href,
			"rel":  rel,
		},
	})
	return this
}

func (this *htmlHeadSegmentBuilder) SetTitle(title string) HTMLHeadSegmentBuilder {
	this.unique["title"] = title
	return this
}

func (this *htmlHeadSegmentBuilder) DeterministicOutput() HTMLHeadSegmentBuilder {
	this.deterministic = true
	return this
}

func (this *htmlHeadSegmentBuilder) AddUnique(key, value string) HTMLHeadSegmentBuilder {
	this.unique[HTMLTagName(key)] = value
	return this
}

func (this *htmlHeadSegmentBuilder) Add(tag HTMLTag) HTMLHeadSegmentBuilder {
	this.rest = append(this.rest, tag)
	return this
}

func (this *htmlHeadSegmentBuilder) Build() string {
	all := this.rest
	for k, v := range this.unique {
		all = append(all, HTMLTag{
			Name:      k,
			InnerHTML: v,
		})
	}
	if this.deterministic {
		sort.Slice(all, func(i, j int) bool {
			return all[i].Name < all[j].Name
		})
	}
	html := ""
	for _, tag := range all {
		html += "<" + tag.Name
		for attr, val := range tag.HTMLTagAttributes {
			html += " " + attr + "=\"" + val + "\""
		}
		html += ">"
		if tag.InnerHTML != "" {
			html += tag.InnerHTML
		}
		html += "</" + tag.Name + ">\n"
	}
	return html
}
