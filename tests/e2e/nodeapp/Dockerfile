FROM alpine:3.7

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
