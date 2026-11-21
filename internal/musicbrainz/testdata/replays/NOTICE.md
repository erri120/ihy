# Replays: MusicBrainz data recorded for offline tests

Source: the MusicBrainz WS/2 API (musicbrainz.org), recorded from the
fixture library in `examples/` (renamed entity MBIDs, releases,
recordings, works, artists). One file per request, named by the SHA-1
of the request URL; `manifest.json` maps URLs to files.

## License

MusicBrainz data is licensed under CC0 1.0 (public domain dedication).
The following parts of each item are licensed under
Attribution-NonCommercial-ShareAlike 3.0 (CC BY-NC-SA 3.0): annotation,
genres, ratings, tags. These fields are REMOVED from every replay by
the recording transport, and the client never requests them.

Attribution: data (c) MusicBrainz, https://musicbrainz.org

- Data dumps and licensing:
  https://musicbrainz.org/doc/Development/JSON_Data_Dumps
- CC0 1.0: https://creativecommons.org/publicdomain/zero/1.0/
- CC BY-NC-SA 3.0: https://creativecommons.org/licenses/by-nc-sa/3.0/

Note: the removed fields also stay out of the database at runtime by
design: the client's `inc=` set never requests tags, genres, ratings
or annotations, so no CC BY-NC-SA content enters ihy at all.

## Regenerating

`make record-replays` (requires network access to MusicBrainz, ~1
request per second). Replay files are deterministic per request URL;
rerunning updates payloads to the latest live data.
