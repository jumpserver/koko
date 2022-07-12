FROM node:10 as ui-build
ARG NPM_REGISTRY="https://registry.npmmirror.com"
ENV NPM_REGISTY=$NPM_REGISTRY

WORKDIR /opt/koko
RUN npm config set registry ${NPM_REGISTRY}
RUN yarn config set registry ${NPM_REGISTRY}

COPY ui  ui/
RUN ls . && cd ui/ && npm install -i && yarn build && ls -al .

FROM golang:1.17-alpine as stage-build
LABEL stage=stage-build
WORKDIR /opt/koko

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk update \
    && apk add git

ARG GOPROXY=https://goproxy.io
ARG TARGETARCH
ENV TARGETARCH=$TARGETARCH
ENV GO111MODULE=on
ENV GOOS=linux

RUN wget https://download.jumpserver.org/public/kubectl-linux-${TARGETARCH}.tar.gz -O kubectl.tar.gz \
    && tar -xf kubectl.tar.gz \
    && chmod +x kubectl \
    && mv kubectl rawkubectl \
    && wget https://download.jumpserver.org/public/helm-v3.9.0-linux-${TARGETARCH}.tar.gz -O helm.tar.gz \
    && tar -xf helm.tar.gz \
    && chmod +x linux-${TARGETARCH}/helm \
    && mv linux-${TARGETARCH}/helm rawhelm \
    && wget http://download.jumpserver.org/public/kubectl_aliases.tar.gz -O kubectl_aliases.tar.gz \
    && tar -xf kubectl_aliases.tar.gz

COPY go.mod go.sum ./
RUN go mod download -x
COPY . .
ARG VERSION=Unknown
ENV VERSION=$VERSION
RUN cd utils && sh -ixeu build.sh

FROM debian:bullseye-slim
ENV LANG en_US.utf8
RUN sed -i 's/deb.debian.org/mirrors.aliyun.com/g' /etc/apt/sources.list \
    && sed -i 's/security.debian.org/mirrors.aliyun.com/g' /etc/apt/sources.list \
    && apt update \
    && apt-get install -y locales \
    && localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 \
    && apt-get install -y --no-install-recommends openssh-client procps curl gdb ca-certificates jq iproute2 less bash-completion unzip sysstat acl net-tools iputils-ping telnet dnsutils wget vim git freetds-bin mariadb-client redis-tools postgresql-client gnupg\
    && wget -qO - https://www.mongodb.org/static/pgp/server-5.0.asc | apt-key add - \
    && echo "deb [ arch=amd64,arm64 ] https://repo.mongodb.org/apt/ubuntu bionic/mongodb-org/5.0 multiverse" | tee /etc/apt/sources.list.d/mongodb-org-5.0.list \
    && apt update \
    && apt-get install -y --no-install-recommends mongodb-mongosh \
    && rm -rf /var/lib/apt/lists/*

ENV TZ Asia/Shanghai
WORKDIR /opt/koko/
COPY --from=stage-build /opt/koko/release/koko /opt/koko
COPY --from=stage-build /opt/koko/release/koko/kubectl /usr/local/bin/kubectl
COPY --from=stage-build /opt/koko/release/koko/helm /usr/local/bin/helm
COPY --from=stage-build /opt/koko/rawkubectl /usr/local/bin/rawkubectl
COPY --from=stage-build /opt/koko/rawhelm /usr/local/bin/rawhelm
COPY --from=stage-build /opt/koko/utils/coredump.sh .
COPY --from=stage-build /opt/koko/entrypoint.sh .
COPY --from=stage-build /opt/koko/utils/init-kubectl.sh .
COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=ui-build /opt/koko/ui/dist ui/dist

RUN chmod 755 entrypoint.sh && chmod 755 init-kubectl.sh

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
