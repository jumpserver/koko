FROM redis:6.2-bullseye as redis

FROM node:16.20-bullseye-slim as ui-build
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

FROM golang:1.21-bullseye as stage-build
LABEL stage=stage-build
ARG TARGETARCH

WORKDIR /opt/koko
ARG HELM_VERSION=v3.12.2
ARG DOWNLOAD_URL=https://download.jumpserver.org

RUN set -ex \
    && echo "no" | dpkg-reconfigure dash

RUN set -ex \
    && mkdir -p /opt/koko/bin \
    && wget ${DOWNLOAD_URL}/public/kubectl-linux-${TARGETARCH}.tar.gz -O kubectl.tar.gz \
    && tar -xf kubectl.tar.gz -C /opt/koko/bin/ \
    && mv /opt/koko/bin/kubectl /opt/koko/bin/rawkubectl \
    && wget -O helm.tar.gz https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz \
    && tar -xf helm.tar.gz --strip-components=1 -C /opt/koko/bin/ linux-${TARGETARCH}/helm \
    && mv /opt/koko/bin/helm /opt/koko/bin/rawhelm \
    && \
    if [ "${TARGETARCH}" == "amd64" ] || [ "${TARGETARCH}" == "arm64" ]; then \
        wget ${DOWNLOAD_URL}/files/clickhouse/22.20.2.11/clickhouse-client-linux-${TARGETARCH}.tar.gz; \
        tar -xf clickhouse-client-linux-${TARGETARCH}.tar.gz -C /opt/koko/bin/; \
    fi \
    && wget ${DOWNLOAD_URL}/public/kubectl_aliases.tar.gz -O kubectl_aliases.tar.gz \
    && tar -xf kubectl_aliases.tar.gz \
    && chmod +x /opt/koko/bin/* \
    && chown root:root /opt/koko/bin/* \
    && rm -f *.tar.gz

ADD go.mod go.sum .

ARG GOPROXY=https://goproxy.io
ENV CGO_ENABLED=0
ENV GO111MODULE=on
ENV GOOS=linux

RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download -x

COPY . .

COPY --from=ui-build /opt/koko/ui/dist ui/dist

ARG VERSION
ENV VERSION=$VERSION

RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    set +x \
    && make build -s \
    && set -x && ls -al . \
    && mv /opt/koko/build/koko-linux-${TARGETARCH} /opt/koko/koko \
    && mv /opt/koko/build/helm-linux-${TARGETARCH} /opt/koko/bin/helm \
    && mv /opt/koko/build/kubectl-linux-${TARGETARCH} /opt/koko/bin/kubectl

RUN mkdir /opt/koko/release \
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
        git                           \
        git-lfs                       \
        iputils-ping                  \
        jq                            \
        less                          \
        locales                       \
        openssh-client                \
        freetds-bin                   \
        mariadb-client                \
        postgresql-client             \
        telnet                        \
        unzip                         \
        vim                           \
        wget                          \
        xz-utils"

ARG APT_MIRROR=http://mirrors.ustc.edu.cn

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked,id=koko \
    sed -i "s@http://.*.debian.org@${APT_MIRROR}@g" /etc/apt/sources.list \
    && rm -f /etc/apt/apt.conf.d/docker-clean \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && apt-get update \
    && apt-get install -y --no-install-recommends ${DEPENDENCIES} \
    && echo "no" | dpkg-reconfigure dash \
    && echo "zh_CN.UTF-8" | dpkg-reconfigure locales \
    && sed -i "s@# export @export @g" ~/.bashrc \
    && sed -i "s@# alias @alias @g" ~/.bashrc \
    && rm -rf /var/lib/apt/lists/*

ARG MONGOSH_VERSION=1.10.3
RUN set -ex \
    && \
    case "${TARGETARCH}" in \
        amd64) \
            wget https://downloads.mongodb.com/compass/mongosh-${MONGOSH_VERSION}-linux-x64.tgz \
            && tar -xf mongosh-${MONGOSH_VERSION}-linux-x64.tgz \
            && chown root:root mongosh-${MONGOSH_VERSION}-linux-x64/bin/* \
            && mv mongosh-${MONGOSH_VERSION}-linux-x64/bin/mongosh /usr/local/bin/ \
            && mv mongosh-${MONGOSH_VERSION}-linux-x64/bin/mongosh_crypt_v1.so /usr/local/lib/ \
            && rm -rf mongosh-${MONGOSH_VERSION}-linux-x64* \
            ;; \
        arm64) \
            wget https://downloads.mongodb.com/compass/mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}.tgz \
            && tar -xf mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}.tgz \
            && chown root:root mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}/bin/* \
            && mv mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}/bin/mongosh /usr/local/bin/ \
            && mv mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}/bin/mongosh_crypt_v1.so /usr/local/lib/ \
            && rm -rf mongosh-${MONGOSH_VERSION}-linux-${TARGETARCH}* \
            ;; \
        *) \
            echo "Unsupported architecture: ${TARGETARCH}" \
            ;; \
    esac

COPY --from=redis /usr/local/bin/redis-cli /usr/local/bin/redis-cli

WORKDIR /opt/koko/

COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=stage-build /opt/koko/bin /usr/local/bin
COPY --from=stage-build /opt/koko/release .
COPY --from=stage-build /opt/koko/koko .

ENV LANG=zh_CN.UTF-8

EXPOSE 2222 5000
CMD ["./entrypoint.sh"]
