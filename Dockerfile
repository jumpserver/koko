FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /opt/coco
ARG GOPROXY
ENV GOPROXY=$GOPROXY
ENV GO111MODULE=on
COPY . .
RUN go mod download
RUN cd cmd && go build koko.go

FROM alpine
WORKDIR /opt/coco/
COPY --from=stage-build /opt/coco/cmd/koko .
COPY --from=stage-build /opt/coco/cmd/locale/ locale
COPY --from=stage-build /opt/coco/cmd/static/ static
COPY --from=stage-build /opt/coco/cmd/templates/ templates
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
