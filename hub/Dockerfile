FROM node:24@sha256:49f7adaf9f1574bc45c20ea317139b39a59d7e758d0e93745e112fd85cb42623 AS web
WORKDIR /src
COPY package.json package-lock.json ./
COPY sdk/ts/package.json sdk/ts/
COPY web/package.json web/
RUN npm ci --ignore-scripts
COPY sdk/ts sdk/ts
COPY web web
RUN npm run build --prefix web && rm -rf web/dist/.vite

FROM golang:1.27@sha256:5f1b98fa02fe396ebce2a0227a0e08309e11792915b9dd2e1f71053d7e7e6666 AS build
WORKDIR /src
COPY hub/go.mod hub/go.sum ./
RUN go mod download
COPY hub .
COPY --from=web /src/web/dist/ internal/server/webdist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/rayakame/plsremind/hub/internal/server.Version=${VERSION}" \
    -o /out/plsremind ./cmd/plsremind

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/plsremind /plsremind
EXPOSE 8080
ENTRYPOINT ["/plsremind"]
