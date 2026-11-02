# Mongo Shell and recording dependencies

MongoDB PTY sessions use `usql --mongo` and retain the existing password prompt,
TLS URI options, HTTP CONNECT gateway and nobody account. MongoDB support is
detected through `usql --drivers`. The standalone mongosh executable and
mongosh_crypt_v1.so are no longer part of the runtime image.

Release usql v0.2.8 with Mongo Shell support before rebuilding Dockerfile-base.
The normal base-image workflow builds both architectures and updates the pinned
koko-base tag in Dockerfile. The runtime build checks that its base includes the
MongoDB-capable usql; an older pinned base fails with the missing capability
instead of producing a broken MongoDB runtime.

For local validation before publishing usql, build the usql Linux binary from
the sibling repository, copy it to `/opt/koko/bin/usql` in a local derivative
of the pinned koko-base image, and override that image using BuildKit's
`--build-context jumpserver/koko-base:<pinned-tag>=docker-image://<local-base>`.
This does not publish or replace an existing registry image.

FFmpeg 7.1.5 is compiled in the `stage-ffmpeg` build stage with a pinned SHA-256.
Its FFmpeg libraries are static; libx264, libc and libm remain dynamic. The runtime
installs only libx264 rather than the Debian ffmpeg package and its codec,
audio and GPU dependencies. Enabled features are file input, concat/image2,
MJPEG decoding, scaling/format conversion, libx264 encoding and MP4 output.
Compiler tools and source archives remain in the build stage.

The `TestRecordingWithSlimFFmpeg` test accepts `KOKO_TEST_FFMPEG` and verifies the
actual encodeMP4 path using odd-sized JPEG frames and faststart MP4 output.
Both amd64 and arm64 FFmpeg binaries must be checked before release.

## Local validation

The arm64 EE comparison, built against the same pinned base, measured 1,318,670,336
bytes of expanded layers before the change and 672,477,184 bytes afterwards
(646 MB / 49% smaller). Local Docker disk usage was 1.75 GB versus 865 MB; y5's
uncompressed content storage and amd64 architecture use a different disk-usage
measurement. These are local test images, not published releases.

The amd64 and arm64 slim FFmpeg builds encoded odd-sized JPEG fixtures into
64x48 H.264 MP4 files. The arm64 full Koko image also passed the version and
MongoDB connection checks as nobody. MongoDB integration covered SCRAM login,
TLS, a client certificate, HTTP CONNECT with TLS, multi-batch cursors, and the
existing KoKo MatchLoginPrefix/DoLogin flow. A shared CRUD/aggregation/BSON script
produced the same structured results in official mongosh and usql.

The existing MongoDB shell is not completely replicated: Node.js APIs, general
async/await, transaction/session helpers and client-side field encryption remain
unsupported. See the usql README for the supported commands and limits.
