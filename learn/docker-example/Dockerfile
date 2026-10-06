# syntax=docker/dockerfile:1

# A Dockerfile is a recipe for an image. Each instruction adds a layer, and
# Docker reuses a layer from last time if neither the instruction nor the
# files it uses have changed. Most of what follows is about that.
#
# This one has two stages. The first has the Go compiler and builds the
# program. The second is what actually ships, and has nothing in it but the
# program.

# ---------- stage 1: build

# "AS build" names the stage so the second one can copy out of it.
FROM golang:1.26-alpine AS build

# Every later instruction runs in this folder; it is created if missing.
WORKDIR /src

# Only the two files that list dependencies, and then download them. Layers
# are reused from the top down until one changes, so as long as go.mod and
# go.sum are untouched, editing main.go does not download everything again.
COPY go.mod go.sum ./
RUN go mod download

# Now the rest of the source. What "." holds is decided by .dockerignore.
COPY . .

# CGO_ENABLED=0 makes a binary that needs no C library, so it can run in an
# image that has none. -trimpath leaves this machine's paths out of it, and
# -ldflags="-s -w" drops the debugging tables to make it smaller.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hits .

# ---------- stage 2: run

# Distroless "static" is a few megabytes: certificates, time zones and a user
# list. There is no shell, no package manager and no compiler, so there is
# little in it to attack. ":nonroot" makes the program run as an ordinary
# user (uid 65532) instead of root.
FROM gcr.io/distroless/static-debian12:nonroot

# The one thing taken from the first stage. Everything else in it, the
# compiler and the source included, is left behind.
COPY --from=build /out/hits /hits

# Documentation: which port the program listens on. It publishes nothing;
# that is done with -p, or "ports" in Compose.
EXPOSE 8080

# How Docker finds out whether the program is working, as opposed to merely
# running. `docker ps` shows the result as (healthy) or (unhealthy).
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/hits", "-healthcheck"]

# What runs when a container starts. Written as a list ("exec form"), the
# program itself is process 1 and receives signals such as SIGTERM directly.
# Written as a plain string, Docker would run it through `/bin/sh -c`. This
# image has no shell, so it would not start at all; in an image that has
# one, the shell is process 1 and may not pass the signal on, and then
# `docker stop` waits ten seconds and kills the container.
ENTRYPOINT ["/hits"]
