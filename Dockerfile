# downloaded from: 
FROM registry.efarda.ir/golang:1.19 as builder

WORKDIR /opt/app

COPY go.* ./
RUN go mod download

COPY . ./

RUN go build -v -o application

FROM registry.efarda.ir/debian:buster-slim
RUN set -x && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /opt/app/application /opt/app/

CMD ["/opt/app/application", "-config", "/etc/network-status-checker/ocnfig.yml"]