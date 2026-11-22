// Artist page rendering: the header variants are data-driven by MB
// artist type, and the naming law (DESIGN.md §2, §5) holds: counts
// name their entity, the head carries at most two names.
package webui

import (
	"context"
	"strings"
	"testing"

	"ihy/internal/display"
)

func renderArtist(t *testing.T, vm ArtistVM, tab string) string {
	t.Helper()
	var sb strings.Builder
	if err := ArtistPage(NewTranslator("en"), vm, tab).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func baseVM(typ string) ArtistVM {
	vm := ArtistVM{
		Mbid:      "d82df34a-a1ff-45fa-ac50-c0b7454b201e",
		Name:      "Keiichi Okabe",
		Canonical: "岡部啓一",
		Type:      typ,
		Tabs:      artistTabs("d82df34a-a1ff-45fa-ac50-c0b7454b201e", "overview", NewTranslator("en")),
	}
	vm.Original = "岡部啓一"
	return vm
}

func TestArtistHeaderVariants(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		verb string
	}{
		{"Person", "born"},
		{"Group", "formed"},
		{"Character", "created"},
	} {
		vm := baseVM(tc.typ)
		vm.Meta = artistTypeLabel(NewTranslator("en"), tc.typ) + " · Japan · " + tc.verb + " 1969-05-26 · 36 recordings"
		html := renderArtist(t, vm, "overview")
		if !strings.Contains(html, " · "+tc.verb+" 1969-05-26 · ") {
			t.Errorf("type %s: meta should read %q, page: %s", tc.typ, tc.verb, vm.Meta)
		}
	}
}

func TestArtistOriginalNameLine(t *testing.T) {
	html := renderArtist(t, baseVM("Person"), "overview")
	if !strings.Contains(html, `class="a-original">岡部啓一</div>`) {
		t.Error("the canonical (original-script) name should render as the muted second line")
	}
}

func TestArtistFormerNameLine(t *testing.T) {
	vm := baseVM("Group")
	vm.Name, vm.Canonical, vm.Original = "Electric Callboy", "Electric Callboy", ""
	vm.FormerName = "formerly Eskimo Callboy (until 2022-03-09)"
	html := renderArtist(t, vm, "overview")
	if !strings.Contains(html, "formerly Eskimo Callboy (until 2022-03-09)") {
		t.Error("a rename alias with an end date should render as the former-name line")
	}
}

func TestArtistTabsRender(t *testing.T) {
	html := renderArtist(t, baseVM("Person"), "overview")
	for _, label := range []string{"Overview", "Albums", "Recordings", "Works", "Credits"} {
		if !strings.Contains(html, ">"+label+"</a>") {
			t.Errorf("tab %q missing", label)
		}
	}
}

func TestAliasSummary(t *testing.T) {
	aliases := []display.Alias{
		{Name: "Keiichi Okabe", Locale: "en", Primary: true},
		{Name: "岡部啓一", Locale: "ja", Primary: true},
		{Name: "岡部 啓一"},
		{Name: "Okabe, Keiichi"},
		{Name: " KEIICHI OKABE"},
	}
	got := aliasSummary(aliases, 4)
	want := "Keiichi Okabe (en, primary) · 岡部啓一 (ja, primary) · 岡部 啓一 · Okabe, Keiichi · +1 more"
	if got != want {
		t.Fatalf("aliasSummary: got %q, want %q", got, want)
	}
}
