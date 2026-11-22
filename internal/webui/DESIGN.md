# ihy Design System: Dendera

> The design of record for the ihy web UI (internal/webui). This file is
> the law; the live catalog of every component is the design page
> (/ui/design, internal/webui/design.templ), which renders the real
> templ components this project ships. Decision history lives in git.

Identity drawn from the name: Ihy, the Egyptian god of music, the
sistrum-playing child of Hathor at Dendera. Music as something played
for the goddess, not merely owned. Myth without kitsch.

Lapis and gold ochre on papyrus warmth. Two dark grounds (Lapis, Silt)
and one shared light theme (Papyrus). Serif display over sans body,
mono for the data underneath. The chrome stays quiet so covers, people
and relationships can speak.

## 1. Visual Theme & Atmosphere

Dendera is a warm-dark system with an Egyptian identity expressed through
material, not imagery: deep lapis blue, gold ochre, papyrus warmth. There
are no pyramids, no hieroglyphs, no gold gradients. The myth lives in
three places only: the palette, the sistrum mark, and the principle that
music is presented as relationships and context rather than a pile of files.

The atmosphere is a lamp-lit study, not a nightclub: dark themes feel like
warm stone and cool water, the light theme feels like morning in a
scriptorium. Identity is always visible (gold actions and marks on every
surface) but never decorative. Covers carry their own color; the chrome
never competes with artwork.

**Listening first**: ihy exists to play music. The primary views (library,
artists, albums) optimize for listening: play, queue, playlists, browse.
The MusicBrainz graph (works, credits, relationships) is the enhancer: it
powers search, grouping and detail surfaces that other players cannot
offer, and it stays one step behind the music: available on demand in
secondary surfaces (row detail, secondary tabs, the credits view), never
the foreground of a primary view.

**Key Characteristics:**
- Two dark grounds: Lapis (`#0d1830`, cool night water) and Silt (`#16130e`, warm Nile mud). In Silt, lapis retreats into panels and floats on the warm ground.
- One shared light theme: Papyrus (`#f3ecda`), lapis ink, ochre action.
- Gold (`#e3b94e` dark themes, `#8a6810` light) is the single accent: actions, active states, brand marks. Never a background fill for large surfaces, never text color for reading content.
- Serif display voices the entity; sans works the UI; mono tells the truth about data (durations, dates, counts, relationship verbs).
- Chips are quiet neutral pills. They belong to detail and data surfaces (row detail, credits view, graphs); primary listening rows stay lean, with credits one interaction away.
- The sistrum mark (line art, single-color stroke, gold) appears in details only: section markers, empty states, loading. Never large, never full-bleed, never animated idly.

## 2. Terms: work, recording, track, release

Definitions are MusicBrainz's (musicbrainz.org/doc/Work,
musicbrainz.org/doc/Recording); ihy resolves against them, so the UI uses
them precisely.

- **Work**: the distinct intellectual or artistic creation: the composition behind any number of performances. Distinct works are joined by work-to-work relationships: "Weight of the World / 壊レタ世界ノ歌", its later translated versions (English, Nouveau-FR) and its later version (the End of YoRHa) are separate works, and its arrangement (the Player[s] Remix) is another; the song is also "part of" the aggregate work NieR:Automata. Each work links to its own recordings through work-to-recording relationships (Okabe's 5:44 original of 壊レタ世界ノ歌, other artists' covers and instrumentals). Works have no artist of their own: every artist link is a relationship (composer, lyricist, writer, arranger, translator, publisher). A work is identified by its ISWC; works can be discrete (a song, an aria) or aggregate (an opera, a concept album); translations, parodies, medleys and mashups are distinct works; works are not necessarily musical (an audiobook chapter is a work).
- **Recording**: the distinct audio: one realized performance captured in production, before final mastering. Each track on a release points at exactly one recording; a recording can be linked to any number of tracks, across releases, compilations and editions. Studio, live and remixed versions are different recordings of the same work. Identified by its ISRC; its length is the median of its tracks' lengths.
- **Track**: a position on a release's medium: title, position, length, pointing at exactly one recording.
- **Release / release group**: the published artifact, and the family of editions it belongs to.

