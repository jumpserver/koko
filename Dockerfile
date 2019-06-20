FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /go/src/github.com/jumpserver/koko
RUN apk update && apk add git
ARG https_proxy
ARG http_proxy
ENV https_proxy=$https_proxy
ENV http_proxy=$http_proxy
RUN go get -u github.com/golang/dep/cmd/dep
COPY . .
RUN dep ensure -vendor-only && cd cmd && go build koko.go

FROM alpine
RUN apk add -U tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && apk del tzdata
WORKDIR /opt/koko/
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/koko .
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/locale/ locale
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/static/ static
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/templates/ templates
RUN echo > config.yml
EXPOSE 2222
EXPOSE 5000
CMD ["./koko"]
