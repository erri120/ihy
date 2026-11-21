package musicbrainz

// Typed views of the MusicBrainz WS/2 JSON payloads, restricted to the
// fields the resolver consumes. Replay files are trimmed to these too.

// MBID is a MusicBrainz Identifier (UUID string form).
type MBID string

// Alias is an alternative name: per-locale primary names ("Peaceful
// Sleep" for a Japanese recording title) and dated historical names
// (Eskimo Callboy ended 2022-03-09).
type Alias struct {
	Name     string `json:"name"`
	SortName string `json:"sort-name,omitempty"`
	Locale   string `json:"locale,omitempty"`
	Type     string `json:"type,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
	Begin    string `json:"begin,omitempty"`
	End      string `json:"end,omitempty"`
}

// ArtistRef is an artist reference inside an artist-credit or
// relationship; only the identity fields are consumed.
type ArtistRef struct {
	MBID           MBID   `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation,omitempty"`
}

// ArtistCreditEntry is one position in an ordered artist credit,
// including the credit-level display name and the joinphrase needed
// to render "Eskimo Callboy feat. The BossHoss" exactly.
type ArtistCreditEntry struct {
	Name       string    `json:"name"`
	JoinPhrase string    `json:"joinphrase,omitempty"`
	Artist     ArtistRef `json:"artist"`
}

// LifeSpan is the artist's activity range.
type LifeSpan struct {
	Begin string `json:"begin,omitempty"`
	End   string `json:"end,omitempty"`
	Ended bool   `json:"ended,omitempty"`
}

// Artist is a full artist entity.
type Artist struct {
	MBID           MBID     `json:"id"`
	Name           string   `json:"name"`
	SortName       string   `json:"sort-name"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Type           string   `json:"type,omitempty"`
	Country        string   `json:"country,omitempty"`
	LifeSpan       LifeSpan `json:"life-span"`
	Aliases        []Alias  `json:"aliases"`
}

// ReleaseGroup is the album across versions; remasters and reissues
// are distinct releases underneath.
type ReleaseGroup struct {
	MBID             MBID                `json:"id"`
	Title            string              `json:"title"`
	PrimaryType      string              `json:"primary-type"`
	SecondaryTypes   []string            `json:"secondary-types"`
	FirstReleaseDate string              `json:"first-release-date"`
	ArtistCredit     []ArtistCreditEntry `json:"artist-credit"`
	Aliases          []Alias             `json:"aliases"`
}

// LabelRef is a label referenced by a release.
type LabelRef struct {
	MBID MBID   `json:"id"`
	Name string `json:"name"`
}

// LabelInfo ties a release to a label and catalogue number.
type LabelInfo struct {
	CatalogNumber string    `json:"catalog-number,omitempty"`
	Label         *LabelRef `json:"label,omitempty"`
}

// ReleaseGroupRef appears nested in release payloads.
type ReleaseGroupRef struct {
	MBID MBID `json:"id"`
}

// Release is one concrete published version of a release-group.
type Release struct {
	MBID         MBID                `json:"id"`
	Title        string              `json:"title"`
	Date         string              `json:"date,omitempty"`
	Country      string              `json:"country,omitempty"`
	Barcode      string              `json:"barcode,omitempty"`
	ReleaseGroup ReleaseGroupRef     `json:"release-group"`
	ArtistCredit []ArtistCreditEntry `json:"artist-credit"`
	LabelInfo    []LabelInfo         `json:"label-info"`
}

// Relation is a typed relationship; target entity payloads are
// references only.
type Relation struct {
	Type       string   `json:"type"`
	TargetType string   `json:"target-type"`
	Direction  string   `json:"direction,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	// Attribute metadata: MusicBrainz documents attribute-credits as
	// the "credited as" names (e.g. guitar credited as "Fender
	// Stratocaster"). Absent from most fixtures, kept for decoding.
	AttributeIDs     map[string]string `json:"attribute-ids,omitempty"`
	AttributeValues  map[string]string `json:"attribute-values,omitempty"`
	AttributeCredits map[string]string `json:"attribute-credits,omitempty"`
	Work             *WorkRef          `json:"work,omitempty"`
	Artist           *ArtistRef        `json:"artist,omitempty"`
}

// AttributeCredit is a credited-as name for one relationship
// attribute type; see Relation.AttributeCredits.

// WorkRef is a work reference inside a relationship.
type WorkRef struct {
	MBID  MBID   `json:"id"`
	Title string `json:"title"`
}

// Recording is one specific recorded performance of a work.
type Recording struct {
	MBID             MBID                `json:"id"`
	Title            string              `json:"title"`
	LengthMS         int                 `json:"length"`
	Disambiguation   string              `json:"disambiguation,omitempty"`
	FirstReleaseDate string              `json:"first-release-date,omitempty"`
	ArtistCredit     []ArtistCreditEntry `json:"artist-credit"`
	Relations        []Relation          `json:"relations"`
	Aliases          []Alias             `json:"aliases"`
}

// Work is the composed song behind one or more recordings.
type Work struct {
	MBID    MBID    `json:"id"`
	Title   string  `json:"title"`
	Type    string  `json:"type,omitempty"`
	Aliases []Alias `json:"aliases"`
}
