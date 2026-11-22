-- 0001: core library graph.
--
-- Principles:
--   * Shared musical entities are keyed by MusicBrainz MBIDs (uuid);
--     source files carry these IDs in standard tags.
--   * Multi-valued facts are rows (credits, aliases), never delimiter
--     strings. Real collections have multi-artist feature credits and
--     localized (e.g. Japanese) names throughout.
--   * Names stored here are MusicBrainz CANONICAL names; display names
--     are computed per user from `alias` at API time.

-- All MusicBrainz dates are stored as text: the API allows partial
-- values ("2017-03", "????-03-20") and they are display data; ISO-style
-- prefixes still sort lexicographically.
CREATE TABLE artist (
    mbid           uuid PRIMARY KEY,
    name           text NOT NULL,
    sort_name      text NOT NULL,
    disambiguation text NOT NULL DEFAULT '',
    type           text,
    country        text,
    begin_date     text,
    end_date       text,
    ended          boolean NOT NULL DEFAULT false
);

CREATE TABLE release_group (
    mbid                uuid PRIMARY KEY,
    title               text NOT NULL,
    primary_type        text NOT NULL,          -- album | ep | single | ...
    secondary_types     text[] NOT NULL DEFAULT '{}',  -- soundtrack, live, ...
    first_release_date  text
);

CREATE TABLE release (
    mbid               uuid PRIMARY KEY,
    release_group_mbid uuid NOT NULL REFERENCES release_group,
    title              text NOT NULL,
    date               text,
    country            text,
    barcode            text
);

CREATE TABLE label (
    mbid  uuid PRIMARY KEY,
    name  text NOT NULL
);

CREATE TABLE release_label (
    release_mbid   uuid NOT NULL REFERENCES release,
    label_mbid     uuid NOT NULL REFERENCES label,
    catalog_number text,
    PRIMARY KEY (release_mbid, label_mbid, catalog_number)
);

CREATE TABLE recording (
    mbid                uuid PRIMARY KEY,
    title               text NOT NULL,
    length_ms           integer,
    disambiguation      text NOT NULL DEFAULT '',
    first_release_date  text,
    -- Search document, maintained by the resolver: recording title,
    -- recording aliases, linked work titles and work aliases. Weighted
    -- (title > aliases > work) so a track's own name outranks the
    -- composition behind it. Users search for what they know and see
    -- the track.
    search_vector       tsvector NOT NULL DEFAULT to_tsvector('simple', '')
);
CREATE INDEX recording_search_idx ON recording USING GIN (search_vector);

-- The composition behind a recording ("Weight of the World" performed
-- by multiple recordings). Recording→work is a performance relation.
CREATE TABLE work (
    mbid  uuid PRIMARY KEY,
    title text NOT NULL,
    type  text,
    iswc  text
);

CREATE TABLE recording_work (
    recording_mbid uuid NOT NULL REFERENCES recording,
    work_mbid      uuid NOT NULL REFERENCES work,
    PRIMARY KEY (recording_mbid, work_mbid)
);

-- Ordered role credits on any named subject. One row per (subject,
-- artist, role, position). Covers "Eskimo Callboy feat. The BossHoss"
-- (two ordered performer credits, positions 0/1) as well as composer/
-- arranger/mixer credits common on soundtrack recordings.
-- role stores MusicBrainz relationship type strings VERBATIM (no local
-- enum: the vocabulary is upstream-maintained and grows; a local enum
-- would only add tech debt). attributes such as instruments or vocal
-- parts go into details (e.g. 'guitar', 'lead vocals'). display_name
-- is the credit name as MusicBrainz recorded it for THIS credit (may
-- differ from artist.name, e.g. localized or shortened forms).
CREATE TABLE credit (
    subject_type text NOT NULL,   -- recording | release | release_group | work
    subject_mbid uuid NOT NULL,
    artist_mbid  uuid NOT NULL REFERENCES artist,
    role         text NOT NULL,   -- raw MB relationship type, verbatim
    details      text NOT NULL DEFAULT '',  -- attributes: 'guitar', 'lead vocals', ...
    position     integer NOT NULL,
    display_name text NOT NULL,
    joinphrase   text NOT NULL DEFAULT '',
    PRIMARY KEY (subject_type, subject_mbid, artist_mbid, role, position)
);
CREATE INDEX credit_subject_idx ON credit (subject_type, subject_mbid);
CREATE INDEX credit_artist_idx ON credit (artist_mbid);

