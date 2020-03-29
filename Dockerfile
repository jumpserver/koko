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

RUN set -ex; \
# gpg: key 5072E1F5: public key "MySQL Release Engineering <mysql-build@oss.oracle.com>" imported
	key='A4A9406876FCBD3C456770C88C718D3B5072E1F5'; \
	export GNUPGHOME="$(mktemp -d)"; \
	( gpg --batch --keyserver p80.pool.sks-keyservers.net  --recv-keys "$key" \
      || gpg --batch --keyserver hkps.pool.sks-keyservers.net --recv-keys "$key" \
      || gpg --batch --keyserver keyserver.ubuntu.com --recv-keys "$key" \
      || gpg --batch --keyserver pgp.mit.edu --recv-keys "$key" \
      || gpg --batch --keyserver keyserver.pgp.com --recv-keys "$key" ); \
	gpg --batch --export "$key" > /etc/apt/trusted.gpg.d/mysql.gpg; \
	gpgconf --kill all; \
	rm -rf "$GNUPGHOME"; \
	apt-key list > /dev/null

ENV MYSQL_MAJOR 8.0
ENV MYSQL_VERSION 8.0.19-1debian9
RUN echo "deb http://repo.mysql.com/apt/debian/ stretch mysql-${MYSQL_MAJOR}" > /etc/apt/sources.list.d/mysql.list
RUN apt-get update && apt-get install -y ca-certificates mysql-community-client="${MYSQL_VERSION}" && rm -rf /var/lib/apt/lists/*

ENV TZ Asia/Shanghai
WORKDIR /opt/koko/
COPY --from=stage-build /opt/koko/cmd/koko .
COPY --from=stage-build /opt/koko/cmd/locale/ locale
COPY --from=stage-build /opt/koko/cmd/static/ static
COPY --from=stage-build /opt/koko/cmd/templates/ templates
COPY --from=stage-build /opt/koko/cmd/config_example.yml .
COPY --from=stage-build /opt/koko/entrypoint.sh .

RUN chmod 755 entrypoint.sh

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
