FROM jumpserver/koko-base:20261127_103612 AS stage-build
WORKDIR /opt/koko
ARG TARGETARCH

COPY . .

RUN make bundle-resources DESTDIR=/opt/koko/release \
    && mv /opt/koko/locale /opt/koko/release \
    && mv /opt/koko/config_example.yml /opt/koko/release \
    && mv /opt/koko/entrypoint.sh /opt/koko/release \
    && mv /opt/koko/utils/init-kubectl.sh /opt/koko/release \
    && chmod 755 /opt/koko/release/entrypoint.sh /opt/koko/release/init-kubectl.sh

ARG VERSION=dev
RUN GOPROXY=off GOMAXPROCS=2 GOFLAGS=-p=2 make build -s VERSION="$VERSION" \
    && ./build/usql --drivers | grep -qx mongodb \
    && ./build/usql --drivers | grep -qx redis

FROM jumpserver/guacd:1.5.5-trixie
ARG TARGETARCH
ENV LANG=C.UTF-8

LABEL org.opencontainers.image.source=https://github.com/jumpserver/koko
LABEL org.opencontainers.image.description="JumpServer Koko"

USER root

ARG DEPENDENCIES="                    \
        bash-completion               \
        libx264-164                   \
        ghostscript                   \
        less                          \
        openssl                       \
        xz-utils                      \
        ca-certificates"

ARG APT_MIRROR=http://deb.debian.org

RUN set -ex \
    && sed -i "s@http://.*.debian.org@${APT_MIRROR}@g" /etc/apt/sources.list.d/debian.sources \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && apt-get update \
    && apt-get install -y --no-install-recommends ${DEPENDENCIES} \
    && apt-get clean all \
    && rm -rf /var/lib/apt/lists/*

RUN --mount=from=stage-build,source=/opt/s6-overlay,target=/tmp/s6-overlay \
    set -ex \
    && tar -C / -Jxpf /tmp/s6-overlay/s6-overlay-noarch.tar.xz \
    && tar -C / -Jxpf /tmp/s6-overlay/s6-overlay-arch.tar.xz

WORKDIR /opt/koko

COPY --from=stage-build /opt/codex /opt/codex
COPY cmd/kael/config_example.yml /opt/kael/config_example.yml
RUN install -d -m 0700 /opt/kael/data

COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=stage-build /opt/koko/bin /usr/local/bin
COPY --from=stage-build /opt/koko/build/usql /usr/local/bin/usql
COPY --from=stage-build /opt/koko/release .

COPY s6-overlay/cont-init.d/ /etc/cont-init.d/
COPY s6-overlay/services.d/ /etc/services.d/
RUN chmod +x /etc/cont-init.d/10-koko-init \
    /etc/services.d/koko/run \
    /etc/services.d/kael/run \
    /etc/services.d/guacd/run \
    /etc/services.d/guacd/log/run \
    && ffmpeg -hide_banner -version

COPY --from=stage-build /opt/koko/build/koko ./koko
COPY --from=stage-build /opt/koko/build/kael /opt/kael/kael

ARG VERSION=dev
ENV VERSION=${VERSION}

VOLUME /opt/koko/data /opt/kael/data

ENTRYPOINT ["/init"]

EXPOSE 2222 5000 5001 8083

STOPSIGNAL SIGQUIT