UI consequences:
- An artist's works tab lists compositions (work entities), never performances; the artist reaches works through composer/writer/arranger relationships, because works have no artist of their own.
- Track lists on release pages list tracks; lists on artist pages list recordings; counts always name the entity they count ("36 recordings", never "36 songs").
- "Song" does not appear in UI copy: the noun is work (composition), recording (audio), or track (release position).
- These terms are precision vocabulary for secondary surfaces (works tab, credits view, row detail): primary views speak listening language: play, queue, playlist, album, artist.
- A work can exist with zero recordings in the library, and a recording can be unlinked from any work: neither implies the other; the UI never hides one because the other is missing.
- Versions of a song are works joined by work-to-work relationships (later translated version, later version, arrangement, part of): the UI may group them under the original using those relationships as the grouping source, but never merges them into one work or files them as mere recordings of it.
- A work's recordings include other artists' covers and instrumentals (work-to-recording is about the composition, not the performer): a composer's artist page does not inherit those recordings.

## 3. Color Palette & Roles

Roles, not colors: `bg / surface / elev` are luminance steps, `text / muted`
are the only two text voices, `accent` carries identity and action. `hair`
and `chip-hair` are the two hairline weights. Tokens live in
`static/tokens.css`; nothing else may hardcode a hex.

### Lapis (dark, native)
- **Ground** (`#0d1830`): page background. Deep lapis, the night color of Dendera's temple.
- **Surface** (`#15223e`): cards, album tiles, panels.
- **Elevated** (`#1c2b4e`): hover, dropdowns, popovers.
- **Text** (`#f2e9d2`): papyrus white. Not pure white; the warmth is the point.
- **Muted** (`#a9a28c`): secondary text, metadata, inactive.
- **Accent** (`#e3b94e`): gold ochre. Actions, active states, brand mark.
- **Accent ink** (`#0d1830`): text/icons on gold. Ground-on-accent, never white-on-gold.

### Silt (dark, warm)
- **Ground** (`#16130e`): warm near-black, Nile mud.
- **Surface** (`#15223e`) and **Elevated** (`#1c2b4e`): deliberately kept lapis. Cool panels on warm ground are the theme's identity; the temperature difference does the separation work.
- Text, muted, accent identical to Lapis.

### Papyrus (light, shared)
- **Ground** (`#f3ecda`): papyrus.
- **Surface** (`#f9f4e6`), **Elevated** (`#fffdf2`): one and two steps up.
- **Text** (`#1c2440`): lapis ink. Muted (`#615f4f`): warm gray.
- **Accent** (`#8a6810`): gold darkened for daylight. Same role, same restraint.

### Verified contrast budgets (WCAG, computed)
- Lapis: text/bg 14.58, text/surface 13.05, muted/bg 6.92, muted/surface 6.19, accent-ink/gold 9.51.
- Silt: text/bg 15.32, text/surface 13.05, muted/bg 7.26, muted/surface 6.19, accent-ink/gold 9.51.
- Papyrus: text/bg 12.95, text/surface 13.89, muted/bg 5.47, muted/surface 5.87, accent-ink/ochre 5.06.
- Negative (terracotta): text on Lapis 6.71, on Silt 7.04, on Papyrus 5.07; danger fill with `--bg` ink: 6.71 dark, 5.07 light.
- Nothing ships below 4.5:1 for text or below 3:1 for meaningful non-text. Re-verify any new pair before it enters the system.

### Status: negative
- Terracotta (`#e08a63` dark themes, `#a5462c` light): errors and destructive actions only. The one non-brand functional hue; never decorative, never a large fill.

### Color is functional, never decorative
- Gold marks action and identity. It is never a large fill, never a text color for reading content, never a gradient.
- Gradients exist only inside artwork placeholders (as cover art, not chrome).
- Gray covers are a real case, not an edge case: libraries have items without artwork. The gray placeholder (`#8a857a` to `#57534b`) is designed, not defaulted.
- Credit/graph surfaces may use group hue; chrome may not. When MusicBrainz groups change upstream, unknown groups fall back to muted, never to a new hardcoded hue.

## 4. Typography Rules

### Families
- **Display/Serif**: `Literata`, with bundled `Noto Serif JP` for CJK, then `Georgia, serif`. Literata's even color harmonizes with Noto Serif JP's weight at display sizes, so no cross-script compensation rule is needed. Speaks: entity names, page titles, section headings.
- **UI/Sans**: `Instrument Sans`, with bundled `Noto Sans JP` for CJK, then `-apple-system, Segoe UI, sans-serif` fallbacks. Works: body, rows, buttons, tiles.
- **Mono**: `Spline Sans Mono`, with bundled `Noto Sans JP` carrying CJK inside mono contexts, then `ui-monospace, Menlo, monospace`. Tells the truth: durations, dates, counts, track numbers, relationship verbs, uppercase labels.
- JP faces ship as subset woff2 (plus OFL files) so rendering is identical across client OSes and offline; system CJK fonts are the last-resort fallback only. The JP subsets carry kana, CJK punctuation and the dev corpus's kanji; the `unicode-range` declarations live in `static/fonts/fonts.css`.

