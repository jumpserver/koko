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
WORKDIR /opt/koko/
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/koko .
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/locale/ locale
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/static/ static
COPY --from=stage-build /go/src/github.com/jumpserver/koko/cmd/templates/ templates
COPY cmd/config_example.yml .
COPY entrypoint.sh .
RUN chmod 755 ./entrypoint.sh \
  && apk add -U tzdata \
  && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
  && echo "Asia/Shanghai" > /etc/timezone \
  && apk del tzdata \
  && rm -rf /var/cache/apk/*

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
