FROM golang:latest as olric

RUN mkdir -p src/github.com/buraksezer
WORKDIR /go/src/github.com/buraksezer

# RUN git clone https://github.com/buraksezer/olric
# WORKDIR /go/src/github.com/buraksezer/olric/cmd/olricd
COPY ./olric.yml olricd-local.yaml
# COPY ./olric.yml /etc/olricd.yaml
RUN go install github.com/buraksezer/olric/cmd/olricd@v0.5.7

EXPOSE 3320 3322

CMD ["olricd", "-c", "olricd-local.yaml"]
