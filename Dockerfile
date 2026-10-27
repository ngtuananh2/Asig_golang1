FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/taskapi ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/taskapi /taskapi
EXPOSE 8080
ENTRYPOINT ["/taskapi"]
