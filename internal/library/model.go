// Package library holds the domain model of the resolved library graph.
//
// One type per MusicBrainz entity, keyed by MBID. Names here are
// canonical MusicBrainz names; user-facing display names are computed
// at API time from Alias rows, per user preference.
package library

// MBID is a MusicBrainz Identifier (UUID string form).
type MBID string

// Artist is a MusicBrainz artist: performer, composer, producer…
// The graph never stores "who sang on what" here; that lives in Credit.
type Artist struct {
	MBID           MBID
	Name           string // canonical, e.g. "Electric Callboy"
	SortName       string // Latin per MB style, e.g. "Electric Callboy"
	Disambiguation string
	Type           string // Group | Person | …
	Country        string
	BeginDate      string
	EndDate        string
	Ended          bool
}

// ReleaseGroup groups the different versions of an album
// (remasters, reissues, regional releases are distinct Releases below).
type ReleaseGroup struct {
	MBID             MBID
	Title            string
	PrimaryType      string   // album | ep | single | …
	SecondaryTypes   []string // soundtrack, live, compilation…
	FirstReleaseDate string
}

// Release is one concrete published version (a country/date/label
// variant) inside a ReleaseGroup.
type Release struct {
	MBID             MBID
	ReleaseGroupMBID MBID
	Title            string
	Date             string
	Country          string
	Barcode          string
	Labels           []ReleaseLabel
}

// ReleaseLabel ties a release to its label and catalogue number.
type ReleaseLabel struct {
	LabelMBID     MBID
	LabelName     string
	CatalogNumber string
}

// Recording is one specific recorded performance of a work
// ("Peaceful Sleep" the track on a given release).
type Recording struct {
	MBID             MBID
	Title            string // canonical, may be Japanese, e.g. 穏ヤカナ眠リ
	LengthMS         int
	Disambiguation   string
	FirstReleaseDate string
}

// Work is the composed song behind one or more recordings.
type Work struct {
	MBID  MBID
	Title string
	Type  string
	ISWC  string
}

// Credit is one ordered artist credit on a subject: who did what,
// in what order, displayed how.
//
// Real-world example: a recording "Hypa Hypa (feat. The BossHoss)"
// carries ordered performer credits [Eskimo Callboy, The BossHoss] at
// positions 0,1. Soundtrack recordings carry composer, arranger and
// mixer credits tied to artists by MBIDs (MUSICBRAINZ_COMPOSERID).
//
// Role is the raw MusicBrainz relationship type string, stored
// verbatim on purpose: the vocabulary is upstream-maintained and
// grows, so a local fixed enum would only add tech debt. Instrument
// and vocal attributes ("guitar", "lead vocals") live in Details.
type Credit struct {
	SubjectType SubjectType
	SubjectMBID MBID
	ArtistMBID  MBID
	Role        string // raw MB relationship type, verbatim
	Details     string // attributes: "guitar", "lead vocals", ...
	Position    int
	DisplayName string // credit name as MB recorded it for this credit
	JoinPhrase  string // e.g. " feat. ", "", ", "
}

// SubjectType says what an alias or credit attaches to.
type SubjectType string

const (
	SubjectArtist       SubjectType = "artist"
	SubjectReleaseGroup SubjectType = "release_group"
	SubjectRelease      SubjectType = "release"
	SubjectRecording    SubjectType = "recording"
	SubjectWork         SubjectType = "work"
)

// Alias is an alternative name synced from MusicBrainz. Two jobs:
// per-locale primary names (岡部啓一 → en: "Keiichi Okabe",
// recording 穏ヤカナ眠リ → en: "Peaceful Sleep") and dated historical
// names (Eskimo Callboy: end 2022-03-09; Electric Callboy: begin
// 2022-03-09). Unlocalized aliases have Locale == "".
type Alias struct {
	EntityMBID MBID
	EntityType SubjectType
	Name       string
	SortName   string
	Locale     string
	Type       string // artist name | search hint | …
	IsPrimary  bool
	BeginDate  string
	EndDate    string
}

// File is one physical audio file on disk, linked into the graph.
type File struct {
	ID            int64
	LibraryID     int64
	Path          string // relative to the library root
	Mtime         string
	SizeBytes     int64
	Format        string
	ReleaseMBID   MBID
	RecordingMBID MBID
}

// Track places a recording inside a release's medium order.
type Track struct {
	ReleaseMBID   MBID
	RecordingMBID MBID
	FileID        int64
	MediumNumber  int
	TrackNumber   int
}

// User is a listener. Library graph state above is shared; everything
// user-owned (sessions, tokens, preferences, events) keys off User.ID.
type User struct {
	ID           int64
	Name         string
	PasswordHash string
	CreatedAt    string
}

// DisplayPref is one entity-type slice of a user's name-display
// profile: an ordered locale chain plus how to treat renamed artists.
type DisplayPref struct {
	UserID        int64
	EntityType    SubjectType
	LocaleChain   []string // e.g. ["ja", "en"] or ["en"]
	HistoryPolicy string   // current | at_release | pinned
}