### Scale (rem-based; scale one root per device class)
| Token | Size | Font | Weight | Tracking | Use |
|---|---|---|---|---|---|
| `--text-display` | 34px | serif | 600 | -0.5px | page titles |
| `--text-title` | 26px | serif | 600 | -0.3px | entity names (desktop) |
| `--text-title-mobile` | 22px | serif | 600 | -0.3px | entity names (constrained) |
| `--text-reading` | 15px | sans | 400 | normal | longform reading |
| `--text-body` | 14px | sans | 400/500 | normal | UI body, minimum UI size |
| `--text-row` | 13px | sans | 500 | normal | track rows |
| `--text-caption` | 12px | sans | 400 | normal | role lines, captions |
| `--text-meta` | 11.5px | mono | 400 | normal | metadata, durations, dates |
| `--text-chip` | 10.5px | sans | 400 | normal | chips |
| `--text-label` | 10px | mono | 400 | +1.6px, uppercase | labels, kickers |

### Principles
- Three voices, strict roles. A serif never renders a duration; mono never renders an entity name.
- Two text colors only (`text`, `muted`). Hierarchy comes from family, size and space, never from a third color.
- Uppercase + wide tracking is the mono label voice (kickers, theme labels). Never applied to reading content.
- Localized names may render at any length; entity names wrap, they do not truncate. Qualifiers (locale, disambiguation) belong to the muted voice.

## 5. Component Stylings

The library ships as `static/components.css`: classes backed only by
tokens, one responsive markup pattern per component (no per-device
duplicates). The templ atoms (internal/webui/components.templ) are the
ONLY implementation: pages and the catalog compose them, never
hand-rolled copies. Every atom carries a render test asserting its
markup contract (internal/webui/components_test.go), and the stylesheet
is contract-tested too: the affordance rules (pointer, hover, focus,
disabled, pulse) and the verified contrast budgets are enforced on
every test run. The design page (dev only: `make dev-ui`, then
/ui/design, unauthenticated; internal/webui/design.templ) catalogs the
atoms as static specimens with generic content ("Button", "Tab 1",
"Placeholder"): application copy belongs to pages, not to the catalog.
Its theme switcher flips the whole page between Lapis, Silt and
Papyrus; it is a fixture affordance, not a product component.

### Core component rules

### Cards and panels
- Card: `--surface` background, `--radius-card`, no border on the outside; separation is a luminance step (and in Silt, a temperature step).
- Album tile: `--surface`, `--radius-tile`, 12px padding, cover at `--radius-cover` (56px on desktop rows).
- Album card: the browse-grid unit for albums (library page, artist pages): full-width square cover at `--radius-tile`, title in sans `--text-body` weight 600, artist line in sans `--text-caption` muted (omitted where the page context already names the artist), data line in mono `--text-meta` (year, counts). Hover and focus reveal a `.btn-play` circle on the cover's lower right; where hover is unavailable it stays visible; the card's own link stretches over the whole tile beneath it. Grid: `repeat(auto-fill, minmax(150px, 1fr))`, 130px floor below 600px.
- Cover placeholder: gray gradient, designed. Gradient artwork placeholders are the only gradients in the system.

### Chips (credits, tags)
- Neutral pill: `--chip-hair` border, `--muted` text, `--text-chip`, `--radius-pill`, 2px 9px padding.
- Never colored text, never colored borders in chrome. Group hue may ride only as an 8px dot inside the pill, and only where the legend exists.
- Chips carry raw relationship types verbatim (performance, vocal, arranger, writer, composer, producer, ...). The type name is upstream data; the UI does not rename or prettify it.
- Mobile: chips wrap to their own line under the title row. They never compete with the title for horizontal space.

### Buttons
- Primary: `--accent` fill, `--accent-ink` content, `--radius-pill`, 7px 16px, `--text-body` at weight 600. Full-width on mobile.
- Hover: `--elev` shift or `--accent-soft` wash; 120-220ms, ease-out.
- Minimum touch target on constrained surfaces: 44px.