-- Alternative names from MusicBrainz: per-locale primary flags and
-- begin/end date ranges. These back the display-name feature: ordered
-- locale chains (e.g. show the English alias "Peaceful Sleep" for a
-- Japanese recording title) and renamed artists (Eskimo Callboy →
-- Electric Callboy, via the alias date ranges).
CREATE TABLE alias (
    id           bigserial PRIMARY KEY,
    entity_type  text NOT NULL,   -- artist | release | release_group | recording | work
    entity_mbid  uuid NOT NULL,
    name         text NOT NULL,
    sort_name    text,
    locale       text,            -- NULL = unlocalized alias
    type         text,            -- artist name | search hint | ...
    is_primary   boolean NOT NULL DEFAULT false,
    begin_date   text,
    end_date     text
);
-- NULL locale must dedupe like any other value: uniqueness is an
-- expression index over coalesce(locale, ''), since table-level
-- UNIQUE constraints cannot contain expressions.
CREATE UNIQUE INDEX alias_uniq_key ON alias (entity_type, entity_mbid, name, coalesce(locale, ''));
CREATE INDEX alias_entity_idx ON alias (entity_type, entity_mbid);

-- Physical media.
CREATE TABLE library (
    id        bigserial PRIMARY KEY,
    root_path text NOT NULL UNIQUE,
    active    boolean NOT NULL DEFAULT true
);

CREATE TABLE file (
    id             bigserial PRIMARY KEY,
    library_id     bigint NOT NULL REFERENCES library,
    path           text NOT NULL UNIQUE,   -- relative to library root
    mtime          timestamptz NOT NULL,
    size_bytes     bigint NOT NULL,
    format         text NOT NULL,          -- flac | mp3 | ...
    release_mbid   uuid REFERENCES release,
    recording_mbid uuid REFERENCES recording,
    -- Raw tag evidence from the scanner: the complete tag map with
    -- multi-valued tags as JSON arrays. Evidence, not truth: the
    -- resolver links files into the graph from MBIDs, the raw map
    -- stays for conflict reporting and debugging.
    tags           jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE track (
    release_mbid   uuid NOT NULL REFERENCES release,
    recording_mbid uuid NOT NULL REFERENCES recording,
    file_id        bigint NOT NULL UNIQUE REFERENCES file,
    medium_number  integer NOT NULL,       -- disc
    track_number   integer NOT NULL,
    PRIMARY KEY (release_mbid, medium_number, track_number)
);

-- Users. Library graph above is shared; everything below is per-user.
CREATE TABLE "user" (
    id            bigserial PRIMARY KEY,
    name          text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE session (
    token      text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES "user",
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE TABLE api_token (
    id         bigserial PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES "user",
    token_hash text NOT NULL UNIQUE,
    name       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Display-name preferences: ordered locale chain per entity type +
-- name-history policy for artists (current | at_release | pinned).
CREATE TABLE display_pref (
    user_id       bigint NOT NULL REFERENCES "user",
    entity_type   text NOT NULL,
    locale_chain  text[] NOT NULL DEFAULT '{}',
    history_policy text NOT NULL DEFAULT 'current',  -- current | at_release | pinned
    PRIMARY KEY (user_id, entity_type)
);

-- Listening events (append-only). Analytics mirror in DuckDB later.
CREATE TABLE event (
    id            bigserial PRIMARY KEY,
    user_id       bigint NOT NULL REFERENCES "user",
    recording_mbid uuid NOT NULL,
    type          text NOT NULL,   -- playback_started | progress | seek | skipped | completed | starred
    position_ms   integer,
    duration_ms   integer,
    client        text NOT NULL DEFAULT '',
    occurred_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_user_time_idx ON event (user_id, occurred_at);

-- Conflicts between tag values and MusicBrainz canonical values for
-- the same MBID-anchored entity. MusicBrainz wins in the graph; these
-- rows exist so the UI can offer the tag value as a display choice
-- (e.g. tags say "Eskimo Callboy", MB canonical is "Electric
-- Callboy"). Resolver-owned, fully rebuilt per resolve run.
CREATE TABLE conflict (
    id           bigserial PRIMARY KEY,
    file_id      bigint NOT NULL REFERENCES file,
    kind         text NOT NULL,        -- recording-title | artist-name | release-title | release-group-title
    tag_value    text NOT NULL,
    mb_value     text NOT NULL,
    UNIQUE (file_id, kind, tag_value, mb_value)
);

-- MusicBrainz HTTP response cache (1 rps upstream; rescans must be free).
CREATE TABLE mb_cache (
    url_hash   bytea PRIMARY KEY,
    url        text NOT NULL,
    body       jsonb NOT NULL,
    fetched_at timestamptz NOT NULL
);

-- Scan bookkeeping lives with the scan feature (0002+); not modeled yet.
