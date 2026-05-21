FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/prophylaxis-scheduler .

FROM alpine:3.20

WORKDIR /app
RUN mkdir -p /app/data

COPY --from=builder /out/prophylaxis-scheduler /app/prophylaxis-scheduler

ENV ADDR=:8080
ENV DATA_FILE=/app/data/maintenances.yaml
ENV COMMAND_TIMEOUT=2m

EXPOSE 8080

CMD ["/app/prophylaxis-scheduler"]
