package httpapi

import (
	"bytes"
	"html"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var serverMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func renderServerMarkdown(source string) string {
	var rendered bytes.Buffer
	if err := serverMarkdown.Convert([]byte(source), &rendered); err != nil {
		return "<pre>" + html.EscapeString(source) + "</pre>"
	}
	return rendered.String()
}
