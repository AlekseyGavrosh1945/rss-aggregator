FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /rss-aggregator ./cmd/aggregator

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 app
USER app
COPY --from=build /rss-aggregator /usr/local/bin/rss-aggregator
EXPOSE 8080
ENTRYPOINT ["rss-aggregator"]
