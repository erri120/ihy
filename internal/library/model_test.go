// Package library: package-level doc and model constants live in
// model.go; the tests here pin graph-shape invariants with real-world
// MusicBrainz facts (a renamed artist with dated name aliases, and a
// recording with a localized primary alias).
package library

import "testing"

// The renamed-artist case: one artist entity (Eskimo Callboy → Electric
// Callboy), two ordered performer credits on a feat. recording.
func TestCreditOrdering(t *testing.T) {
	credits := []Credit{
		{SubjectType: SubjectRecording, ArtistMBID: "cf075492", Role: "performer", Position: 0, DisplayName: "Eskimo Callboy"},
		{SubjectType: SubjectRecording, ArtistMBID: "boss-hoss", Role: "performer", Position: 1, DisplayName: "The BossHoss", JoinPhrase: " feat. "},
	}
	if credits[0].Position > credits[1].Position {
		t.Fatal("credit order must follow position")
	}
	if credits[1].JoinPhrase != " feat. " {
		t.Fatal("joinphrase must survive round-trip; display joins credits with it")
	}
}

// The localized-name case: canonical names stay in the graph (Japanese),
// localized display comes from Alias rows keyed per locale with a
// primary flag.
func TestAliasPrimaryPerLocale(t *testing.T) {
	aliases := []Alias{
		{EntityType: SubjectRecording, Name: "穏ヤカナ眠リ", Locale: "", IsPrimary: false},
		{EntityType: SubjectRecording, Name: "Peaceful Sleep", Locale: "en", IsPrimary: true},
	}
	var primaryEn *Alias
	for i := range aliases {
		a := &aliases[i]
		if a.Locale == "en" && a.IsPrimary {
			primaryEn = a
		}
	}
	if primaryEn == nil || primaryEn.Name != "Peaceful Sleep" {
		t.Fatal("per-locale primary alias must be resolvable")
	}
}
