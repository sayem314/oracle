# Build the single static binary (pure-Go sqlite, embedded migrations).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
RUN cd apps/api && go mod download
COPY apps/api ./apps/api
RUN cd apps/api && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/oracle ./cmd/oracle

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 oracle
COPY --from=build /out/oracle /usr/local/bin/oracle
# The data volume is seeded from this directory's ownership on first use.
RUN mkdir -p /data && chown oracle:oracle /data
USER oracle
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
	CMD wget -qO- http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["oracle"]
