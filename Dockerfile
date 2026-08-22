# Build a static binary, then ship it on distroless.
#
# The image carries no shell and no package manager. This service holds no
# keys and moves no funds, but it does publish figures under Wayfare's name,
# and the smallest possible surface is the cheapest way to keep it that way.

FROM golang:1.22-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off for a genuinely static binary; distroless/static has no libc.
# The snapshots under testdata/ are test fixtures and are not needed at
# runtime — .dockerignore keeps them out of the build context.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/wayfared ./cmd/wayfared

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/wayfared /wayfared

# The run store lives on a mounted volume. Declared so a container started
# without one still has somewhere to write rather than failing at open.
ENV WAYFARE_DATA_DIR=/data
VOLUME ["/data"]

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/wayfared"]
