FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN apk update && apk add openssh-client bash

WORKDIR /app

COPY nodeapp .
COPY id_rsa .
COPY id_rsa.pub .
COPY docker-entrypoint.sh .
COPY docker-exitpoint.sh .

RUN chmod +x nodeapp && \
    chmod + docker-entrypoint.sh && \
    chmod + docker-exitpoint.sh

ENTRYPOINT ["./docker-entrypoint.sh"]
