package networking

import (
	"maps"
	"slices"
	"sort"
	"strings"

	. "github.com/lilybw/go-solid/shared/networking"
)

type htmlHeadSegmentBuilder struct {
	//This map is read naively: i.e. kv pair [k;v] becomes <k>v</k> in the HTML head. For more complex needs, use a custom HTML head template.
	unique        map[HTMLTagName]string
	rest          []HTMLTag
	deterministic bool
}

// newHeadSegmentBuilder is the library default: a title and nothing else.
func newHeadSegmentBuilder() *htmlHeadSegmentBuilder {
	instance := &htmlHeadSegmentBuilder{
		unique:        make(map[HTMLTagName]string),
		rest:          make([]HTMLTag, 0),
		deterministic: false,
	}
	instance.SetTitle("go-solid")
	return instance
}

func (this *htmlHeadSegmentBuilder) clone() *htmlHeadSegmentBuilder {
	return &htmlHeadSegmentBuilder{
		unique:        maps.Clone(this.unique),
		rest:          slices.Clone(this.rest),
		deterministic: this.deterministic,
	}
}

// NewHTMLHeadSegmentBuilder returns a builder carrying only the library
// defaults. For one seeded from a Bundler's configured head template, use
// Defaults.NewHTMLHeadSegmentBuilder
func NewHTMLHeadSegmentBuilder() HTMLHeadSegmentBuilder {
	return newHeadSegmentBuilder()
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

func (this *htmlHeadSegmentBuilder) AddUnique(key HTMLTagName, value string) HTMLHeadSegmentBuilder {
	this.unique[key] = value
	return this
}

func (this *htmlHeadSegmentBuilder) Add(tag HTMLTag) HTMLHeadSegmentBuilder {
	this.rest = append(this.rest, tag)
	return this
}

func (this *htmlHeadSegmentBuilder) Build() string {
	// Build reads; it must not write.
	all := make([]HTMLTag, 0, len(this.rest)+len(this.unique))
	all = append(all, this.rest...)
	for k, v := range this.unique {
		all = append(all, HTMLTag{
			Name:      k,
			InnerHTML: v,
		})
	}
	if this.deterministic {
		// Stable, so two tags sharing a name keep the order they were added in.
		sort.SliceStable(all, func(i, j int) bool {
			return all[i].Name < all[j].Name
		})
	}
	var b strings.Builder
	for _, tag := range all {
		b.WriteString("<" + tag.Name)
		// Map iteration is unordered, which would leave the output varying run
		// to run even with DeterministicOutput set.
		attrs := slices.Collect(maps.Keys(tag.HTMLTagAttributes))
		if this.deterministic {
			sort.Strings(attrs)
		}
		for _, attr := range attrs {
			b.WriteString(" " + attr + "=\"" + tag.HTMLTagAttributes[attr] + "\"")
		}
		b.WriteString(">")
		if tag.InnerHTML != "" {
			b.WriteString(tag.InnerHTML)
		}
		b.WriteString("</" + tag.Name + ">\n")
	}
	return b.String()
}
