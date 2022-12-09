FROM node:16.5 as ui-build
ARG TARGETARCH
ARG NPM_REGISTRY="https://registry.npmmirror.com"
ENV NPM_REGISTY=$NPM_REGISTRY

RUN set -ex \
    && npm config set registry ${NPM_REGISTRY} \
    && yarn config set registry ${NPM_REGISTRY}

WORKDIR /opt/koko/ui
ADD ui/package.json ui/yarn.lock .
RUN --mount=type=cache,target=/usr/local/share/.cache/yarn,sharing=locked,id=koko \
    yarn install

ADD ui .
RUN --mount=type=cache,target=/usr/local/share/.cache/yarn,sharing=locked,id=koko \
    yarn build

FROM golang:1.18-bullseye as stage-build
LABEL stage=stage-build
ARG TARGETARCH

WORKDIR /opt/koko
ARG DOWNLOAD_URL=https://download.jumpserver.org

RUN set -ex \
    && echo "no" | dpkg-reconfigure dash \
    && wget ${DOWNLOAD_URL}/public/kubectl-linux-${TARGETARCH}.tar.gz -O kubectl.tar.gz \
    && tar -xf kubectl.tar.gz \
    && chmod +x kubectl \
    && mv kubectl rawkubectl \
    && wget ${DOWNLOAD_URL}/public/helm-v3.9.0-linux-${TARGETARCH}.tar.gz -O helm.tar.gz \
    && tar -xf helm.tar.gz \
    && chmod +x linux-${TARGETARCH}/helm \
    && mv linux-${TARGETARCH}/helm rawhelm \
    && wget ${DOWNLOAD_URL}/public/kubectl_aliases.tar.gz -O kubectl_aliases.tar.gz \
    && tar -xf kubectl_aliases.tar.gz \
    && wget ${DOWNLOAD_URL}/files/clickhouse/22.20.2.11/clickhouse-client-linux-${TARGETARCH}.tar.gz \
    && tar xf clickhouse-client-linux-${TARGETARCH}.tar.gz \
    && chmod +x clickhouse-client

ADD go.mod go.sum .

ARG GOPROXY=https://goproxy.io
ENV CGO_ENABLED=0
ENV GO111MODULE=on
ENV GOOS=linux

RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download -x

COPY . .
ARG VERSION
ENV VERSION=$VERSION

RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    set +x \
    && export cipherKey="$(head -c 100 /dev/urandom | base64 | head -c 32)"  \
    && export KEYFLAG="-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$cipherKey'" \
    && export GOFlAGS="-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`'" \
    && export GOFlAGS="$GOFlAGS -X 'main.Githash=`git rev-parse HEAD`'" \
    && export GOFlAGS="${GOFlAGS} -X 'main.Goversion=`go version`'" \
    && export GOFlAGS="$GOFlAGS -X 'main.Version=$VERSION'" \
    && go build -ldflags "$GOFlAGS $KEYFLAG" -o koko ./cmd/koko \
    && go build -ldflags "$KEYFLAG" -o kubectl ./cmd/kubectl \
    && go build -ldflags "$KEYFLAG" -o helm ./cmd/helm \
    && set -x && ls -al .

RUN mkdir /opt/koko/bin \
    && mv /opt/koko/clickhouse-client /opt/koko/bin \
    && mv /opt/koko/rawkubectl /opt/koko/bin \
    && mv /opt/koko/rawhelm /opt/koko/bin

RUN mkdir /opt/koko/release \
    && mv /opt/koko/static /opt/koko/release \
    && mv /opt/koko/templates /opt/koko/release \
    && mv /opt/koko/locale /opt/koko/release \
    && mv /opt/koko/config_example.yml /opt/koko/release \
    && mv /opt/koko/entrypoint.sh /opt/koko/release \
    && mv /opt/koko/utils/init-kubectl.sh /opt/koko/release \
    && chmod 755 /opt/koko/release/entrypoint.sh /opt/koko/release/init-kubectl.sh

FROM debian:bullseye-slim
ARG TARGETARCH

ARG DEPENDENCIES="                    \
        bash-completion               \
        ca-certificates               \
        curl                          \
        dnsutils                      \
        freetds-bin                   \
        gdb                           \
        git                           \
        gnupg                         \
        iproute2                      \
        iputils-ping                  \
        jq                            \
        less                          \
        locales                       \
        mariadb-client                \
        net-tools                     \
        openssh-client                \
        postgresql-client             \
        procps                        \
        redis-tools                   \
        sysstat                       \
        telnet                        \
        unzip                         \
        vim                           \
        wget"

ARG APT_MIRROR=http://mirrors.ustc.edu.cn

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked,id=koko \
    sed -i "s@http://.*.debian.org@${APT_MIRROR}@g" /etc/apt/sources.list \
    && rm -f /etc/apt/apt.conf.d/docker-clean \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && apt-get update \
    && apt-get install -y --no-install-recommends ${DEPENDENCIES} \
    && wget -qO - https://www.mongodb.org/static/pgp/server-5.0.asc | apt-key add - \
    && echo "deb [ arch=amd64,arm64 ] https://repo.mongodb.org/apt/ubuntu bionic/mongodb-org/5.0 multiverse" | tee /etc/apt/sources.list.d/mongodb-org-5.0.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends mongodb-mongosh \
    && echo "no" | dpkg-reconfigure dash \
    && echo "zh_CN.UTF-8" | dpkg-reconfigure locales \
    && sed -i "s@# export @export @g" ~/.bashrc \
    && sed -i "s@# alias @alias @g" ~/.bashrc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/koko/

COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=stage-build /opt/koko/bin /usr/local/bin
COPY --from=stage-build /opt/koko/release .
COPY --from=stage-build /opt/koko/koko .
COPY --from=stage-build /opt/koko/kubectl .
COPY --from=stage-build /opt/koko/helm .
COPY --from=ui-build /opt/koko/ui/dist ui/dist

ENV LANG=zh_CN.UTF-8

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
