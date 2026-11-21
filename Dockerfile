# ihy app image. Pure Go build (pgx, no cgo dependencies yet);
# ffmpeg lands here with the transcoder.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ihy ./cmd/ihy

FROM alpine:3.22
RUN adduser -D -H -u 1000 ihy
COPY --from=build /out/ihy /usr/local/bin/ihy
USER ihy
EXPOSE 7731
ENTRYPOINT ["ihy"]
