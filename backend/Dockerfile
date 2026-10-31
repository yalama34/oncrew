FROM golang:1.26.1-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /orchestrator ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /orchestrator /app/orchestrator
USER 65532:65532
EXPOSE 8080
CMD ["/app/orchestrator"]
