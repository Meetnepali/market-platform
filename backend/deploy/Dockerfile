# Multi-stage build shared by all three services.
# Build:  docker build -f deploy/Dockerfile --build-arg SERVICE=api -t market-api .
ARG SERVICE=api

FROM golang:1.23-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /bin/service ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/service /service
USER nonroot
ENTRYPOINT ["/service"]
