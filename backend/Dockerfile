FROM golang:1.26.1-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

FROM build AS build-api
RUN CGO_ENABLED=0 go build -o /orchestrator ./cmd/api

FROM build AS build-preview
RUN CGO_ENABLED=0 go build -o /preview ./cmd/preview

FROM alpine:3.21 AS preview
COPY --from=build-preview /preview /preview
USER 65532:65532
EXPOSE 8081
CMD ["/preview"]

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build-api /orchestrator /app/orchestrator
USER 65532:65532
EXPOSE 8080
CMD ["/app/orchestrator"]
