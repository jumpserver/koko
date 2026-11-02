FROM debian:trixie-slim AS stage-ffmpeg
ARG APT_MIRROR=http://deb.debian.org
RUN set -ex \
    && sed -i "s@http://.*.debian.org@${APT_MIRROR}@g" /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends build-essential pkg-config nasm libx264-dev xz-utils \
    && rm -rf /var/lib/apt/lists/*

ADD --checksum=sha256:de668509caf9e35e3cd162473441fdb29538c6d96ed080292b3cf9e6fc5d558f \
    https://ffmpeg.org/releases/ffmpeg-7.1.5.tar.xz /tmp/ffmpeg.tar.xz

# Web recordings only need JPEG frames, scaling, H.264 encoding and MP4 output.
# FFmpeg libraries are static; libx264, libc and libm remain dynamic.
RUN set -ex \
    && mkdir /tmp/ffmpeg \
    && tar -xf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg --strip-components=1 \
    && cd /tmp/ffmpeg \
    && ./configure --disable-everything --disable-autodetect --disable-network \
        --disable-doc --disable-debug --disable-ffplay --disable-ffprobe \
        --disable-avdevice --disable-swresample --disable-shared --enable-static \
        --enable-small --enable-ffmpeg --enable-gpl --enable-libx264 \
        --enable-protocol=file --enable-demuxer=concat,image2 \
        --enable-parser=mjpeg --enable-decoder=mjpeg --enable-encoder=libx264 \
        --enable-muxer=mp4 --enable-filter=scale,format,null --enable-swscale \
    && make -j2 ffmpeg \
    && install -m 755 ffmpeg /usr/local/bin/ffmpeg \
    && strip /usr/local/bin/ffmpeg

FROM jumpserver/koko-base:20261028_075134 AS stage-build
WORKDIR /opt/koko
ARG TARGETARCH

COPY . .

ARG VERSION
ENV VERSION=$VERSION

WORKDIR /opt/koko
RUN /opt/koko/bin/usql --drivers | grep -x mongodb \
    && rm -f /opt/koko/bin/mongosh \
    && GOMAXPROCS=2 GOFLAGS=-p=2 make build -s \
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
        libx264-164                   \
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
COPY --from=stage-ffmpeg /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=stage-build /opt/koko/release .
COPY --from=stage-build /opt/koko/koko .

COPY s6-overlay/cont-init.d/ /etc/cont-init.d/
COPY s6-overlay/services.d/ /etc/services.d/
RUN chmod +x /etc/cont-init.d/10-koko-init \
    /etc/services.d/koko/run \
    /etc/services.d/guacd/run \
    /etc/services.d/guacd/log/run \
    && ffmpeg -hide_banner -version

ARG VERSION
ENV VERSION=${VERSION}

VOLUME /opt/koko/data

ENTRYPOINT ["/init"]

EXPOSE 2222 5000 5001

STOPSIGNAL SIGQUIT
