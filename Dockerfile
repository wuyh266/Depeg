# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS go-build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY web ./web
RUN mkdir /out && go build -mod=readonly -trimpath -buildvcs=false -o /out/ ./cmd/api ./cmd/migrate ./cmd/import

FROM python:3.11-slim-bookworm AS collector
WORKDIR /app
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
RUN pip install --no-cache-dir ccxt==4.5.39 \
    && install -d -o 10001 -g 10001 /var/lib/depeg/collector-pending
COPY --from=go-build /out/import /usr/local/bin/depeg-import
COPY --from=collector_src /src/__init__.py /src/collect_ohlcv.py /src/ohlcv_pages.py ./src/
USER 10001:10001
CMD ["python", "-m", "src.collect_ohlcv", "--help"]

FROM alpine:3.23 AS api
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=go-build /out/api /usr/local/bin/depeg-api
COPY --from=go-build /out/migrate /usr/local/bin/depeg-migrate
USER 10001:10001
EXPOSE 8080
CMD ["/usr/local/bin/depeg-api"]
