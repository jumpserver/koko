FROM jumpserver/koko-base:20250624_070957 AS stage-build

WORKDIR /opt/koko
ARG TARGETARCH
COPY . .

ARG VERSION
ENV VERSION=$VERSION

WORKDIR /opt/koko/ui
RUN yarn build

WORKDIR /opt/koko
RUN make build -s \
    && set -x && ls -al . \
    && mv /opt/koko/build/koko /opt/koko/koko \
    && mv /opt/koko/bin/rawhelm /opt/koko/bin/helm \
    && mv /opt/koko/bin/rawkubectl /opt/koko/bin/kubectl

RUN mkdir /opt/koko/release \
    && mv /opt/koko/locale /opt/koko/release \
    && mv /opt/koko/config_example.yml /opt/koko/release \
    && mv /opt/koko/entrypoint.sh /opt/koko/release \
    && mv /opt/koko/utils/init-kubectl.sh /opt/koko/release \
    && chmod 755 /opt/koko/release/entrypoint.sh /opt/koko/release/init-kubectl.sh

FROM debian:bullseye-slim
ARG TARGETARCH
ENV LANG=en_US.UTF-8

LABEL org.opencontainers.image.source=https://github.com/jumpserver/koko
LABEL org.opencontainers.image.description="JumpServer Koko"


ARG DEPENDENCIES="                    \
        bash-completion               \
        jq                            \
        less                          \
        ca-certificates"

ARG APT_MIRROR=http://deb.debian.org

RUN set -ex \
    && sed -i "s@http://.*.debian.org@${APT_MIRROR}@g" /etc/apt/sources.list \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && apt-get update \
    && apt-get install -y --no-install-recommends ${DEPENDENCIES} \
    && apt-get clean all \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/koko

COPY --from=stage-build /usr/local/bin/redis-cli /usr/local/bin/redis-cli
COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=stage-build /opt/koko/bin /usr/local/bin
COPY --from=stage-build /opt/koko/lib /usr/local/lib
COPY --from=stage-build /opt/koko/release .
COPY --from=stage-build /opt/koko/koko .

ARG VERSION
ENV VERSION=${VERSION}

VOLUME /opt/koko/data

ENTRYPOINT ["./entrypoint.sh"]

EXPOSE 2222

STOPSIGNAL SIGQUIT

CMD [ "./koko" ]
