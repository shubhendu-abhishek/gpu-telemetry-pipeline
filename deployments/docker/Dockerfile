# Build from repository root: docker build -f deployments/docker/Dockerfile --build-arg APP=api .
FROM golang:1.27.3-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod ./
RUN go mod download
COPY . .
ARG APP=api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${APP}

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /src/data /app/data
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/app"]
