FROM golang:1.25.8

WORKDIR /src

RUN apt-get update && apt-get install -y findutils && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY storageapi ./storageapi

COPY ./station-types/build-plugins.sh ./build-plugins.sh

RUN chmod +x /src/build-plugins.sh

CMD ["/src/build-plugins.sh"]
