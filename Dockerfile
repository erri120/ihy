# ihy app image. Pure Go build (pgx, no cgo dependencies yet);
# ffmpeg lands here with the transcoder. Base images are pinned by
# digest (multi-arch index); bump deliberately, e.g. with
# `docker manifest inspect golang:1.27-alpine`.
FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# Build cache is a host cache mount, not an image layer, so rebuilds
# skip unchanged packages.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ihy ./cmd/ihy

FROM alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f
RUN adduser -D -H -u 1000 ihy
COPY --from=build /out/ihy /usr/local/bin/ihy
USER ihy
EXPOSE 7731
ENTRYPOINT ["ihy"]
