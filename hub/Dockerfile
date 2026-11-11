FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/rayakame/plsremind/hub/internal/httpapi.Version=${VERSION}" \
    -o /out/plsremind ./cmd/plsremind

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/plsremind /plsremind
EXPOSE 8080
ENTRYPOINT ["/plsremind"]
