FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/habitos ./cmd/web

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/habitos /habitos
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/habitos"]