### Track rows
- The same row pattern serves tracks (a release position) and recordings (distinct audio); counts and section labels follow whichever entity the list holds.
- Primary listening rows stay lean: number, title, duration, with a play affordance on hover. Clicking a row (or its play affordance) is a play intent; credits and relationships open through the row's info affordance (detail surface) or on data surfaces (credits view), never as the primary click.
- Desktop: one line, lean by default: number, title (weight 500, ellipsis), duration right-aligned. Hairline separators, no zebra.
- Mobile: the same markup adapts at the 600px breakpoint; the chips wrap to their own line under the title (title line keeps number, wrapped title, right-aligned duration). No per-device markup duplicates.
- Credits/role line: caption voice, muted; entity names in `text` weight 600.

### Artist header variants
- The artist header is data-driven by MB artist type (musicbrainz.org/doc/Artist): Person, Group, Character (Orchestra/Choir would follow the Group pattern).
- Always: display name for the locale (title), original-script name beneath it when it differs, type + area + life-span + recording count in the meta line, Play / Add-to-queue / favorite actions.
- Person: meta reads "born <begin-date>"; gender belongs to the about cells, not the head. Group: no gender anywhere; meta reads "formed <begin-date>"; a former name (rename alias with an end date) renders as the muted alias line ("formerly Eskimo Callboy (until 2022-03-09)"). Character: gender exists for characters (about cells only); meta reads "created <begin-date>" (the date the character concept was created; MB says characters get no end date); characters typically carry many localized aliases (Hatsune Miku: 9+), listed in the about cells.
- Reference composition: /ui/design catalogs the atoms; the artist page adopts them with real data from the dev library.

### Icons
- Set (13): search, play, pause, prev, next, heart, heart-filled, info, trash, home, library, settings, close. Assets live at `static/icons/<name>.svg` (served at `/ui/static/icons/<name>.svg`); 24x24 viewBox, paint attributes on the `g id="icon"` with `currentColor`, 2px round-cap strokes for line icons, filled glyphs for transport.
- Reference inline: `<svg class="icon" viewBox="0 0 24 24"><use href="/ui/static/icons/<name>.svg#icon"></use></svg>`, or the `Icon` templ component. `.icon` sizes (18px default, 16px in search, 20px in nav items); color inherits.
- Adding an icon means adding the asset and listing it in the design page's icon section. No emoji, no icon fonts, no inline path copies in markup.

