# ihy

A free, open-source, self-hosted music streaming server. ihy resolves
your music library against [MusicBrainz](https://musicbrainz.org)
server-side: artists, albums, tracks, works and full credit
relationships become a proper graph instead of whatever the ID3 tags
happen to contain. Localized display names, first-party web UI and
JSON API, all under your control.

## The name

Ihy is the ancient Egyptian god of music. Depicted as a child holding
a sistrum, the rattle priests shook for the goddess Hathor, he is
worshipped at Dendera as the son (or the child form) of Hathor
herself: music as something played for the goddess, not merely owned.
A fitting namesake for a server that treats music as relationships and
context, not as a pile of files.

## Setup

The deployment path is Docker Compose. Create a `.env` file next to
[docker-compose.yml](./docker-compose.yml):

```
DB_HOST=db
DB_USER=ihy
DB_PASSWORD=<your postgres password>
DB_DATABASE=ihy
IHY_LIBRARY=/mnt/nfs/music
IHY_ADMIN_PASSWORD=<choose on first boot>
```

Then:

```
docker compose up -d
```

The app image builds from the repository [Dockerfile](./Dockerfile)
and the Postgres container initializes itself. Open
`http://<host>:7731`, log in with the admin account created from
`IHY_ADMIN_PASSWORD` (first boot only), and the scan plus MusicBrainz
resolution run in the background.

Without Docker, build the binary and run it against any Postgres:

```
CGO_ENABLED=0 go build -o ihy ./cmd/ihy
DB_HOST=... DB_DATABASE=... DB_USER=... DB_PASSWORD=... \
IHY_LIBRARIES=/path/to/music \
IHY_ADMIN_PASSWORD=... ./ihy
```

### Configuration values

Required:

| Variable             | Meaning                                                        |
| -------------------- | -------------------------------------------------------------- |
| `DB_HOST`            | Postgres host                                                  |
| `DB_DATABASE`        | Postgres database                                              |
| `DB_USER`            | Postgres user                                                  |
| `DB_PASSWORD`        | Postgres password                                              |
| `IHY_LIBRARIES`      | Library roots (comma-separated); mounted read-only, never written |
| `IHY_ADMIN_PASSWORD` | Password for the auto-created `admin` account (first boot only) |

Optional:

| Variable             | Default                  | Meaning                                |
| -------------------- | ------------------------ | -------------------------------------- |
| `DB_PORT`            | `5432`                   | Postgres port                          |
| `DB_SSLMODE`         | `disable`                | Postgres SSL mode                      |
| `IHY_ADDR`           | `:7731`                  | HTTP listen address                    |
| `IHY_ARTWORK_DIR`    | `./data/artwork`         | Derived thumbnails (small, never full-size) |
| `IHY_TRANSCODE_DIR`  | `./data/transcode`       | Disposable transcode cache             |
| `IHY_MB_BASE_URL`    | `https://musicbrainz.org`| MusicBrainz endpoint; set to a self-hosted mirror |
| `IHY_MB_RATE_LIMIT`  | `1`                      | Requests per second; **mirror only**. Setting it while pointing at the official server is a configuration error, the official 1 req/s rule is not negotiable |
| `IHY_MB_REPLAYS`     | unset                    | Development: serve MusicBrainz responses from a local replay corpus instead of the network |

Library roots are mounted read-only and the app never writes to them.
If the Postgres data directory lives on NFS, mount it with `hard` on
the client and export it with `sync` on the server; that is the
supported configuration for Postgres on NFS.

## License

See [LICENSE](./LICENSE) for details.
