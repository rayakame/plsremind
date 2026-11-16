FROM node:24 AS web
WORKDIR /src
COPY package.json package-lock.json ./
COPY sdk/ts/package.json sdk/ts/
COPY web/package.json web/
RUN npm ci --ignore-scripts
COPY sdk/ts sdk/ts
COPY web web
RUN npm run build --prefix web && rm -rf web/dist/.vite

FROM golang:1.27 AS build
WORKDIR /src
COPY hub/go.mod hub/go.sum ./
RUN go mod download
COPY hub .
COPY --from=web /src/web/dist/ internal/server/webdist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/rayakame/plsremind/hub/internal/server.Version=${VERSION}" \
    -o /out/plsremind ./cmd/plsremind

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/plsremind /plsremind
EXPOSE 8080
ENTRYPOINT ["/plsremind"]
