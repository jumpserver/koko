# Recording dependencies

FFmpeg 7.1.5 is compiled in the `stage-ffmpeg` stage of `Dockerfile-base` with
a pinned SHA-256. Only the resulting executable is copied to
`/opt/koko/bin/ffmpeg` in the base image; compiler tools and source archives
remain in the separate build stage. Compilation uses two jobs.

Its FFmpeg libraries are static; libx264, libc and libm remain dynamic. The runtime
installs `libx264-164` instead of the Debian ffmpeg package. Enabled features are
file input, concat/image2, MJPEG decoding, scaling/format conversion, libx264
encoding and MP4 output.

Build and publish the base image for both amd64 and arm64 before building the
runtime image. The base-image workflow updates the pinned koko-base tag in
`Dockerfile`. Normal image builds copy the prebuilt FFmpeg executable through
`/opt/koko/bin`, without downloading or compiling FFmpeg sources.

MongoDB sessions continue to use mongosh and its `mongosh_crypt_v1.so` library.
The usql release remains v0.2.7.

The `TestRecordingWithSlimFFmpeg` test accepts `KOKO_TEST_FFMPEG` and verifies the
actual encodeMP4 path using odd-sized JPEG frames and faststart MP4 output.
Both amd64 and arm64 FFmpeg binaries must be checked before release.
