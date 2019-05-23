FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /go/src/cocogo
RUN apk update && apk add git
ARG https_proxy
ARG http_proxy
ENV https_proxy=$https_proxy
ENV http_proxy=$http_proxy
RUN go get -u github.com/golang/dep/cmd/dep
COPY . .
RUN dep ensure -vendor-only && cd cmd && go build coco.go

FROM alpine
WORKDIR /opt/coco/
COPY --from=stage-build /go/src/cocogo/cmd/coco .
COPY --from=stage-build /go/src/cocogo/cmd/locale .
RUN  echo > config.yml
EXPOSE 2222
CMD ["./coco"]
