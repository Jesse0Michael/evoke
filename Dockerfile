ARG BASE_IMAGE=gcr.io/distroless/static
FROM golang:1.26-alpine AS build

# Git is required for fetching some dependencies.
RUN apk update && apk add --no-cache git

WORKDIR /go/src

# Fetch dependencies first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# GO_COMMAND selects which binary to build (defaults to the registry API).
ARG GO_COMMAND
ENV GO_COMMAND=${GO_COMMAND:-./cmd/app}
ENV CGO_ENABLED=0
RUN go build -o ./app $GO_COMMAND

FROM $BASE_IMAGE AS app
COPY --from=build /go/src/app /app
ENTRYPOINT ["/app"]
