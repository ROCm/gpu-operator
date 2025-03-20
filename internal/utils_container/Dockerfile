# Base image
FROM alpine:3.20.3

# Install build dependencies
RUN apk add --no-cache \
    bash \
    build-base \
    automake \
    autoconf \
    libtool \
    pkgconfig \
    gettext-dev \
    bison \
    wget \
    tar \
    flex \
    linux-headers

# Set working directory
WORKDIR /tmp

RUN wget https://github.com/util-linux/util-linux/archive/v2.40.tar.gz && tar -xzf v2.40.tar.gz

# Build and install nsenter only
WORKDIR /tmp/util-linux-2.40
RUN ./autogen.sh && \
    ./configure --disable-all-programs --enable-nsenter && \
    make nsenter && \
    cp nsenter /nsenter

ENTRYPOINT ["/nsenter"]
