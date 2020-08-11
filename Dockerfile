FROM golang:1.12-alpine as stage-build
LABEL stage=stage-build
WORKDIR /opt/koko
ARG GOPROXY=https://goproxy.io
ARG KUBECTLDOWNLOADURL=https://download.jumpserver.org/public/kubectl.tar.gz
ARG VERSION
ENV GOPROXY=$GOPROXY
ENV VERSION=$VERSION
ENV GO111MODULE=on
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0

COPY . .
RUN wget "$KUBECTLDOWNLOADURL" -O kubectl.tar.gz && tar -xzf kubectl.tar.gz \
    && chmod +x kubectl && mv kubectl rawkubectl
RUN cd utils && sh -ixeu build.sh

FROM debian:stretch-slim
RUN sed -i  's/deb.debian.org/mirrors.163.com/g' /etc/apt/sources.list \
    && sed -i  's/security.debian.org/mirrors.163.com/g' /etc/apt/sources.list
RUN apt-get update -y \
    && apt-get install -y locales \
    && localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 \
    && apt-get install -y --no-install-recommends gnupg dirmngr openssh-client procps curl \
    && rm -rf /var/lib/apt/lists/*
ENV LANG en_US.utf8

ENV MYSQL_MAJOR 8.0
RUN echo "deb http://mirrors.tuna.tsinghua.edu.cn/mysql/apt/debian stretch mysql-${MYSQL_MAJOR}" > /etc/apt/sources.list.d/mysql.list
RUN apt-get update && apt-get install -y --allow-unauthenticated --no-install-recommends mysql-community-client \
    && apt-get install -y --no-install-recommends gdb ca-certificates jq iproute2 less bash-completion unzip sysstat acl net-tools iputils-ping telnet dnsutils wget vim git \
    && rm -rf /var/lib/apt/lists/*

ENV TZ Asia/Shanghai
WORKDIR /opt/koko/
COPY --from=stage-build /opt/koko/release/koko /opt/koko
COPY --from=stage-build /opt/koko/release/koko/kubectl /usr/local/bin/kubectl
COPY --from=stage-build /opt/koko/rawkubectl /usr/local/bin/rawkubectl
COPY --from=stage-build /usr/local/go/src/runtime/sys_linux_amd64.s /usr/local/go/src/runtime/sys_linux_amd64.s
COPY --from=stage-build /opt/koko/tools/coredump.sh .
COPY --from=stage-build /opt/koko/entrypoint.sh .
COPY --from=stage-build /opt/koko/init-kubectl.sh .

RUN chmod 755 entrypoint.sh && chmod 755 init-kubectl.sh

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
