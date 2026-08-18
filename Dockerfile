# syntax=docker/dockerfile:1

ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS deps
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

FROM deps AS build-api
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM deps AS build-ingest
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest

FROM deps AS build-worker
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM deps AS build-voice-dispatcher
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/voice-dispatcher ./cmd/voice-dispatcher

FROM scratch AS api
COPY --from=build-api /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-api /out/api /thundercall
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/thundercall"]

FROM scratch AS ingest
COPY --from=build-ingest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-ingest /out/ingest /thundercall
USER 65532:65532
ENTRYPOINT ["/thundercall"]

FROM scratch AS worker
COPY --from=build-worker /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-worker /out/worker /thundercall
USER 65532:65532
ENTRYPOINT ["/thundercall"]

FROM scratch AS voice-dispatcher
COPY --from=build-voice-dispatcher /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-voice-dispatcher /out/voice-dispatcher /thundercall
USER 65532:65532
ENTRYPOINT ["/thundercall"]
