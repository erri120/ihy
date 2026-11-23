// Component tests: every atom's markup contract (DESIGN.md §5: the
// catalog markup is what the real UI emits), plus the stylesheet
// contract: affordance rules for interactive components and the
// verified contrast budgets, enforced on every test run.
package webui

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), io.Writer(&sb)); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestIconAtom(t *testing.T) {
	html := renderComponent(t, Icon("heart"))
	if !strings.Contains(html, `class="icon"`) || !strings.Contains(html, `/ui/static/icons/heart.svg#icon`) {
		t.Errorf("Icon should reference the asset via <use>, got: %s", html)
	}
}

func TestChipAtomVerbatim(t *testing.T) {
	// The raw upstream type renders verbatim: no prettifying.
	html := renderComponent(t, Chip("phonographic copyright"))
	if !strings.Contains(html, `class="chip"`) || !strings.Contains(html, "phonographic copyright</span>") {
		t.Errorf("Chip should render the raw type verbatim, got: %s", html)
	}
}

func TestChipDotAtom(t *testing.T) {
	html := renderComponent(t, ChipDot("performance", "#8f6bd0"))
	if !strings.Contains(html, `class="dot"`) || !strings.Contains(html, "#8f6bd0") {
		t.Errorf("ChipDot should carry the 8px dot with the given hue, got: %s", html)
	}
}

func TestCoverAtom(t *testing.T) {
	empty := renderComponent(t, Cover("", "No artwork", "cover-l"))
	if !strings.Contains(empty, "cover-empty") || strings.Contains(empty, "<img") {
		t.Errorf("an empty src renders the designed placeholder, got: %s", empty)
	}
	img := renderComponent(t, Cover("/ui/static/x.jpg", "Album cover", "cover-m"))
	if !strings.Contains(img, `<img class="cover cover-m"`) || !strings.Contains(img, `alt="Album cover"`) {
		t.Errorf("a src renders an img with alt, got: %s", img)
	}
}

func TestTrackRowAtom(t *testing.T) {
	t.Run("static specimen has no behavior", func(t *testing.T) {
		html := renderComponent(t, TrackRow(NewTranslator("en"), "1", "Title", "5:44", "", "", nil))
		if strings.Contains(html, "hx-post") || strings.Contains(html, `role="button"`) {
			t.Errorf("static specimen should carry no htmx wiring: %s", html)
		}
	})
	t.Run("playable row posts the play intent", func(t *testing.T) {
		html := renderComponent(t, TrackRow(NewTranslator("en"), "1", "Title", "5:44",
			"/ui/play?recording=r1", "", nil))
		for _, want := range []string{`role="button"`, `tabindex="0"`, `hx-post="/ui/play?recording=r1"`, `hx-swap="none"`, `keydown[key=='Enter']`} {
			if !strings.Contains(html, want) {
				t.Errorf("playable row missing %s", want)
			}
		}
	})
	t.Run("info affordance loads the detail into the slot", func(t *testing.T) {
		html := renderComponent(t, TrackRow(NewTranslator("en"), "1", "Title", "5:44",
			"/ui/play?recording=r1", "/ui/artists/a/recordings/r1/detail", nil))
		if !strings.Contains(html, `hx-get="/ui/artists/a/recordings/r1/detail"`) ||
			!strings.Contains(html, `hx-target="next .tr-detail-slot"`) ||
			!strings.Contains(html, `class="tr-detail-slot"`) {
			t.Errorf("detail affordance wiring missing: %s", html)
		}
	})
	t.Run("rendered detail bypasses the slot", func(t *testing.T) {
		detail := ArtistRecordingDetail(NewTranslator("en"), nil, nil)
		html := renderComponent(t, TrackRow(NewTranslator("en"), "1", "T", "0:00", "/ui/play?recording=r", "", detail))
		if strings.Contains(html, `class="tr-detail-slot"`) || !strings.Contains(html, "tr-detail") {
			t.Errorf("a rendered detail should replace the slot: %s", html)
		}
	})
}

func TestNavAtom(t *testing.T) {
	t.Run("real destinations, active state, no htmx wiring", func(t *testing.T) {
		html := renderComponent(t, Nav(NewTranslator("en"), "search"))
		for _, want := range []string{
			`id="shell-nav"`,
			`hx-preserve="true"`,
			`class="shell-brand" href="/"`,
			`class="nav-item is-active" href="/ui/search"`,
			`class="nav-item" href="/"`,
			`/ui/static/icons/search.svg#icon`,
			`/ui/static/icons/library.svg#icon`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("nav missing %s, got: %s", want, html)
			}
		}
		if strings.Contains(html, "hx-get") || strings.Contains(html, "hx-post") {
			t.Errorf("nav links must rely on boosted navigation, got: %s", html)
		}
	})
	t.Run("logout opts out of boost", func(t *testing.T) {
		html := renderComponent(t, Nav(NewTranslator("en"), ""))
		if !strings.Contains(html, `action="/logout"`) || !strings.Contains(html, `hx-boost="false"`) {
			t.Errorf("logout must be a real (unboosted) navigation, got: %s", html)
		}
	})
}

