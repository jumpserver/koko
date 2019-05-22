FROM golang:1.12-alpine as stage-build
WORKDIR /go/src/coco
RUN apk update && apk add git
RUN export https_proxy=http://192.168.1.9:1087
RUN go get -u github.com/golang/dep/cmd/dep
COPY . .
RUN cd cmd && go build coco.go

FROM alpine
WORKDIR /opt/coco/
COPY --from=stage-build /go/src/coco/cmd/ /opt/coco/
CMD ['/opt/coco/coco']
