FROM jumpserver/koko-base:20261028_075134 AS stage-build
WORKDIR /opt/koko
ARG TARGETARCH
ARG USQL_VERSION=v0.2.7

# The pinned base image may contain an older usql; update the binary copied
# into the final image without requiring a separate base-image release.
RUN set -ex \
    && asset="usql-${USQL_VERSION}-linux-${TARGETARCH}.tar.gz" \
    && release_url="https://github.com/jumpserver-dev/usql/releases/download/${USQL_VERSION}" \
    && wget -O "/tmp/${asset}" "${release_url}/${asset}" \
    && wget -O /tmp/usql_checksums.txt "${release_url}/usql_${USQL_VERSION#v}_checksums.txt" \
    && cd /tmp \
    && grep "  ${asset}$" usql_checksums.txt | sha256sum -c - \
    && tar -xzf "${asset}" -C /opt/koko/bin usql \
    && rm -f "${asset}" usql_checksums.txt \
    && /opt/koko/bin/usql --drivers | grep -qx db2

COPY . .

ARG VERSION
ENV VERSION=$VERSION

WORKDIR /opt/koko
RUN make build -s \
    && set -x && ls -al . \
    && mv /opt/koko/build/koko /opt/koko/koko

RUN mkdir /opt/koko/release \
    && mv /opt/koko/locale /opt/koko/release \
    && mv /opt/koko/config_example.yml /opt/koko/release \
    && mv /opt/koko/entrypoint.sh /opt/koko/release \
    && mv /opt/koko/utils/init-kubectl.sh /opt/koko/release \
    && chmod 755 /opt/koko/release/entrypoint.sh /opt/koko/release/init-kubectl.sh

FROM jumpserver/guacd:1.5.5-trixie
ARG TARGETARCH
ENV LANG=C.UTF-8

LABEL org.opencontainers.image.source=https://github.com/jumpserver/koko
LABEL org.opencontainers.image.description="JumpServer Koko"

USER root

ARG DEPENDENCIES="                    \
        bash-completion               \
        ffmpeg                        \
        ghostscript                   \
        less                          \
        openssl                       \
        redis-tools                   \
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

COPY --from=stage-build /opt/koko/.kubectl_aliases /opt/kubectl-aliases/.kubectl_aliases
COPY --from=stage-build /opt/koko/bin /usr/local/bin
COPY --from=stage-build /opt/koko/lib /usr/local/lib
COPY --from=stage-build /opt/koko/release .
COPY --from=stage-build /opt/koko/koko .

COPY s6-overlay/cont-init.d/ /etc/cont-init.d/
COPY s6-overlay/services.d/ /etc/services.d/
RUN chmod +x /etc/cont-init.d/10-koko-init \
    /etc/services.d/koko/run \
    /etc/services.d/guacd/run \
    /etc/services.d/guacd/log/run

ARG VERSION
ENV VERSION=${VERSION}

VOLUME /opt/koko/data

ENTRYPOINT ["/init"]

EXPOSE 2222 5000 5001

STOPSIGNAL SIGQUIT
