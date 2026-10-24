# Not generated. repo.nix lists this file as unmanaged, since golden-service
# builds one binary per image. Both servers ship in one image, so one Kargo
# promotion moves both and their migrations always match.
#
# The entrypoint is the instance server, which the migrate Job also runs.
# The world server's Deployment picks its binary with `command`.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/ ./src/cmd/instance-server ./src/cmd/world-server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/instance-server /out/world-server /
EXPOSE 8080
ENTRYPOINT ["/instance-server"]
