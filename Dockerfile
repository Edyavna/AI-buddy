FROM golang:1.26.5-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 go build -trimpath -o /out/seed ./cmd/seed \
    && CGO_ENABLED=0 go build -trimpath -o /out/demo-control ./cmd/demo-control \
    && CGO_ENABLED=0 go build -trimpath -o /out/knowledge-import ./cmd/knowledge-import

FROM alpine:3.23 AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/server /out/migrate /out/seed /out/demo-control /out/knowledge-import /app/
COPY demo/knowledge /app/knowledge
USER 10001:10001
EXPOSE 8080
CMD ["/app/server"]
