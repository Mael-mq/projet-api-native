# --- Étape de build ---
FROM golang:1.22-alpine AS build
WORKDIR /src

# git est requis par le module proxy pour certaines dépendances.
RUN apk add --no-cache git
# Interdit toute montée de toolchain : les versions sont figées pour docker v27.3.1.
ENV GOTOOLCHAIN=local
# -mod=mod : go build résout et écrit go.sum à la volée (uniquement les imports réels,
# sans les dépendances de test des modules tierces qui cassaient `go mod tidy`).
ENV GOFLAGS=-mod=mod

COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api

# --- Image finale ---
FROM alpine:3.20
RUN adduser -D -u 10001 appuser \
    && mkdir -p /data && chown appuser /data
COPY --from=build /out/api /usr/local/bin/api
USER appuser
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["api"]
