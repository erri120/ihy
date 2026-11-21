// UI string localization with go-i18n v2. The English file is the
// source of truth (no hashes needed); other locales are translations
// and may be partial: missing keys fall back to English, and an ID
// missing everywhere renders as the raw ID instead of panicking.
// The UI locale currently follows Accept-Language; tying it to a user
// preference comes with the display-prefs UI.
package webui

import (
	"embed"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales
var localeFS embed.FS

// bundle holds the UI messages for the process lifetime.
var bundle *i18n.Bundle

func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		panic(fmt.Sprintf("locales: %v", err))
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(localeFS, "locales/"+e.Name()); err != nil {
			panic(fmt.Sprintf("load %s: %v", e.Name(), err))
		}
	}
}

// T resolves UI strings for one request.
type T struct {
	localizer *i18n.Localizer
	Lang      language.Tag
}

// NewTranslator matches the Accept-Language header against the loaded
// locales and falls back to the bundle default (English).
func NewTranslator(acceptLanguage string) *T {
	matcher := language.NewMatcher(bundle.LanguageTags())
	lang, _ := language.MatchStrings(matcher, acceptLanguage)
	return &T{
		localizer: i18n.NewLocalizer(bundle, acceptLanguage),
		Lang:      lang,
	}
}

// Msg resolves a simple (non-plural) message. A missing ID renders as
// the ID itself: loud, but no panic and no blank UI.
func (t *T) Msg(id string) string {
	s, err := t.localizer.Localize(&i18n.LocalizeConfig{MessageID: id})
	if err != nil {
		return id
	}
	return s
}

// Plural resolves a message with a CLDR plural count. The count is
// available in the message template as {{.PluralCount}}.
func (t *T) Plural(id string, count int) string {
	s, err := t.localizer.Localize(&i18n.LocalizeConfig{
		MessageID:   id,
		PluralCount: count,
	})
	if err != nil {
		return fmt.Sprintf("%s (%d)", id, count)
	}
	return s
}

// kindID maps a search-hit entity type to its kind message ID.
func kindID(entityType string) string {
	switch entityType {
	case "artist":
		return "KindArtist"
	case "release_group":
		return "KindAlbum"
	case "release":
		return "KindRelease"
	case "recording":
		return "KindTrack"
	default:
		return entityType
	}
}
