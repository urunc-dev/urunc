FROM gcr.io/oss-fuzz-base/base-builder-go
RUN apt-get update && apt-get install -y make autoconf automake libtool
COPY . $SRC/urunc
WORKDIR $SRC/urunc
COPY .clusterfuzzlite/build.sh $SRC/
