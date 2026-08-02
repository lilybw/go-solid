package networking

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
type HTMLElementID = string

type HTMLTag struct {
	Name              HTMLTagName
	HTMLTagAttributes map[string]string
	InnerHTML         string
}
