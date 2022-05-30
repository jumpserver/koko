FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /opt/koko
ARG GOPROXY
ENV GOPROXY=$GOPROXY
ENV GO111MODULE=on
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
  && apk update \
  && apk add git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN cd cmd && go build -ldflags "-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`' -X 'main.Githash=`git rev-parse HEAD`' -X 'main.Goversion=`go version`'" -x -o koko koko.go

FROM debian:stretch-slim
RUN sed -i  's/deb.debian.org/mirrors.163.com/g' /etc/apt/sources.list \
    && sed -i  's/security.debian.org/mirrors.163.com/g' /etc/apt/sources.list
RUN apt-get update -y \
    && apt-get install -y --no-install-recommends gnupg dirmngr openssh-client procps curl \
    && rm -rf /var/lib/apt/lists/*

ENV MYSQL_MAJOR 8.0
RUN echo "deb http://mirrors.tuna.tsinghua.edu.cn/mysql/apt/debian stretch mysql-${MYSQL_MAJOR}" > /etc/apt/sources.list.d/mysql.list
RUN apt-get update && apt-get install -y --allow-unauthenticated gdb ca-certificates mysql-community-client && rm -rf /var/lib/apt/lists/*

ENV TZ Asia/Shanghai
WORKDIR /opt/koko/
COPY --from=stage-build /usr/local/go/src/runtime/sys_linux_amd64.s /usr/local/go/src/runtime/sys_linux_amd64.s
COPY --from=stage-build /opt/koko/tools/coredump.sh .
COPY --from=stage-build /opt/koko/cmd/koko .
COPY --from=stage-build /opt/koko/cmd/locale/ locale
COPY --from=stage-build /opt/koko/cmd/static/ static
COPY --from=stage-build /opt/koko/cmd/templates/ templates
COPY --from=stage-build /opt/koko/cmd/config_example.yml .
COPY --from=stage-build /opt/koko/entrypoint.sh .

RUN chmod 755 entrypoint.sh

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
