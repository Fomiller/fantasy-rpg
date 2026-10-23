# The managed root Dockerfile builds only world-server. This one builds any
# binary under src/cmd until golden-service can emit one image per binary.
FROM golang:1.27 AS build
ARG BINARY
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./src/cmd/${BINARY}

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
