# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Download modules in their own layer so code changes don't refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
# 8080 for browsers, 8081 for machines' link daemons (gRPC).
EXPOSE 8080 8081
ENTRYPOINT ["/server"]
