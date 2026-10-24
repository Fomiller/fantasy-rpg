# ---------------------------------------------------------------------------
# GENERATED FILE — managed by flake-hub (golden-service).
# Do not edit manually: `nix run .#generate` overwrites it.
# To change it, edit repo.nix, or the template in the pack that owns it.
# ---------------------------------------------------------------------------

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/instance-server ./src/cmd/instance-server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/instance-server /instance-server
EXPOSE 8080
ENTRYPOINT ["/instance-server"]
