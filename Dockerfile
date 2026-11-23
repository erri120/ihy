# ihy app image. Pure Go build (pgx, no cgo dependencies).
#
# ffmpeg is built in the first stage: a static, audio-only binary
# (decode flac/mp3/vorbis/opus/aac/alac/pcm, encode libopus to ogg, a
# few MB) instead of the ~130 MiB distro chain or a stock static build
# with the whole video stack. Source tarballs are checksum-pinned;
# bump deliberately by updating the version and checksum together.
#
# Base images are pinned by digest (multi-arch index); bump
# deliberately, e.g. with `docker manifest inspect golang:1.27-alpine`.
FROM alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f AS ffmpeg
RUN apk add --no-cache build-base nasm pkgconf xz ca-certificates

# libopus, static, no external dependencies.
ARG OPUS_VERSION=1.5.2
RUN wget -q https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz \
    && echo "65c1d2f78b9f2fb20082c38cbe47c951ad5839345876e46941612ee87f9a7ce1  opus-${OPUS_VERSION}.tar.gz" | sha256sum -c - \
    && tar xzf opus-${OPUS_VERSION}.tar.gz \
    && cd opus-${OPUS_VERSION} \
    && ./configure --prefix=/usr/local --disable-shared --enable-static --disable-doc --disable-extra-programs \
    && make -j"$(nproc)" >/dev/null \
    && make install >/dev/null \
    && pkg-config --modversion opus

# ffmpeg, static, audio-only. The lavfi sine indev exists for the
# build-time smoke test only; the null muxer and pcm encoder are its
# decode target.
ARG FFMPEG_VERSION=9.0.2
RUN wget -q https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz \
    && echo "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e  ffmpeg-${FFMPEG_VERSION}.tar.xz" | sha256sum -c - \
    && tar xJf ffmpeg-${FFMPEG_VERSION}.tar.xz \
    && cd ffmpeg-${FFMPEG_VERSION} \
    && ./configure \
        --disable-everything --disable-autodetect \
        --disable-doc --disable-debug --disable-network --disable-iconv \
        --disable-sdl2 \
        --enable-ffmpeg --disable-ffprobe --disable-ffplay \
        --enable-indev=lavfi \
        --enable-libopus --pkg-config-flags=--static --extra-ldflags=-static \
        --enable-encoder=libopus,pcm_s16le \
        --enable-decoder=flac,mp3,vorbis,opus,aac,alac,pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le \
        --enable-demuxer=flac,mp3,ogg,wav,aiff,aac,mov \
        --enable-muxer=ogg,null \
        --enable-parser=flac,mpegaudio,opus,vorbis,aac \
        --enable-filter=aresample,anull,aformat,sine \
        --enable-protocol=file \
        --enable-pthreads \
        --enable-small \
    && make -j"$(nproc)" >/dev/null \
    && strip ffmpeg \
    && install -m 0755 ffmpeg /usr/local/bin/ffmpeg \
    && /usr/local/bin/ffmpeg -hide_banner -loglevel error \
        -f lavfi -i "sine=frequency=440:duration=1" -c:a libopus -b:a 96k /tmp/t.ogg \
    && /usr/local/bin/ffmpeg -hide_banner -loglevel error -i /tmp/t.ogg -f null -

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
COPY --from=ffmpeg /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=build /out/ihy /usr/local/bin/ihy
USER ihy
EXPOSE 7731
ENTRYPOINT ["ihy"]
