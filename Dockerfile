FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /opt/koko
ARG GOPROXY=https://goproxy.io
ARG VERSION
ENV GOPROXY=$GOPROXY
ENV VERSION=$VERSION
ENV GO111MODULE=on
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0

COPY . .
RUN cd utils && sh -ixeu build.sh

FROM alpine:3.12
ENV LANG=en_US.utf8

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk update \
    && apk add --no-cache curl gdb procps ca-certificates busybox-extras mysql-client openssh-client \
    && apk add -U tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone \
    && apk del tzdata \
    && rm -rf /var/cache/apk/*

WORKDIR /opt/koko/
COPY --from=stage-build /opt/koko/release/koko /opt/koko
COPY --from=stage-build /usr/local/go/src/runtime/sys_linux_amd64.s /usr/local/go/src/runtime/sys_linux_amd64.s
COPY --from=stage-build /opt/koko/tools/coredump.sh .
COPY --from=stage-build /opt/koko/entrypoint.sh .

RUN chmod 755 entrypoint.sh

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
