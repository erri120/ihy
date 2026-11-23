// Search page contract: the page and the live fragment are separate
// URLs; the input fetches the fragment, the page deep-links with ?q=.
package webui

import (
	"strings"
	"testing"
)

func TestSearchPage(t *testing.T) {
	t.Run("input fetches the fragment, not the page", func(t *testing.T) {
		html := renderComponent(t, SearchPage(NewTranslator("en"), "", nil))
		for _, want := range []string{
			`hx-get="/ui/search/results"`,
			`hx-target="#search-results"`,
			`id="search-results"`,
			`class="search"`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("search page missing %s", want)
			}
		}
		if strings.Contains(html, `hx-get="/ui/search"`) {
			t.Error("the input must fetch the fragment URL, not the page URL")
		}
	})
	t.Run("a query renders results server-side for deep links", func(t *testing.T) {
		results := []SearchResultVM{{Type: "artist", Mbid: "a1", Name: "Keiichi Okabe"}}
		html := renderComponent(t, SearchPage(NewTranslator("en"), "okabe", results))
		if !strings.Contains(html, "Keiichi Okabe") {
			t.Errorf("deep-linked query should render its results, got: %s", html)
		}
	})
	t.Run("no query renders an empty results container", func(t *testing.T) {
		html := renderComponent(t, SearchPage(NewTranslator("en"), "", nil))
		if strings.Contains(html, "NothingFound") {
			t.Error("an empty query must not render the nothing-found state on page load")
		}
	})
}