### The sistrum mark
- The project mark: the sistrum of Ihy, the rattle shaken for Hathor. Serves as the project icon/favicon and as the in-UI motif.
- Assets, never inline copies: `static/mark/sistrum-line.svg` (stroke: currentColor; color it with `var(--accent)`) for in-UI section markers, empty states and loading at 14-32px; `static/mark/sistrum-solid.svg` (fixed brand gold) for favicon, tab and app icon, where it reads cleanly at 16px.
- Reference inline via `<svg viewBox="0 0 32 48"><use href="/ui/static/mark/sistrum-line.svg#mark"></use></svg>` (the asset exposes `id="mark"` with `stroke: currentColor` on the group; paint attributes live on the referenced group because root attributes do not transfer through `<use>`, and the consumer must declare its own viewBox because the source root's viewBox does not transfer either). This keeps the mark vector-crisp at any zoom and DPI while inheriting `color: var(--accent)`. Do not rasterize it (CSS mask, `<img>`, canvas): masks blur and band artifacts at non-1x zoom. The favicon references sistrum-solid.svg directly.
- Never animated continuously, never large, never multi-colored, never used as a bullet for content.

## 6. Layout Principles

- 8px spacing base: 4, 8, 12, 16, 22, 32, 48.
- Page designs compose page content only: head, tabs, panels. No breadcrumb or other URL-path display at the top of a page; location is carried by the app's navigation, not by the page itself. Global chrome (navigation, playbar) is designed in the catalog and composed in the app shell. A page design covers every tab/panel of the page, and tabs stay actually switchable, never a single painted tab. The artist page is the reference composition as it adopts the system.
- Overview pattern: primary views lead with listening: most played albums (grid, up to eight) then most played recordings (ten), each section head clickable into its full tab, with entity data last on the overview.
- Density is a feature: this is a library for people who read credits. Whitespace comes from spacing tokens, not from hiding metadata.
- Entity names wrap, never truncate. Metadata may truncate with ellipsis.
- Mobile-first wrapping rules: tiles stack, track rows split into title-line + chip-line, actions go full-width. Tablet restores the desktop structure at two columns. TV (10-foot): scale the root, not the layout; increase chip and dot sizes; do not invent a separate navigation paradigm.

## 7. Depth & Elevation
- Depth is luminance, not shadow: three steps (bg, surface, elev). In Silt, temperature (warm ground, cool panels) carries the same work.
- Hairlines: `--hair` for structural separators, `--chip-hair` for chip containers. No drop shadows in chrome; a soft shadow is permitted only for transient overlays (menus) at low opacity.
- Focus: visible ring on `--accent` at 2px, never removed.

## 8. Do's and Don'ts

### Do
- Lead every primary view with playable music: play and queue affordances on everything playable; credits and relationships surface on demand (row detail, secondary tabs), never as the foreground.
- Use gold for actions, active states and the sistrum mark; put `--accent-ink` on it, never white.
- Keep chips neutral and quiet; let the raw relationship type be the information.
- Show localized display names as primary content. Page heads carry at most two names: the display name for the user's locale and the original-script name beneath it when it differs (one name when they are the same). Locale qualifiers ("(ja, primary)") and full alias lists live in detail surfaces, never repeated in a page head.
- Treat gray covers as designed states.
- Let track rows split (title line / chip line) when width demands it.
- Verify every new color pair numerically before it ships.

### Don't
- Don't let data vocabulary (work, recording, ISWC, relationship verbs) dominate primary views; don't put credit chips inline on listening rows by default.
- Don't use gold as a background for large surfaces, as reading-text color, or in gradients in the chrome.
- Don't color relationship chips in chrome, and never hardcode a hue map for relationship groups; use the dot pattern with a muted fallback for unknown groups.
- Don't show "Various Artists" where ihy's resolution can name the real artists; upstream hacks are hidden, not displayed.
- Don't truncate entity names or add a third text color.
- Don't use emoji, glassmorphism, neon gradients, or drop shadows on chrome.
- Don't animate the sistrum idly or scale it past a section marker.
- Don't invent a second accent color. Gold + lapis + papyrus + muted gray is the whole voice.

## 9. Responsive Behavior & Device Matrix

| Device | Width | Root scale | Layout |
|---|---|---|---|
| Mobile | 320-600px | 1.0 | single column; tiles stack; rows split; full-width actions; 44px targets |
| Tablet | 600-1024px | 1.0-1.0625 | desktop structure, two-column grids |
| Desktop | 1024px+ | 1.0-1.125 | full structure, centered column |
| TV / 10-foot | any (large DPI-scaled viewport) | 1.5-2.0 | desktop structure scaled by root; chips and dots enlarged; hover/focus-driven navigation; no hover-only affordances for critical actions |

- All sizing is rem-based; devices scale one root. No px-based type.
- Designs are responsive single instances, not per-device mockups: exercise them by resizing the viewport (check 390, 768 and desktop widths). If a design only exists as a static mobile painting next to a desktop painting, it is not finished.
- Dual theme is semantic: Papyrus shares Lapis/Silt's role structure, never their values.
- Reduced-motion: swaps become instant, the sistrum never moves.

## 10. Working With the System

### Real data
- `internal/musicbrainz/testdata/replays/` holds the MusicBrainz replay corpus of the dev library (208 responses: 51 artists, 100 recordings, 3 releases, 49 works), one JSON per resolved entity. Never invent artist, album or track names when the corpus can supply them.
- The dev environment (make dev / dev-ui) mounts a fixture library with real cover art and runs replay-first offline; artwork serves through the app's artwork endpoint, so designs reference runtime URLs, never fixture paths.
- The design catalog itself uses generic specimen content; real data belongs to page designs.

### Iteration Guide
1. Roles before colors: any new token must state which role it extends, never a raw hex in component CSS.
2. Gold is spent, not spent wildly: one accent per view for action; identity is the sistrum and the palette, not more gold.
3. Chips speak upstream verbatim; mute the chrome so the verbs can be read. Primary rows stay lean: the data lives one interaction away.
4. Listening first: every primary view leads with playable music; data renders on demand, one interaction away.
5. Check every color decision in both dark grounds and Papyrus; Silt's cool-on-warm separation is the one that fails quietly.
6. When MusicBrainz changes, only the mapping changes, never the tokens.
7. A design decision is not made until it lives here and on /ui/design (dev only, no auth); the templ components are the only implementation. Run `make dev-ui` and watch it reload.
