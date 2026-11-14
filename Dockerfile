FROM golang:1.26.5-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 go build -trimpath -o /out/seed ./cmd/seed \
    && CGO_ENABLED=0 go build -trimpath -o /out/hr-scope ./cmd/hr-scope \
    && CGO_ENABLED=0 go build -trimpath -o /out/demo-control ./cmd/demo-control \
    && CGO_ENABLED=0 go build -trimpath -o /out/knowledge-import ./cmd/knowledge-import \
    && CGO_ENABLED=0 go build -trimpath -o /out/playbook-import ./cmd/playbook-import

FROM node:24.15.0-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/index.html frontend/tsconfig.json frontend/vite.config.ts frontend/vitest.config.ts ./
COPY frontend/src ./src
COPY frontend/public ./public
COPY contracts/examples/scenario.json /src/contracts/examples/scenario.json
RUN npm run build

FROM nginx:stable-alpine AS frontend
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=frontend-build /src/frontend/dist /usr/share/nginx/html

FROM alpine:3.23 AS backend-runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/server /out/migrate /out/seed /out/demo-control /out/knowledge-import /out/hr-scope /app/
COPY demo/knowledge /app/knowledge
USER 10001:10001
EXPOSE 8080
CMD ["/app/server"]

FROM backend-runtime AS playbook-import
USER root
RUN apk add --no-cache git && git config --system --add safe.directory /playbook
COPY --from=build /out/playbook-import /app/
USER 10001:10001
ENTRYPOINT ["/app/playbook-import"]

FROM backend-runtime AS runtime
