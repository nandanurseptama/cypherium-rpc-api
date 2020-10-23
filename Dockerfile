# Build Cypher in a stock Go builder container
FROM golang:1.10-alpine as builder

RUN apk add --no-cache make gcc musl-dev linux-headers

ADD . /cypherBFT
RUN cd /cypherBFT && make cypher

# Pull Cypher into a second stage deploy alpine container
FROM alpine:latest

RUN apk add --no-cache ca-certificates
COPY --from=builder /cypherBFT/build/bin/cypher /usr/local/bin/

EXPOSE 7100 9090 6000 8000/udp
ENTRYPOINT ["cypher"]
