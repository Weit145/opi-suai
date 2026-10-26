FROM golang:1.25.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/app && CGO_ENABLED=0 go build -trimpath -o /out/provider ./cmd/provider
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/app /out/provider /usr/local/bin/
USER app
CMD ["app"]