func TestButtonAtom(t *testing.T) {
	primary := renderComponent(t, Button(NewTranslator("en"), "Play", "", nil))
	if !strings.Contains(primary, `class="btn"`) || !strings.Contains(primary, ">Play</button>") {
		t.Errorf("primary button wrong: %s", primary)
	}
	ghost := renderComponent(t, Button(NewTranslator("en"), "Add to queue", "ghost", nil))
	if !strings.Contains(ghost, `class="btn btn-ghost"`) {
		t.Errorf("ghost variant wrong: %s", ghost)
	}
	disabled := renderComponent(t, Button(NewTranslator("en"), "Queued", "", templ.Attributes{"disabled": true, "title": "later"}))
	if !strings.Contains(disabled, "disabled") || !strings.Contains(disabled, `title="later"`) {
		t.Errorf("attrs not carried: %s", disabled)
	}
}

func TestIconButtonAtom(t *testing.T) {
	idle := renderComponent(t, IconButton(NewTranslator("en"), "heart", false, nil))
	if !strings.Contains(idle, `class="btn-icon"`) || !strings.Contains(idle, "heart.svg#icon") {
		t.Errorf("icon button wrong: %s", idle)
	}
	active := renderComponent(t, IconButton(NewTranslator("en"), "heart-filled", true, templ.Attributes{"aria-label": "Favorited"}))
	if !strings.Contains(active, `class="btn-icon is-active"`) || !strings.Contains(active, `aria-label="Favorited"`) {
		t.Errorf("active state wrong: %s", active)
	}
}

