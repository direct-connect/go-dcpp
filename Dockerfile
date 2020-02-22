### Build stage ###
FROM golang:1.12-alpine3.11 AS build

RUN \
echo "**** install build dependencies ****" && \
apk add --no-cache gcc git musl-dev && \
mkdir /app

WORKDIR /app

# Copy go.mod and go.sum and get *most* of the dependecies. Do it separately
# before copying everything so it can be cached for faster rebuilds.
COPY go.* ./
RUN \
echo "**** get go dependencies ****" && \
go get -d -v ./...

# When building from the gohub repository, just copy it in
copy . .
# When building from outside the gohub repository, grab the source
#RUN git clone --depth=1 https://github.com/direct-connect/go-dcpp.git .

# Get dependencies
RUN \
echo "**** Install docker start script ****" && \
mv -v start-docker.sh /bin/go-dcpp.sh && \
echo "**** build and install GoHub ****" && \
go install -v ./...

# Whether to run the go-dcpp tests (0 = no, 1 = yes)
ARG RUN_TESTS
ENV RUN_TESTS=${RUN_TESTS}

# The tests are currently flaky, so skip them by default
RUN \
if [ "${RUN_TESTS:-0}" = "1" ]; then \
echo "**** Testing GoHub ****" && \
go test ./...; \
else \
echo "**** Skipping GoHub Tests ****"; \
fi


### Final container stage ###
FROM alpine:3.11

LABEL description="GoHub Direct Connect Hub"

RUN \
echo "**** install dependencies ****" && \
apk add --no-cache pwgen && \
echo "**** create config directory ****" && \
mkdir /config

WORKDIR /config
VOLUME /config

# The GoHub port (TCP)
EXPOSE 1411

# The HubStats port (TCP), be careful exposing this to the internet
EXPOSE 2112

# The pprof profiling port (TCP), be careful exposing this to the internet
EXPOSE 6060

# The default admin username and password (only used on the first run)
ENV ADMIN_USER="admin"
# Empty password means generate a random password on the first run
ENV ADMIN_PASSWORD=""

# The arguments to pass to go-hub serve
CMD []
ENTRYPOINT ["go-dcpp.sh"]

# Run this command periodically to make sure everything is working
# Disabled by default since it clogs up the hub logs with connects and
# disconnects from the pinger
#HEALTHCHECK CMD dcping ping 127.0.0.1:1411

# Copy go-hub and dcping from the build stage
COPY --from=build /go/bin/dcping /go/bin/go-hub /bin/go-dcpp.sh /bin/

# The GoHub user and group to run as (change with docker run -u)
USER 888:888
