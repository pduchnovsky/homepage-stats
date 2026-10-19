# syntax=docker/dockerfile:1
ARG GO_VERSION
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download all
COPY main.go LICENSE THIRD_PARTY_NOTICES.md ./
COPY scripts/licenses.sh ./scripts/licenses.sh
RUN sh scripts/licenses.sh /licenses
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -ldflags="-s -w" -o /stats .

FROM scratch
COPY --from=build /stats /stats
COPY --from=build /licenses /usr/share/licenses
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/homepage-stats/
USER 65532:65532
EXPOSE 61208
HEALTHCHECK CMD ["/stats", "healthcheck"]
ENTRYPOINT ["/stats"]