func TestTabsNavAtom(t *testing.T) {
	tabs := []TabVM{
		{Name: "one", Label: "Tab 1", URL: "/x?tab=one", Fragment: "/x/tab/one", Active: true},
		{Name: "two", Label: "Tab 2", URL: "/x?tab=two", Fragment: "/x/tab/two"},
	}
	wired := renderComponent(t, TabsNav(NewTranslator("en"), tabs, "Sections", "panel"))
	for _, want := range []string{
		`class="tab is-active" href="/x?tab=one"`,
		`hx-get="/x/tab/two"`,
		`hx-target="#panel"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(wired, want) {
			t.Errorf("wired tabs missing %s: %s", want, wired)
		}
	}
	static := renderComponent(t, TabsNav(NewTranslator("en"), tabs, "Sections", ""))
	if strings.Contains(static, "hx-get") {
		t.Errorf("static specimen must carry no htmx wiring: %s", static)
	}
}

func TestEmptyAtom(t *testing.T) {
	withTitle := renderComponent(t, Empty(NewTranslator("en"), "Nothing here yet", "Body line."))
	if !strings.Contains(withTitle, `class="empty-title">Nothing here yet<`) || !strings.Contains(withTitle, "sistrum-line.svg#mark") {
		t.Errorf("empty with title wrong: %s", withTitle)
	}
	titleless := renderComponent(t, Empty(NewTranslator("en"), "", "Body line."))
	if strings.Contains(titleless, "empty-title") || !strings.Contains(titleless, `class="empty-body">Body line.<`) {
		t.Errorf("titleless empty must omit the title line: %s", titleless)
	}
}

func TestAlbumCardAtom(t *testing.T) {
	t.Run("static specimen has no behavior", func(t *testing.T) {
		html := renderComponent(t, AlbumCard(NewTranslator("en"), AlbumVM{Name: "Album", FirstReleaseDate: "2017"}, "Artist"))
		if strings.Contains(html, "ac-link") || strings.Contains(html, "hx-post") {
			t.Errorf("static card must carry no link or play intent: %s", html)
		}
		if !strings.Contains(html, `class="ac-artist">Artist<`) {
			t.Errorf("artist line missing: %s", html)
		}
	})
	t.Run("wired card links and posts a play intent", func(t *testing.T) {
		vm := AlbumVM{Mbid: "rg-1", Name: "Album", FirstReleaseDate: "2017"}
		html := renderComponent(t, AlbumCard(NewTranslator("en"), vm, ""))
		for _, want := range []string{
			`href="/ui/release-groups/rg-1"`,
			`hx-post="/ui/play?release-group=rg-1"`,
			`hx-swap="none"`,
			`class="btn-icon btn-play ac-play"`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("wired card missing %s: %s", want, html)
			}
		}
		if strings.Contains(html, "ac-artist") {
			t.Errorf("empty artist must omit the artist line: %s", html)
		}
	})
}

func TestTrackDetailAtom(t *testing.T) {
	full := renderComponent(t, TrackDetail(NewTranslator("en"), []string{"performance", "arranger"}, "Weight of the World"))
	if !strings.Contains(full, `class="chip">performance<`) || !strings.Contains(full, `class="chip">arranger<`) {
		t.Errorf("roles must render verbatim as chips: %s", full)
	}
	if !strings.Contains(full, `Work:</span> <b>Weight of the World</b>`) && !strings.Contains(full, "Work:") {
		t.Errorf("work line missing: %s", full)
	}
	none := renderComponent(t, TrackDetail(NewTranslator("en"), nil, ""))
	if !strings.Contains(none, "No credits or linked works") {
		t.Errorf("empty detail must render the no-details line: %s", none)
	}
}

// ---- Stylesheet contract -------------------------------------------------

func designCSS(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// cssRule asserts a required (selector, property: value) pair exists:
// the affordance rules that make components read clickable and states
// visible live in CSS, where rendering tests cannot see them.
func cssRule(t *testing.T, css, selector, propValue string) {
	t.Helper()
	escaped := regexp.QuoteMeta(selector + " { " + propValue)
	if !regexp.MustCompile(escaped).MatchString(css) {
		t.Errorf("stylesheet contract violated: missing %q", selector+" { "+propValue)
	}
}

func TestCSSAffordanceContract(t *testing.T) {
	css := designCSS(t, "components.css")
	cssRule(t, css, `.tr[role="button"]`, "cursor: pointer; }")
	cssRule(t, css, `.tr[role="button"]:hover`, "background: var(--surface); }")
	cssRule(t, css, `:focus-visible`, "outline: 2px solid var(--accent);")
	cssRule(t, css, `.tr.is-playing`, "background: var(--accent-soft); }")
	cssRule(t, css, `[disabled], .is-disabled`, "opacity: 0.45;")
	// Shell: the nav is one markup rendered as a sticky sidebar, and
	// the active item carries the accent; the mobile bar clears the
	// viewport edge (safe area) for tap targets.
	cssRule(t, css, `.shell-nav`, "position: sticky; top: 0;")
	cssRule(t, css, `.nav-item.is-active`, "color: var(--text); background: var(--accent-soft); box-shadow: inset 2px 0 0 var(--accent); }")
	cssRule(t, css, `body.with-shell`, "display: block; padding-bottom: calc(64px + env(safe-area-inset-bottom, 0px)); }")
	// The play circle is always the gold action (roles, not colors).
	cssRule(t, css, `.btn-play`, "background: var(--accent); color: var(--accent-ink); width: 44px;")
}

// ---- Contrast budgets (DESIGN.md §3, verified pairs) ----------------------

type rgba struct{ r, g, b float64 }

// relLuminance implements WCAG 2.x relative luminance for sRGB.
func relLuminance(c rgba) float64 {
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

func contrast(a, b rgba) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func parseHex(s string) (rgba, error) {
	s = strings.TrimPrefix(s, "#")
	var r, g, b int
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return rgba{}, err
	}
	return rgba{float64(r) / 255, float64(g) / 255, float64(b) / 255}, nil
}

func TestTokenContrastBudgets(t *testing.T) {
	tokens := designCSS(t, "tokens.css")
	// One verified pair per role relationship, all three themes: the
	// budgets from DESIGN.md §3 (text pairs >= 4.5, accent ink >= 3).
	pairs := map[string][][]string{
		"lapis":   {{"#0d1830", "#f2e9d2"}, {"#0d1830", "#a9a28c"}, {"#15223e", "#f2e9d2"}, {"#15223e", "#a9a28c"}, {"#e3b94e", "#0d1830"}},
		"silt":    {{"#16130e", "#f2e9d2"}, {"#16130e", "#a9a28c"}, {"#15223e", "#f2e9d2"}, {"#15223e", "#a9a28c"}, {"#e3b94e", "#0d1830"}},
		"papyrus": {{"#f3ecda", "#1c2440"}, {"#f3ecda", "#615f4f"}, {"#f9f4e6", "#1c2440"}, {"#f9f4e6", "#615f4f"}, {"#8a6810", "#fffdf2"}},
	}
	for theme, ps := range pairs {
		// The theme block must actually declare these values.
		block := regexp.MustCompile(`\[data-theme="` + theme + `"\] \{([\s\S]*?)\}`).FindString(tokens)
		if block == "" {
			t.Fatalf("theme %s missing from tokens.css", theme)
		}
		for _, p := range ps {
			fg, err1 := parseHex(p[1])
			bg, err2 := parseHex(p[0])
			if err1 != nil || err2 != nil {
				t.Fatalf("bad hex in %s pair %v", theme, p)
			}
			if !strings.Contains(block, p[0]) {
				t.Errorf("theme %s does not declare %s; update the verified pairs", theme, p[0])
			}
			if ratio := contrast(fg, bg); ratio < 4.5 {
				t.Errorf("theme %s pair %s/%s: %.2f:1 below the 4.5 budget", theme, p[0], p[1], ratio)
			}
		}
	}
}
