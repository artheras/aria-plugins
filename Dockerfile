FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY packages ./packages
COPY apps ./apps
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/commerce-worker

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 aria && mkdir /data && chown aria /data
COPY --from=build /out/server /usr/local/bin/aria-commerce
COPY --from=build /out/worker /usr/local/bin/aria-commerce-worker
USER aria
ENV ARIA_BIND_ALL=1 ARIA_DATA_DIR=/data PORT=4180
EXPOSE 4180
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/aria-commerce"]
