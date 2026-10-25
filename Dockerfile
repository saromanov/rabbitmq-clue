# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/internal/web/dist /src/internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/rabbitmq-clue ./cmd/rabbitmq-clue

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/rabbitmq-clue /rabbitmq-clue
EXPOSE 8080
ENTRYPOINT ["/rabbitmq-clue"]