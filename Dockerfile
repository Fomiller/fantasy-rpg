# Not generated. repo.nix lists this file as unmanaged, since golden-service
# builds one binary per image. Every service ships in one image, so one Kargo
# promotion moves them all and their migrations always match.
#
# The entrypoint is the instance server, which the migrate Job also runs.
# The world server, instance manager, inventory, bot and bot director pick their
# binary with `command`.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/ ./src/cmd/instance-server ./src/cmd/world-server ./src/cmd/instance-manager ./src/cmd/inventory ./src/cmd/bot ./src/cmd/bot-director

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/instance-server /out/world-server /out/instance-manager /out/inventory /out/bot /out/bot-director /
EXPOSE 8080
ENTRYPOINT ["/instance-server"]
